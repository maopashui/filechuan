package signaling

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
	"github.com/yuxy/filechuan/internal/config"
)

func TestRoomSignalIsolation(t *testing.T) {
	cfg := &config.Config{RoomMaxPeers: 8, RoomIdleTimeout: time.Hour, RoomMaxAge: 24 * time.Hour, RoomCreatePerMin: 100}
	hub := NewHub(cfg)
	srv := httptest.NewServer(http.HandlerFunc(hub.HandleWS))
	defer srv.Close()
	u := "ws" + srv.URL[len("http"):]

	create := func(code string) {
		hub.mu.Lock()
		now := time.Now()
		hub.rooms[code] = &room{code: code, peers: make(map[string]*peer), createdAt: now, lastActive: now}
		hub.mu.Unlock()
	}
	create("AAAAAA")
	create("BBBBBB")

	dial := func() *websocket.Conn {
		c, _, err := websocket.DefaultDialer.Dial(u, nil)
		require.NoError(t, err)
		return c
	}

	a := dial()
	b := dial()
	defer a.Close()
	defer b.Close()

	require.NoError(t, a.WriteMessage(websocket.TextMessage, []byte(`{"type":"join","room":"AAAAAA","peerId":"Alice"}`)))
	_, msgA, err := a.ReadMessage()
	require.NoError(t, err)
	var joinedA map[string]any
	require.NoError(t, json.Unmarshal(msgA, &joinedA))
	require.Equal(t, "joined", joinedA["type"])

	require.NoError(t, b.WriteMessage(websocket.TextMessage, []byte(`{"type":"join","room":"BBBBBB","peerId":"Bob"}`)))
	_, msgB, err := b.ReadMessage()
	require.NoError(t, err)
	var joinedB map[string]any
	require.NoError(t, json.Unmarshal(msgB, &joinedB))
	require.Equal(t, "joined", joinedB["type"])
	bobID, _ := joinedB["peerId"].(string)
	require.NotEmpty(t, bobID)

	payload, _ := json.Marshal(map[string]any{
		"type": "signal", "to": bobID, "payload": map[string]any{"kind": "sdp"},
	})
	require.NoError(t, a.WriteMessage(websocket.TextMessage, payload))
	require.NoError(t, a.SetReadDeadline(time.Now().Add(2*time.Second)))
	_, errBody, err := a.ReadMessage()
	require.NoError(t, err)
	var errMsg map[string]any
	require.NoError(t, json.Unmarshal(errBody, &errMsg))
	require.Equal(t, "error", errMsg["type"])
	require.Equal(t, "peer_not_found", errMsg["error"])

	require.NoError(t, b.SetReadDeadline(time.Now().Add(200*time.Millisecond)))
	_, _, err = b.ReadMessage()
	require.Error(t, err)
}

func TestJoinDoesNotAutoCreate(t *testing.T) {
	cfg := &config.Config{RoomMaxPeers: 8, RoomIdleTimeout: time.Hour, RoomMaxAge: 24 * time.Hour, RoomCreatePerMin: 100}
	hub := NewHub(cfg)
	srv := httptest.NewServer(http.HandlerFunc(hub.HandleWS))
	defer srv.Close()
	u := "ws" + srv.URL[len("http"):]

	c, _, err := websocket.DefaultDialer.Dial(u, nil)
	require.NoError(t, err)
	defer c.Close()

	require.NoError(t, c.WriteMessage(websocket.TextMessage, []byte(`{"type":"join","room":"NOSUCH","peerId":"X"}`)))
	require.NoError(t, c.SetReadDeadline(time.Now().Add(2*time.Second)))
	_, body, err := c.ReadMessage()
	require.NoError(t, err)
	var msg map[string]any
	require.NoError(t, json.Unmarshal(body, &msg))
	require.Equal(t, "error", msg["type"])
	require.Equal(t, "room_not_found", msg["error"])
}

func TestCheckOriginRejectsCrossSite(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "http://example.com/ws", nil)
	req.Host = "example.com"
	req.Header.Set("Origin", "https://evil.example")
	require.False(t, checkOrigin(req))

	req2 := httptest.NewRequest(http.MethodGet, "http://example.com/ws", nil)
	req2.Host = "example.com"
	req2.Header.Set("Origin", "http://example.com")
	require.True(t, checkOrigin(req2))

	req3 := httptest.NewRequest(http.MethodGet, "http://example.com/ws", nil)
	req3.Host = "example.com"
	require.True(t, checkOrigin(req3))
}

func TestRoomMaxAgeSweep(t *testing.T) {
	cfg := &config.Config{RoomMaxPeers: 8, RoomIdleTimeout: time.Hour, RoomMaxAge: time.Millisecond, RoomCreatePerMin: 100}
	hub := NewHub(cfg)
	hub.mu.Lock()
	hub.rooms["OLDOLD"] = &room{
		code: "OLDOLD", peers: make(map[string]*peer),
		createdAt: time.Now().Add(-time.Second), lastActive: time.Now(),
	}
	hub.mu.Unlock()
	hub.sweepRooms()
	hub.mu.Lock()
	_, ok := hub.rooms["OLDOLD"]
	hub.mu.Unlock()
	require.False(t, ok)
}
