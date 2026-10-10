package api_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
	"github.com/yuxy/filechuan/internal/config"
	"github.com/yuxy/filechuan/internal/server"
)

func testApp(t *testing.T) (*server.App, *httptest.Server, *http.Client) {
	t.Helper()
	dir := t.TempDir()
	cfg := &config.Config{
		ListenAddr:       ":0",
		DataDir:          dir,
		DBPath:           filepath.Join(dir, "app.db"),
		FilesDir:         filepath.Join(dir, "files"),
		TempDir:          filepath.Join(dir, "temp"),
		LogsDir:          filepath.Join(dir, "logs"),
		SessionTTL:       time.Hour,
		CookieSecure:     false,
		CookieSameSite:   "Lax",
		TrustedProxies:   []string{"127.0.0.1", "::1"},
		MaxFileSize:      10 << 20,
		PartSize:         1024,
		MaxUploadConcurr: 2,
		RoomMaxPeers:     8,
		RoomIdleTimeout:  time.Hour,
		RoomMaxAge:       24 * time.Hour,
		RoomCreatePerMin: 100,
		STUNServers:      []string{"stun:stun.l.google.com:19302"},
		CleanupInterval:  time.Hour,
		TempTTL:          time.Hour,
	}
	app, err := server.New(cfg, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = app.Close() })
	ts := httptest.NewServer(app.Engine)
	t.Cleanup(ts.Close)
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, Timeout: 10 * time.Second}
	return app, ts, client
}

func postJSON(t *testing.T, client *http.Client, url string, body any, csrf string) *http.Response {
	t.Helper()
	b, _ := json.Marshal(body)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(b))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	if csrf != "" {
		req.Header.Set("X-CSRF-Token", csrf)
	}
	resp, err := client.Do(req)
	require.NoError(t, err)
	return resp
}

func csrfFromJar(client *http.Client, base string) string {
	u, _ := http.NewRequest(http.MethodGet, base, nil)
	for _, c := range client.Jar.Cookies(u.URL) {
		if c.Name == "fc_csrf" {
			return c.Value
		}
	}
	return ""
}

func TestSetupRegisterAndRejectDuplicate(t *testing.T) {
	_, ts, client := testApp(t)
	resp := postJSON(t, client, ts.URL+"/api/v1/setup/register", map[string]string{
		"username": "admin", "password": "password123",
	}, "")
	require.Equal(t, 201, resp.StatusCode)
	_ = resp.Body.Close()

	resp = postJSON(t, client, ts.URL+"/api/v1/setup/register", map[string]string{
		"username": "admin2", "password": "password123",
	}, "")
	require.Equal(t, 409, resp.StatusCode)
	_ = resp.Body.Close()
}

func TestConcurrentRegister(t *testing.T) {
	_, ts, _ := testApp(t)
	var success atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			jar, _ := cookiejar.New(nil)
			c := &http.Client{Jar: jar}
			resp := postJSON(t, c, ts.URL+"/api/v1/setup/register", map[string]string{
				"username": fmt.Sprintf("admin%d", i), "password": "password123",
			}, "")
			if resp.StatusCode == 201 {
				success.Add(1)
			}
			_ = resp.Body.Close()
		}(i)
	}
	wg.Wait()
	require.Equal(t, int32(1), success.Load())
}

func TestLoginAndAuthGuard(t *testing.T) {
	_, ts, client := testApp(t)
	resp := postJSON(t, client, ts.URL+"/api/v1/setup/register", map[string]string{
		"username": "admin", "password": "password123",
	}, "")
	require.Equal(t, 201, resp.StatusCode)
	_ = resp.Body.Close()

	// logout then login fail/success
	csrf := csrfFromJar(client, ts.URL)
	resp = postJSON(t, client, ts.URL+"/api/v1/auth/logout", map[string]any{}, csrf)
	_ = resp.Body.Close()

	resp = postJSON(t, client, ts.URL+"/api/v1/auth/login", map[string]string{
		"username": "admin", "password": "wrongpass1",
	}, "")
	require.Equal(t, 401, resp.StatusCode)
	_ = resp.Body.Close()

	resp = postJSON(t, client, ts.URL+"/api/v1/auth/login", map[string]string{
		"username": "admin", "password": "password123",
	}, "")
	require.Equal(t, 200, resp.StatusCode)
	_ = resp.Body.Close()

	anon, _ := cookiejar.New(nil)
	anonClient := &http.Client{Jar: anon}
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/files", nil)
	resp, err := anonClient.Do(req)
	require.NoError(t, err)
	require.Equal(t, 401, resp.StatusCode)
	_ = resp.Body.Close()
}

func TestUploadPickupExpireRevokeDelete(t *testing.T) {
	app, ts, client := testApp(t)
	resp := postJSON(t, client, ts.URL+"/api/v1/setup/register", map[string]string{
		"username": "admin", "password": "password123",
	}, "")
	require.Equal(t, 201, resp.StatusCode)
	_ = resp.Body.Close()
	csrf := csrfFromJar(client, ts.URL)

	content := bytes.Repeat([]byte("hello-飞船-"), 200)
	resp = postJSON(t, client, ts.URL+"/api/v1/uploads", map[string]any{
		"filename": "../evil.txt", "size": len(content), "expires_days": 1,
	}, csrf)
	require.Equal(t, 201, resp.StatusCode)
	var upload struct {
		ID         string `json:"id"`
		PartSize   int64  `json:"part_size"`
		TotalParts int    `json:"total_parts"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&upload))
	_ = resp.Body.Close()
	require.Equal(t, "evil.txt", filepath.Base("../evil.txt"))

	for part := 1; part <= upload.TotalParts; part++ {
		start := (part - 1) * int(upload.PartSize)
		end := start + int(upload.PartSize)
		if end > len(content) {
			end = len(content)
		}
		req, err := http.NewRequest(http.MethodPut, fmt.Sprintf("%s/api/v1/uploads/%s/parts/%d", ts.URL, upload.ID, part), bytes.NewReader(content[start:end]))
		require.NoError(t, err)
		req.Header.Set("X-CSRF-Token", csrf)
		resp, err = client.Do(req)
		require.NoError(t, err)
		require.Equal(t, 200, resp.StatusCode)
		_ = resp.Body.Close()
	}

	resp = postJSON(t, client, ts.URL+"/api/v1/uploads/"+upload.ID+"/complete", map[string]any{}, csrf)
	require.Equal(t, 200, resp.StatusCode)
	var fileRec struct {
		ID         int64  `json:"id"`
		PickupCode string `json:"pickup_code"`
		ShareToken string `json:"share_token"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&fileRec))
	_ = resp.Body.Close()
	require.NotEmpty(t, fileRec.PickupCode)
	require.NotContains(t, fileRec.ShareToken, "..")

	// verify code
	resp = postJSON(t, client, ts.URL+"/api/v1/pickup/verify", map[string]string{"code": fileRec.PickupCode}, "")
	require.Equal(t, 200, resp.StatusCode)
	_ = resp.Body.Close()

	resp = postJSON(t, client, ts.URL+"/api/v1/pickup/verify", map[string]string{"code": "ZZZZZZZZ"}, "")
	require.Equal(t, 404, resp.StatusCode)
	_ = resp.Body.Close()

	// download
	resp, err := client.Get(ts.URL + "/api/v1/pickup/" + fileRec.ShareToken + "/download")
	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode)
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	require.Equal(t, content, body)

	// revoke
	req, _ := http.NewRequest(http.MethodDelete, fmt.Sprintf("%s/api/v1/files/%d/pickup-code", ts.URL, fileRec.ID), nil)
	req.Header.Set("X-CSRF-Token", csrf)
	resp, err = client.Do(req)
	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode)
	_ = resp.Body.Close()

	resp, err = client.Get(ts.URL + "/api/v1/pickup/" + fileRec.ShareToken + "/download")
	require.NoError(t, err)
	require.Equal(t, 410, resp.StatusCode)
	_ = resp.Body.Close()

	// regenerate then expire
	resp = postJSON(t, client, fmt.Sprintf("%s/api/v1/files/%d/pickup-code", ts.URL, fileRec.ID), map[string]any{}, csrf)
	require.Equal(t, 200, resp.StatusCode)
	var regen struct {
		ShareToken string `json:"share_token"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&regen))
	_ = resp.Body.Close()

	_, err = app.DB.Exec(`UPDATE files SET expires_at = ?`, time.Now().UTC().Add(-time.Hour).Format(time.RFC3339))
	require.NoError(t, err)
	_, err = app.DB.Exec(`UPDATE pickup_codes SET expires_at = ?`, time.Now().UTC().Add(-time.Hour).Format(time.RFC3339))
	require.NoError(t, err)

	resp, err = client.Get(ts.URL + "/api/v1/pickup/" + regen.ShareToken + "/download")
	require.NoError(t, err)
	require.Equal(t, 410, resp.StatusCode)
	_ = resp.Body.Close()
}

func TestDeleteFileBlocksDownload(t *testing.T) {
	_, ts, client := testApp(t)
	resp := postJSON(t, client, ts.URL+"/api/v1/setup/register", map[string]string{
		"username": "admin", "password": "password123",
	}, "")
	_ = resp.Body.Close()
	csrf := csrfFromJar(client, ts.URL)
	content := []byte("delete-me")
	resp = postJSON(t, client, ts.URL+"/api/v1/uploads", map[string]any{
		"filename": "a.txt", "size": len(content), "expires_days": 7,
	}, csrf)
	var upload struct {
		ID         string `json:"id"`
		TotalParts int    `json:"total_parts"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&upload)
	_ = resp.Body.Close()
	req, _ := http.NewRequest(http.MethodPut, ts.URL+"/api/v1/uploads/"+upload.ID+"/parts/1", bytes.NewReader(content))
	req.Header.Set("X-CSRF-Token", csrf)
	resp, _ = client.Do(req)
	_ = resp.Body.Close()
	resp = postJSON(t, client, ts.URL+"/api/v1/uploads/"+upload.ID+"/complete", map[string]any{}, csrf)
	var fileRec struct {
		ID         int64  `json:"id"`
		ShareToken string `json:"share_token"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&fileRec)
	_ = resp.Body.Close()

	req, _ = http.NewRequest(http.MethodDelete, fmt.Sprintf("%s/api/v1/files/%d", ts.URL, fileRec.ID), nil)
	req.Header.Set("X-CSRF-Token", csrf)
	resp, err := client.Do(req)
	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode)
	_ = resp.Body.Close()

	resp, err = client.Get(ts.URL + "/api/v1/pickup/" + fileRec.ShareToken + "/download")
	require.NoError(t, err)
	require.True(t, resp.StatusCode == 410 || resp.StatusCode == 409 || resp.StatusCode == 404)
	_ = resp.Body.Close()
}

func TestAbortUploadCleansTemp(t *testing.T) {
	app, ts, client := testApp(t)
	resp := postJSON(t, client, ts.URL+"/api/v1/setup/register", map[string]string{
		"username": "admin", "password": "password123",
	}, "")
	_ = resp.Body.Close()
	csrf := csrfFromJar(client, ts.URL)
	resp = postJSON(t, client, ts.URL+"/api/v1/uploads", map[string]any{
		"filename": "x.bin", "size": 2048, "expires_days": 3,
	}, csrf)
	var upload struct {
		ID string `json:"id"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&upload)
	_ = resp.Body.Close()
	temp := filepath.Join(app.Cfg.TempDir, upload.ID)
	require.DirExists(t, temp)
	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/v1/uploads/"+upload.ID, nil)
	req.Header.Set("X-CSRF-Token", csrf)
	resp, err := client.Do(req)
	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode)
	_ = resp.Body.Close()
	_, err = os.Stat(temp)
	require.True(t, os.IsNotExist(err))
}

func TestAPI404NotHTML(t *testing.T) {
	_, ts, client := testApp(t)
	resp, err := client.Get(ts.URL + "/api/v1/does-not-exist")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, 404, resp.StatusCode)
	body, _ := io.ReadAll(resp.Body)
	require.Contains(t, string(body), `"error"`)
	require.NotContains(t, string(body), "<html")
}

func TestHealthz(t *testing.T) {
	_, ts, client := testApp(t)
	resp, err := client.Get(ts.URL + "/healthz")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, 200, resp.StatusCode)
}

func TestWSJoinViaAPI(t *testing.T) {
	_, ts, client := testApp(t)
	resp, err := client.Post(ts.URL+"/api/v1/rooms", "application/json", nil)
	require.NoError(t, err)
	var room struct {
		Room string `json:"room"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&room))
	_ = resp.Body.Close()
	require.NotEmpty(t, room.Room)

	u := "ws" + ts.URL[len("http"):] + "/ws/signaling"
	c, _, err := websocket.DefaultDialer.Dial(u, nil)
	require.NoError(t, err)
	defer c.Close()
	require.NoError(t, c.WriteMessage(websocket.TextMessage, []byte(
		fmt.Sprintf(`{"type":"join","room":"%s","peerId":"Z"}`, room.Room),
	)))
	require.NoError(t, c.SetReadDeadline(time.Now().Add(2*time.Second)))
	_, body, err := c.ReadMessage()
	require.NoError(t, err)
	require.Contains(t, string(body), `"joined"`)
}

func TestWSJoinRejectsUnknownRoom(t *testing.T) {
	_, ts, _ := testApp(t)
	u := "ws" + ts.URL[len("http"):] + "/ws/signaling"
	c, _, err := websocket.DefaultDialer.Dial(u, nil)
	require.NoError(t, err)
	defer c.Close()
	require.NoError(t, c.WriteMessage(websocket.TextMessage, []byte(`{"type":"join","room":"ZZZZZZ","peerId":"Z"}`)))
	require.NoError(t, c.SetReadDeadline(time.Now().Add(2*time.Second)))
	_, body, err := c.ReadMessage()
	require.NoError(t, err)
	require.Contains(t, string(body), `"room_not_found"`)
}

func TestAbortUploadRejectsPathTraversal(t *testing.T) {
	app, ts, client := testApp(t)
	resp := postJSON(t, client, ts.URL+"/api/v1/setup/register", map[string]string{
		"username": "admin", "password": "password123",
	}, "")
	_ = resp.Body.Close()
	csrf := csrfFromJar(client, ts.URL)

	marker := filepath.Join(app.Cfg.DataDir, "keep-me.txt")
	require.NoError(t, os.WriteFile(marker, []byte("ok"), 0o640))

	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/v1/uploads/..", nil)
	req.Header.Set("X-CSRF-Token", csrf)
	resp, err := client.Do(req)
	require.NoError(t, err)
	_ = resp.Body.Close()

	_, err = os.Stat(marker)
	require.NoError(t, err, "data dir must not be deleted via path traversal")
}
