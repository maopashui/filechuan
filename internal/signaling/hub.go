package signaling

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/yuxy/filechuan/internal/auth"
	"github.com/yuxy/filechuan/internal/config"
)

const (
	maxMessageBytes = 64 * 1024
	writeWait       = 10 * time.Second
	pongWait        = 60 * time.Second
	pingPeriod      = 30 * time.Second
)

func checkOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		// Non-browser clients (tests/tools). Browsers always send Origin on WS.
		return true
	}
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return false
	}
	return strings.EqualFold(u.Host, r.Host)
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	CheckOrigin:     checkOrigin,
}

type Message struct {
	Type    string          `json:"type"`
	Room    string          `json:"room,omitempty"`
	From    string          `json:"from,omitempty"`
	To      string          `json:"to,omitempty"`
	Payload json.RawMessage `json:"payload,omitempty"`
	PeerID  string          `json:"peerId,omitempty"`
	Peers   []PeerInfo      `json:"peers,omitempty"`
	Error   string          `json:"error,omitempty"`
}

type PeerInfo struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
}

type peer struct {
	id     string
	name   string
	room   string
	conn   *websocket.Conn
	send   chan []byte
	hub    *Hub
	closed bool
	mu     sync.Mutex
}

type room struct {
	code       string
	peers      map[string]*peer
	createdAt  time.Time
	lastActive time.Time
	mu         sync.Mutex
}

type Hub struct {
	cfg   *config.Config
	mu    sync.Mutex
	rooms map[string]*room

	createMu sync.Mutex
	creates  map[string][]time.Time
}

func NewHub(cfg *config.Config) *Hub {
	h := &Hub{
		cfg:     cfg,
		rooms:   make(map[string]*room),
		creates: make(map[string][]time.Time),
	}
	go h.janitor()
	return h
}

func (h *Hub) janitor() {
	t := time.NewTicker(time.Minute)
	for range t.C {
		h.sweepRooms()
	}
}

func (h *Hub) sweepRooms() {
	h.mu.Lock()
	now := time.Now()
	type doomed struct {
		code  string
		peers []*peer
	}
	var closeList []doomed
	for code, r := range h.rooms {
		r.mu.Lock()
		idle := now.Sub(r.lastActive) > h.cfg.RoomIdleTimeout
		tooOld := h.cfg.RoomMaxAge > 0 && now.Sub(r.createdAt) > h.cfg.RoomMaxAge
		empty := len(r.peers) == 0
		var peers []*peer
		if tooOld {
			peers = make([]*peer, 0, len(r.peers))
			for _, pe := range r.peers {
				peers = append(peers, pe)
			}
		}
		r.mu.Unlock()
		if tooOld {
			delete(h.rooms, code)
			closeList = append(closeList, doomed{code: code, peers: peers})
			continue
		}
		if empty && idle {
			delete(h.rooms, code)
		}
	}
	h.mu.Unlock()

	for _, d := range closeList {
		msg := mustJSON(Message{Type: "room_closed", Room: d.code})
		for _, pe := range d.peers {
			pe.trySend(msg)
			pe.room = ""
		}
	}
}

func (h *Hub) allowCreate(ip string) bool {
	h.createMu.Lock()
	defer h.createMu.Unlock()
	now := time.Now()
	cutoff := now.Add(-time.Minute)
	list := h.creates[ip]
	kept := list[:0]
	for _, t := range list {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	if len(kept) >= h.cfg.RoomCreatePerMin {
		h.creates[ip] = kept
		return false
	}
	kept = append(kept, now)
	h.creates[ip] = kept
	return true
}

func randomRoomCode() (string, error) {
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	out := make([]byte, 6)
	for i := range out {
		out[i] = alphabet[int(b[i])%len(alphabet)]
	}
	return string(out), nil
}

func clientIPFromRequest(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func (h *Hub) CreateRoomHTTP(w http.ResponseWriter, r *http.Request) {
	h.CreateRoomForIP(w, r, clientIPFromRequest(r))
}

func (h *Hub) CreateRoomForIP(w http.ResponseWriter, r *http.Request, ip string) {
	if ip == "" {
		ip = clientIPFromRequest(r)
	}
	if !h.allowCreate(ip) {
		http.Error(w, `{"error":{"code":"rate_limited","message":"too many rooms"}}`, http.StatusTooManyRequests)
		return
	}
	code, err := randomRoomCode()
	if err != nil {
		http.Error(w, `{"error":{"code":"internal","message":"failed"}}`, http.StatusInternalServerError)
		return
	}
	h.mu.Lock()
	for {
		if _, ok := h.rooms[code]; !ok {
			break
		}
		code, _ = randomRoomCode()
	}
	now := time.Now()
	h.rooms[code] = &room{code: code, peers: make(map[string]*peer), createdAt: now, lastActive: now}
	h.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"room":     code,
		"join_url": "/lan/" + code,
	})
}

func (h *Hub) HandleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	conn.SetReadLimit(maxMessageBytes)
	_ = conn.SetReadDeadline(time.Now().Add(pongWait))
	conn.SetPongHandler(func(string) error {
		_ = conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	p := &peer{
		id:   mustPeerID(),
		conn: conn,
		send: make(chan []byte, 32),
		hub:  h,
	}
	go p.writePump()
	p.readPump()
}

func mustPeerID() string {
	t, err := auth.RandomToken(12)
	if err != nil {
		return time.Now().Format("150405.000")
	}
	return t
}

func (p *peer) readPump() {
	defer func() {
		p.leave()
		p.mu.Lock()
		if !p.closed {
			p.closed = true
			close(p.send)
		}
		p.mu.Unlock()
		_ = p.conn.Close()
	}()
	for {
		_, data, err := p.conn.ReadMessage()
		if err != nil {
			return
		}
		var msg Message
		if err := json.Unmarshal(data, &msg); err != nil {
			p.sendErr("invalid_json")
			continue
		}
		p.handle(msg)
	}
}

func (p *peer) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		_ = p.conn.Close()
	}()
	for {
		select {
		case msg, ok := <-p.send:
			_ = p.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				_ = p.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := p.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		case <-ticker.C:
			_ = p.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := p.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func (p *peer) handle(msg Message) {
	switch msg.Type {
	case "join":
		p.join(strings.ToUpper(strings.TrimSpace(msg.Room)), msg.PeerID)
	case "leave":
		p.leave()
	case "signal":
		p.relay(msg)
	case "close_room":
		p.closeRoom()
	default:
		p.sendErr("unknown_type")
	}
}

func (p *peer) join(code, name string) {
	if code == "" {
		p.sendErr("room_required")
		return
	}
	if p.room != "" {
		p.leave()
	}
	h := p.hub
	h.mu.Lock()
	r, ok := h.rooms[code]
	if !ok {
		h.mu.Unlock()
		p.sendErr("room_not_found")
		return
	}
	if h.cfg.RoomMaxAge > 0 && time.Since(r.createdAt) > h.cfg.RoomMaxAge {
		delete(h.rooms, code)
		h.mu.Unlock()
		p.sendErr("room_expired")
		return
	}
	h.mu.Unlock()

	r.mu.Lock()
	if len(r.peers) >= h.cfg.RoomMaxPeers {
		r.mu.Unlock()
		p.sendErr("room_full")
		return
	}
	if name == "" {
		name = "Peer"
	}
	if len(name) > 32 {
		name = name[:32]
	}
	p.name = name
	p.room = code
	r.peers[p.id] = p
	r.lastActive = time.Now()

	peers := make([]PeerInfo, 0, len(r.peers))
	for _, pe := range r.peers {
		peers = append(peers, PeerInfo{ID: pe.id, Name: pe.name})
	}
	r.mu.Unlock()

	p.sendJSON(Message{Type: "joined", Room: code, PeerID: p.id, Peers: peers})
	p.broadcast(Message{Type: "peer_joined", Room: code, From: p.id, Peers: []PeerInfo{{ID: p.id, Name: p.name}}}, true)
}

func (p *peer) leave() {
	if p.room == "" {
		return
	}
	code := p.room
	h := p.hub
	h.mu.Lock()
	r := h.rooms[code]
	h.mu.Unlock()
	if r == nil {
		p.room = ""
		return
	}
	r.mu.Lock()
	delete(r.peers, p.id)
	r.lastActive = time.Now()
	r.mu.Unlock()
	p.room = ""
	p.broadcastTo(code, Message{Type: "peer_left", Room: code, From: p.id}, false)
	// Keep empty rooms until idle timeout / max age so invite links survive brief disconnects.
}

func (p *peer) closeRoom() {
	if p.room == "" {
		return
	}
	code := p.room
	h := p.hub
	h.mu.Lock()
	r := h.rooms[code]
	delete(h.rooms, code)
	h.mu.Unlock()
	if r == nil {
		return
	}
	r.mu.Lock()
	peers := make([]*peer, 0, len(r.peers))
	for _, pe := range r.peers {
		peers = append(peers, pe)
	}
	r.mu.Unlock()
	msg := mustJSON(Message{Type: "room_closed", Room: code, From: p.id})
	for _, pe := range peers {
		pe.trySend(msg)
		pe.room = ""
	}
}

func (p *peer) relay(msg Message) {
	if p.room == "" {
		p.sendErr("not_in_room")
		return
	}
	if msg.To == "" {
		p.sendErr("target_required")
		return
	}
	if len(msg.Payload) > maxMessageBytes {
		p.sendErr("payload_too_large")
		return
	}
	h := p.hub
	h.mu.Lock()
	r := h.rooms[p.room]
	h.mu.Unlock()
	if r == nil {
		p.sendErr("room_gone")
		return
	}
	r.mu.Lock()
	target := r.peers[msg.To]
	_, selfOK := r.peers[p.id]
	r.lastActive = time.Now()
	r.mu.Unlock()
	if !selfOK || target == nil {
		p.sendErr("peer_not_found")
		return
	}
	out := Message{Type: "signal", Room: p.room, From: p.id, To: msg.To, Payload: msg.Payload}
	target.trySend(mustJSON(out))
}

func (p *peer) broadcast(msg Message, excludeSelf bool) {
	p.broadcastTo(p.room, msg, excludeSelf)
}

func (p *peer) broadcastTo(code string, msg Message, excludeSelf bool) {
	h := p.hub
	h.mu.Lock()
	r := h.rooms[code]
	h.mu.Unlock()
	if r == nil {
		return
	}
	data := mustJSON(msg)
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, pe := range r.peers {
		if excludeSelf && id == p.id {
			continue
		}
		pe.trySend(data)
	}
}

func (p *peer) sendJSON(msg Message) {
	p.trySend(mustJSON(msg))
}

func (p *peer) sendErr(code string) {
	p.sendJSON(Message{Type: "error", Error: code})
}

func (p *peer) trySend(data []byte) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return
	}
	select {
	case p.send <- data:
	default:
		// drop if slow consumer
	}
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		return []byte(`{"type":"error","error":"encode"}`)
	}
	return b
}

var ErrRoomNotFound = errors.New("room not found")
