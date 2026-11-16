//go:build windows || darwin

package ui

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/ollama/ollama/app/share"
	"github.com/ollama/ollama/app/store"
	"github.com/ollama/ollama/app/ui/responses"
)

const testPassword = "correct horse battery"

// fakeEngine stands in for a model server and records what it was sent.
type fakeEngine struct {
	*httptest.Server
	mu      sync.Mutex
	paths   []string
	headers []http.Header
}

func newFakeEngine(t *testing.T, name string) *fakeEngine {
	t.Helper()
	f := &fakeEngine{}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.paths = append(f.paths, r.URL.Path)
		f.headers = append(f.headers, r.Header.Clone())
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/version":
			w.Write([]byte(`{"version":"` + name + `"}`))
		case "/api/me":
			w.Write([]byte(`{"name":"` + name + `-account"}`))
		default:
			w.Write([]byte(`{"models":[{"name":"` + name + `-model"}]}`))
		}
	}))
	t.Cleanup(f.Close)
	return f
}

func (f *fakeEngine) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.paths)
}

type testApp struct {
	server  *Server
	handler http.Handler
}

func newTestApp(t *testing.T, sharing *share.Server) *testApp {
	t.Helper()
	st := &store.Store{DBPath: filepath.Join(t.TempDir(), "db.sqlite")}
	t.Cleanup(func() { st.Close() })
	if err := st.SetupVault(testPassword); err != nil {
		t.Fatal(err)
	}
	s := &Server{Store: st, Share: sharing, Dev: true}
	return &testApp{server: s, handler: s.Handler()}
}

func (a *testApp) request(method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	rec := httptest.NewRecorder()
	a.handler.ServeHTTP(rec, req)
	return rec
}

// newSharingComputer is another HardLlama sharing its engine.
func newSharingComputer(t *testing.T, engine *fakeEngine) (*testApp, *share.Server) {
	t.Helper()
	u, _ := url.Parse(engine.URL)
	sharing := &share.Server{Dir: t.TempDir(), Engine: func() *url.URL { return u }, Addr: "127.0.0.1:0"}
	app := newTestApp(t, sharing)
	sharing.Devices = app.server.Store
	t.Cleanup(func() { sharing.Stop() })
	return app, sharing
}

func TestPairedModelServer(t *testing.T) {
	local := newFakeEngine(t, "local")
	remote := newFakeEngine(t, "remote")
	t.Setenv("OLLAMA_HOST", local.URL)

	host, sharing := newSharingComputer(t, remote)
	if rec := host.request(http.MethodPost, "/api/v1/share", `{"enabled":true}`); rec.Code != http.StatusOK {
		t.Fatalf("turn on sharing: %d %s", rec.Code, rec.Body)
	}
	rec := host.request(http.MethodPost, "/api/v1/share/pair", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("pairing code: %d %s", rec.Code, rec.Body)
	}
	var code responses.PairingCodeResponse
	json.Unmarshal(rec.Body.Bytes(), &code)
	address := sharing.ListenAddr().String()

	mac := newTestApp(t, nil)
	tags := func(t *testing.T, want string) {
		t.Helper()
		rec := mac.request(http.MethodGet, "/api/tags", "")
		if !strings.Contains(rec.Body.String(), want+"-model") {
			t.Fatalf("model list came from the wrong computer, want %s: %d %s", want, rec.Code, rec.Body)
		}
	}
	version := func(t *testing.T, want string) {
		t.Helper()
		v, err := mac.server.inferenceClient().Version(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if v != want {
			t.Fatalf("chats went to the %s computer, want %s", v, want)
		}
	}

	tags(t, "local")
	version(t, "local")

	// The UI's login cookie, and headers revealing its port, never leave the
	// UI server.
	withUIHeaders := func() *http.Request {
		req := httptest.NewRequest(http.MethodGet, "/api/tags", nil)
		req.Header.Set("Cookie", "token=ui-secret")
		req.Header.Set("Origin", "http://127.0.0.1:54321")
		req.Header.Set("Referer", "http://127.0.0.1:54321/c/chat")
		return req
	}
	noUIHeaders := func(t *testing.T, f *fakeEngine) {
		t.Helper()
		f.mu.Lock()
		last := f.headers[len(f.headers)-1]
		f.mu.Unlock()
		for _, h := range []string{"Cookie", "Origin", "Referer", "Authorization"} {
			if v := last.Get(h); v != "" {
				t.Errorf("%s passed on: %q", h, v)
			}
		}
	}
	mac.handler.ServeHTTP(httptest.NewRecorder(), withUIHeaders())
	noUIHeaders(t, local)

	if rec := mac.request(http.MethodPost, "/api/v1/model-server/pair", `{"address":"`+address+`","code":"AAAA-AAAA-AAAA-AAAA"}`); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "code") {
		t.Fatalf("wrong code: %d %s", rec.Code, rec.Body)
	}
	rec = mac.request(http.MethodPost, "/api/v1/model-server/pair", `{"address":"`+address+`","code":"`+code.Code+`"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("pair: %d %s", rec.Code, rec.Body)
	}
	if rec := mac.request(http.MethodGet, "/api/v1/model-server", ""); !strings.Contains(rec.Body.String(), address) {
		t.Fatalf("pairing not saved: %s", rec.Body)
	}

	tags(t, "remote")
	version(t, "remote")

	mac.handler.ServeHTTP(httptest.NewRecorder(), withUIHeaders())
	noUIHeaders(t, remote)

	// The ollama.com account is always this computer's.
	if rec := mac.request(http.MethodPost, "/api/me", ""); !strings.Contains(rec.Body.String(), "local-account") {
		t.Fatalf("account came from the other computer: %s", rec.Body)
	}

	// The sharing computer lists the device, without its token.
	rec = host.request(http.MethodGet, "/api/v1/share", "")
	var status responses.ShareResponse
	json.Unmarshal(rec.Body.Bytes(), &status)
	if !status.Enabled || !status.Running || len(status.Devices) != 1 || !strings.HasPrefix(status.Devices[0].Name, "Mac") && !strings.HasPrefix(status.Devices[0].Name, "Windows") {
		t.Fatalf("unexpected share status: %s", rec.Body)
	}

	// Locking the sharing computer doesn't stop sharing, but does stop
	// changes to it.
	host.server.Store.LockVault()
	version(t, "remote")
	if rec := host.request(http.MethodPost, "/api/v1/share/pair", ""); rec.Code != http.StatusForbidden {
		t.Fatalf("pairing code made while locked: %d", rec.Code)
	}
	if rec := host.request(http.MethodDelete, "/api/v1/share/devices/"+status.Devices[0].ID, ""); rec.Code != http.StatusForbidden {
		t.Fatalf("device removed while locked: %d", rec.Code)
	}
	if rec := host.request(http.MethodPost, "/api/v1/share", `{"enabled":false}`); rec.Code != http.StatusForbidden {
		t.Fatalf("sharing turned off while locked: %d", rec.Code)
	}

	// Locking this computer hides the pairing, so nothing is sent.
	mac.server.Store.LockVault()
	before := remote.count()
	tags(t, "local")
	if remote.count() != before {
		t.Fatal("request went to the other computer while locked")
	}
	if rec := mac.request(http.MethodPost, "/api/v1/model-server/pair", `{"address":"`+address+`","code":"`+code.Code+`"}`); rec.Code != http.StatusForbidden {
		t.Fatalf("paired while locked: %d", rec.Code)
	}
	if rec := mac.request(http.MethodDelete, "/api/v1/model-server", ""); rec.Code != http.StatusForbidden {
		t.Fatalf("unpaired while locked: %d", rec.Code)
	}
	if err := mac.server.Store.UnlockVault(testPassword); err != nil {
		t.Fatal(err)
	}
	tags(t, "remote")

	// Removing the device on the sharing computer cuts it off.
	if err := host.server.Store.UnlockVault(testPassword); err != nil {
		t.Fatal(err)
	}
	if rec := host.request(http.MethodDelete, "/api/v1/share/devices/"+status.Devices[0].ID, ""); rec.Code != http.StatusOK {
		t.Fatalf("remove device: %d %s", rec.Code, rec.Body)
	}
	if _, err := mac.server.inferenceClient().Version(context.Background()); err == nil {
		t.Fatal("removed device still served")
	}

	// Unpairing goes back to this computer.
	if rec := mac.request(http.MethodDelete, "/api/v1/model-server", ""); rec.Code != http.StatusOK {
		t.Fatalf("unpair: %d %s", rec.Code, rec.Body)
	}
	tags(t, "local")
	version(t, "local")

	// Turning sharing off stops it.
	if rec := host.request(http.MethodPost, "/api/v1/share", `{"enabled":false}`); rec.Code != http.StatusOK {
		t.Fatalf("turn off sharing: %d %s", rec.Code, rec.Body)
	}
	if sharing.Running() {
		t.Fatal("still sharing")
	}
}

// A website that points its own name at this computer (DNS rebinding) must
// not reach the API, even with a valid login cookie.
func TestRefusesOtherHostnames(t *testing.T) {
	st := &store.Store{DBPath: filepath.Join(t.TempDir(), "db.sqlite")}
	t.Cleanup(func() { st.Close() })
	s := &Server{Store: st, Token: "ui-secret"}
	handler := s.Handler()

	for host, want := range map[string]int{
		"attacker.example:54321": http.StatusForbidden,
		"attacker.example":       http.StatusForbidden,
		"192.168.1.20:54321":     http.StatusForbidden,
		"127.0.0.1:54321":        http.StatusOK,
		"localhost:54321":        http.StatusOK,
	} {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/vault", nil)
		req.Host = host
		req.AddCookie(&http.Cookie{Name: "token", Value: "ui-secret"})
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != want {
			t.Errorf("%s: got %d, want %d", host, rec.Code, want)
		}
	}
}
