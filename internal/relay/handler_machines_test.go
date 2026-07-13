package relay

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	dbstore "github.com/brporter/phosphor/internal/store"
)

func newMachinesTestServer(t *testing.T) (*Server, http.Handler) {
	t.Helper()
	hub := NewHub(NewMemorySessionStore(), nil, "test", slog.Default())
	authSessions := NewMemoryAuthSessionStore(5 * time.Minute)
	t.Cleanup(authSessions.Stop)
	s := NewServer(hub, slog.Default(), "http://test", nil, true, authSessions, nil, dbstore.NewFake(), 60*time.Second)
	return s, s.Handler()
}

func testAuthorizedKey(t *testing.T) string {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPub)))
}

func createMachine(t *testing.T, h http.Handler, token, name, pubKey string) machineJSON {
	t.Helper()
	body := fmt.Sprintf(`{"name":%q,"hostname":"box.local","public_key":%q}`, name, pubKey)
	req := httptest.NewRequest(http.MethodPost, "/api/machines", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create machine: status %d, body %s", w.Code, w.Body.String())
	}
	var m machineJSON
	if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func listMachines(t *testing.T, h http.Handler, token string) []machineJSON {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/machines", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list machines: status %d, body %s", w.Code, w.Body.String())
	}
	var out []machineJSON
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestMachines_CreateAndList(t *testing.T) {
	_, h := newMachinesTestServer(t)
	key := testAuthorizedKey(t)

	m := createMachine(t, h, "google:alice", "laptop", key)
	if m.Name != "laptop" || m.Online {
		t.Errorf("unexpected machine: %+v", m)
	}

	got := listMachines(t, h, "google:alice")
	if len(got) != 1 || got[0].ID != m.ID {
		t.Errorf("list = %+v", got)
	}

	// Another identity gets its own tenant and sees nothing.
	if got := listMachines(t, h, "google:bob"); len(got) != 0 {
		t.Errorf("cross-tenant list = %+v", got)
	}
}

func TestMachines_CreateValidation(t *testing.T) {
	_, h := newMachinesTestServer(t)
	key := testAuthorizedKey(t)

	cases := []struct {
		name   string
		body   string
		status int
	}{
		{"bad key", `{"name":"x","public_key":"not-a-key"}`, http.StatusBadRequest},
		{"missing name and hostname", fmt.Sprintf(`{"public_key":%q}`, key), http.StatusBadRequest},
		{"bad json", `{`, http.StatusBadRequest},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(http.MethodPost, "/api/machines", strings.NewReader(tc.body))
		req.Header.Set("Authorization", "Bearer google:alice")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != tc.status {
			t.Errorf("%s: status %d, want %d", tc.name, w.Code, tc.status)
		}
	}

	// Duplicates
	createMachine(t, h, "google:alice", "laptop", key)
	req := httptest.NewRequest(http.MethodPost, "/api/machines",
		strings.NewReader(fmt.Sprintf(`{"name":"laptop","public_key":%q}`, testAuthorizedKey(t))))
	req.Header.Set("Authorization", "Bearer google:alice")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusConflict {
		t.Errorf("duplicate name: status %d, want 409", w.Code)
	}
	req = httptest.NewRequest(http.MethodPost, "/api/machines",
		strings.NewReader(fmt.Sprintf(`{"name":"other","public_key":%q}`, key)))
	req.Header.Set("Authorization", "Bearer google:alice")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusConflict {
		t.Errorf("duplicate key: status %d, want 409", w.Code)
	}
}

func TestMachines_RenameDeleteTenantScoping(t *testing.T) {
	_, h := newMachinesTestServer(t)
	m := createMachine(t, h, "google:alice", "laptop", testAuthorizedKey(t))

	// Bob cannot see, rename, or delete Alice's machine.
	for _, method := range []string{http.MethodPatch, http.MethodDelete} {
		req := httptest.NewRequest(method, "/api/machines/"+m.ID, strings.NewReader(`{"name":"stolen"}`))
		req.Header.Set("Authorization", "Bearer google:bob")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != http.StatusNotFound {
			t.Errorf("%s as bob: status %d, want 404", method, w.Code)
		}
	}

	// Alice renames.
	req := httptest.NewRequest(http.MethodPatch, "/api/machines/"+m.ID, strings.NewReader(`{"name":"workbox"}`))
	req.Header.Set("Authorization", "Bearer google:alice")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("rename: status %d, body %s", w.Code, w.Body.String())
	}
	if got := listMachines(t, h, "google:alice"); got[0].Name != "workbox" {
		t.Errorf("name after rename = %q", got[0].Name)
	}

	// Alice deletes.
	req = httptest.NewRequest(http.MethodDelete, "/api/machines/"+m.ID, nil)
	req.Header.Set("Authorization", "Bearer google:alice")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("delete: status %d", w.Code)
	}
	if got := listMachines(t, h, "google:alice"); len(got) != 0 {
		t.Errorf("list after delete = %+v", got)
	}
}

type fakeTunnels struct {
	online map[string]bool
	closed []string
}

func (f *fakeTunnels) Online(id string) bool            { return f.online[id] }
func (f *fakeTunnels) Dial(id string) (net.Conn, error) { return nil, fmt.Errorf("not implemented") }
func (f *fakeTunnels) Close(id string) bool {
	f.closed = append(f.closed, id)
	return f.online[id]
}

func TestMachines_OnlineStatusAndSSHInfo(t *testing.T) {
	s, h := newMachinesTestServer(t)

	// Without a gateway, /api/ssh-info is unavailable.
	req := httptest.NewRequest(http.MethodGet, "/api/ssh-info", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("ssh-info without gate: status %d, want 503", w.Code)
	}

	m := createMachine(t, h, "google:alice", "laptop", testAuthorizedKey(t))

	pub, _, _ := ed25519.GenerateKey(rand.Reader)
	hostPub, _ := ssh.NewPublicKey(pub)
	tunnels := &fakeTunnels{online: map[string]bool{m.ID: true}}
	s.SetSSHGate(tunnels, "relay.example.com:2222", hostPub)

	if got := listMachines(t, h, "google:alice"); !got[0].Online {
		t.Error("expected machine to be online")
	}

	req = httptest.NewRequest(http.MethodGet, "/api/ssh-info", nil)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ssh-info: status %d", w.Code)
	}
	var info map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &info); err != nil {
		t.Fatal(err)
	}
	if info["addr"] != "relay.example.com:2222" || !strings.HasPrefix(info["host_key"], "ssh-ed25519 ") || !strings.HasPrefix(info["fingerprint"], "SHA256:") {
		t.Errorf("ssh-info = %+v", info)
	}

	// Deleting an online machine closes its tunnel.
	req = httptest.NewRequest(http.MethodDelete, "/api/machines/"+m.ID, nil)
	req.Header.Set("Authorization", "Bearer google:alice")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("delete: status %d", w.Code)
	}
	if len(tunnels.closed) != 1 || tunnels.closed[0] != m.ID {
		t.Errorf("tunnel close calls = %v", tunnels.closed)
	}
}
