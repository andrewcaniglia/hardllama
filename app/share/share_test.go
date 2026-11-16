package share

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type memoryDevices struct {
	mu      sync.Mutex
	devices []Device
}

func (m *memoryDevices) ShareDevices() ([]Device, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]Device(nil), m.devices...), nil
}

func (m *memoryDevices) AddShareDevice(d Device) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.devices = append(m.devices, d)
	return nil
}

func (m *memoryDevices) clear() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.devices = nil
}

// engine stands in for the model server and records what reached it.
type engine struct {
	*httptest.Server
	mu       sync.Mutex
	requests []*http.Request
}

func newEngine(t *testing.T) *engine {
	t.Helper()
	e := &engine{}
	e.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		e.mu.Lock()
		e.requests = append(e.requests, r.Clone(context.Background()))
		e.mu.Unlock()
		if r.URL.Path == "/api/chat" {
			// Streams a reply in two parts.
			w.Write([]byte("first\n"))
			w.(http.Flusher).Flush()
			time.Sleep(300 * time.Millisecond)
			w.Write([]byte("second\n"))
			return
		}
		w.Write([]byte(`{"models":[]}`))
	}))
	t.Cleanup(e.Close)
	return e
}

func (e *engine) seen() []*http.Request {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]*http.Request(nil), e.requests...)
}

func newSharing(t *testing.T, dir string, e *engine, devices *memoryDevices) *Server {
	t.Helper()
	u, _ := url.Parse(e.URL)
	s := &Server{Dir: dir, Devices: devices, Engine: func() *url.URL { return u }, Addr: "127.0.0.1:0"}
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Stop() })
	return s
}

func addr(s *Server) string { return s.ListenAddr().String() }

func pair(t *testing.T, s *Server, address string) (Pairing, error) {
	t.Helper()
	code, _, err := s.StartPairing()
	if err != nil {
		t.Fatal(err)
	}
	return Pair(context.Background(), address, code, "Mac")
}

func get(p Pairing, path string) (*http.Response, error) {
	client := &http.Client{Transport: p.Transport(), Timeout: 5 * time.Second}
	return client.Get(p.URL().String() + path)
}

func TestPairAndUse(t *testing.T) {
	e := newEngine(t)
	devices := &memoryDevices{}
	s := newSharing(t, t.TempDir(), e, devices)

	code, _, err := s.StartPairing()
	if err != nil {
		t.Fatal(err)
	}
	// Codes are accepted as typed.
	typed := strings.ToLower(strings.ReplaceAll(code, "-", " "))
	p, err := Pair(context.Background(), addr(s), typed, "Mac")
	if err != nil {
		t.Fatal(err)
	}
	if len(devices.devices) != 1 || devices.devices[0].Name != "Mac" {
		t.Fatalf("device not saved: %+v", devices.devices)
	}
	if string(devices.devices[0].TokenHash) == p.Token {
		t.Fatal("token stored as is")
	}

	req, _ := http.NewRequest(http.MethodGet, p.URL().String()+"/api/tags", nil)
	req.Header.Set("Cookie", "token=ui-secret")
	req.Header.Set("Origin", "http://127.0.0.1:54321")
	req.Header.Set("Referer", "http://127.0.0.1:54321/c/chat")
	resp, err := (&http.Client{Transport: p.Transport()}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("model list: %d", resp.StatusCode)
	}
	reqs := e.seen()
	if len(reqs) != 1 {
		t.Fatalf("expected 1 request at the engine, got %d", len(reqs))
	}
	if reqs[0].Host != "localhost" {
		t.Errorf("engine saw Host %q", reqs[0].Host)
	}
	for _, h := range []string{"Authorization", "Cookie", "Origin", "Referer"} {
		if v := reqs[0].Header.Get(h); v != "" {
			t.Errorf("%s passed on to the engine: %q", h, v)
		}
	}

	// The same code doesn't pair a second device.
	if _, err := Pair(context.Background(), addr(s), code, "Other"); !errors.Is(err, ErrBadCode) {
		t.Fatalf("code reused: %v", err)
	}
}

func TestPairRejectsWrongAndExpiredCodes(t *testing.T) {
	e := newEngine(t)
	devices := &memoryDevices{}
	s := newSharing(t, t.TempDir(), e, devices)

	code, _, err := s.StartPairing()
	if err != nil {
		t.Fatal(err)
	}
	wrong := "AAAA-AAAA-AAAA-AAAA"
	if code == wrong {
		t.Skip("astronomically unlucky")
	}
	for range maxPairingFailures {
		if _, err := Pair(context.Background(), addr(s), wrong, "Guess"); !errors.Is(err, ErrBadCode) {
			t.Fatalf("wrong code: %v", err)
		}
	}
	// Too many wrong guesses cancel the code.
	if _, err := Pair(context.Background(), addr(s), code, "Mac"); !errors.Is(err, ErrBadCode) {
		t.Fatalf("code still worked after %d wrong guesses: %v", maxPairingFailures, err)
	}

	now := time.Now()
	s.now = func() time.Time { return now }
	code, _, err = s.StartPairing()
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(pairingLifetime + time.Second)
	if _, err := Pair(context.Background(), addr(s), code, "Mac"); !errors.Is(err, ErrBadCode) {
		t.Fatalf("expired code: %v", err)
	}
	if len(devices.devices) != 0 {
		t.Fatalf("devices paired: %+v", devices.devices)
	}
}

func TestOnlyPairedDevicesRunModels(t *testing.T) {
	e := newEngine(t)
	devices := &memoryDevices{}
	s := newSharing(t, t.TempDir(), e, devices)
	p, err := pair(t, s, addr(s))
	if err != nil {
		t.Fatal(err)
	}

	for name, token := range map[string]string{"no token": "", "wrong token": "not-a-token"} {
		t.Run(name, func(t *testing.T) {
			resp, err := get(Pairing{Address: p.Address, Fingerprint: p.Fingerprint, Token: token}, "/api/tags")
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			if resp.StatusCode != http.StatusUnauthorized {
				t.Fatalf("got %d", resp.StatusCode)
			}
		})
	}

	// Only running models is allowed.
	client := &http.Client{Transport: p.Transport()}
	for _, route := range []struct{ method, path string }{
		{http.MethodDelete, "/api/delete"},
		{http.MethodPost, "/api/create"},
		{http.MethodPost, "/api/push"},
		{http.MethodPost, "/api/me"},
		{http.MethodPost, "/api/signout"},
		{http.MethodGet, "/api/ps"},
		{http.MethodGet, "/"},
	} {
		req, _ := http.NewRequest(route.method, p.URL().String()+route.path, nil)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode < 400 {
			t.Errorf("%s %s allowed: %d", route.method, route.path, resp.StatusCode)
		}
	}
	if n := len(e.seen()); n != 0 {
		t.Fatalf("%d requests reached the engine", n)
	}

	// Removing the device on the sharing computer cuts it off.
	devices.clear()
	resp, err := get(p, "/api/tags")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("removed device still allowed: %d", resp.StatusCode)
	}
}

// mitm sits between a device and the sharing computer with its own
// certificate, decrypting and re-encrypting everything.
func mitm(t *testing.T, upstream string) string {
	t.Helper()
	cert, err := loadOrCreateIdentity(filepath.Join(t.TempDir(), identityFile))
	if err != nil {
		t.Fatal(err)
	}
	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{*cert}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			down, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer down.Close()
				up, err := tls.Dial("tcp", upstream, &tls.Config{InsecureSkipVerify: true})
				if err != nil {
					return
				}
				defer up.Close()
				go io.Copy(up, down)
				io.Copy(down, up)
			}()
		}
	}()
	return ln.Addr().String()
}

func TestPairDetectsSomeoneInBetween(t *testing.T) {
	e := newEngine(t)
	devices := &memoryDevices{}
	s := newSharing(t, t.TempDir(), e, devices)

	// Relaying the real pairing: the sharing computer sees a proof for the
	// wrong certificate and refuses.
	if _, err := pair(t, s, mitm(t, addr(s))); err == nil {
		t.Fatal("paired through someone in between")
	}
	if len(devices.devices) != 0 {
		t.Fatal("device paired through someone in between")
	}

	// Pretending to be the sharing computer without the code: the device
	// gets no valid proof and refuses.
	impostor := &Server{Dir: t.TempDir(), Devices: &memoryDevices{}, Engine: func() *url.URL { u, _ := url.Parse(e.URL); return u }, Addr: "127.0.0.1:0"}
	if err := impostor.Start(); err != nil {
		t.Fatal(err)
	}
	defer impostor.Stop()
	if _, _, err := impostor.StartPairing(); err != nil {
		t.Fatal(err)
	}
	realCode, _, err := s.StartPairing()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Pair(context.Background(), addr(impostor), realCode, "Mac"); err == nil {
		t.Fatal("paired with an impostor")
	}

	// After pairing, a different certificate at the same address is refused.
	p, err := pair(t, s, addr(s))
	if err != nil {
		t.Fatal(err)
	}
	p.Address = addr(impostor)
	if _, err := get(p, "/api/tags"); !errors.Is(err, ErrPinMismatch) {
		t.Fatalf("connected to a different certificate: %v", err)
	}
	p.Address = mitm(t, addr(s))
	if _, err := get(p, "/api/tags"); !errors.Is(err, ErrPinMismatch) {
		t.Fatalf("connected through someone in between: %v", err)
	}
}

func TestIdentitySurvivesRestart(t *testing.T) {
	e := newEngine(t)
	dir := t.TempDir()
	devices := &memoryDevices{}
	s := newSharing(t, dir, e, devices)
	p, err := pair(t, s, addr(s))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Stop(); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(filepath.Join(dir, identityFile))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("key file mode %v, want 0600", info.Mode().Perm())
	}

	restarted := newSharing(t, dir, e, devices)
	p.Address = addr(restarted)
	resp, err := get(p, "/api/tags")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("paired device refused after restart: %d", resp.StatusCode)
	}
}

func TestRepliesStream(t *testing.T) {
	e := newEngine(t)
	s := newSharing(t, t.TempDir(), e, &memoryDevices{})
	p, err := pair(t, s, addr(s))
	if err != nil {
		t.Fatal(err)
	}

	client := &http.Client{Transport: p.Transport()}
	start := time.Now()
	resp, err := client.Post(p.URL().String()+"/api/chat", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	line, err := bufio.NewReader(resp.Body).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	if line != "first\n" || time.Since(start) > 250*time.Millisecond {
		t.Fatalf("reply wasn't streamed: %q after %v", line, time.Since(start))
	}
}

func TestPairingNeedsSharingOn(t *testing.T) {
	s := &Server{Dir: t.TempDir(), Devices: &memoryDevices{}}
	if _, _, err := s.StartPairing(); err == nil {
		t.Fatal("made a pairing code while not sharing")
	}
}

func TestNormalizeAddress(t *testing.T) {
	for in, want := range map[string]string{
		"192.168.1.20":            "192.168.1.20:11443",
		"192.168.1.20:9000":       "192.168.1.20:9000",
		"https://laptop.local/":   "laptop.local:11443",
		"[fd00::1]":               "[fd00::1]:11443",
		"[fd00::1]:9000":          "[fd00::1]:9000",
		"10.8.0.1":                "10.8.0.1:11443",
		"laptop.tail1234.ts.net":  "laptop.tail1234.ts.net:11443",
		"  laptop.local:11443   ": "laptop.local:11443",
	} {
		got, err := NormalizeAddress(in)
		if err != nil || got != want {
			t.Errorf("%q: got %q %v, want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"", "http://x/y", "user@host", "host/path", "a b"} {
		if got, err := NormalizeAddress(in); err == nil {
			t.Errorf("%q accepted as %q", in, got)
		}
	}
}
