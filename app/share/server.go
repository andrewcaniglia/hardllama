package share

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hmac"
	"crypto/rand"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	pairingLifetime    = 5 * time.Minute
	maxPairingFailures = 5
	identityFile       = "share-identity.pem"
)

// Device is a device paired with this computer.
type Device struct {
	ID        string
	Name      string
	TokenHash []byte
	CreatedAt time.Time
}

// DeviceStore keeps the paired devices. It must work while chats are
// locked, so sharing keeps working, which is why it only holds token hashes.
type DeviceStore interface {
	ShareDevices() ([]Device, error)
	AddShareDevice(Device) error
}

// Server shares this computer's models with paired devices.
type Server struct {
	// Dir holds this computer's key.
	Dir string
	// Devices keeps the paired devices.
	Devices DeviceStore
	// Engine returns the address of this computer's model server.
	Engine func() *url.URL
	// Addr is where to listen. It defaults to every interface on Port.
	Addr string
	// Logger defaults to slog.Default().
	Logger *slog.Logger

	mu          sync.Mutex
	srv         *http.Server
	addr        net.Addr
	cert        *tls.Certificate
	fingerprint string
	pairing     *pairing
	now         func() time.Time
}

type pairing struct {
	code     string
	expires  time.Time
	failures int
}

func (s *Server) log() *slog.Logger {
	if s.Logger != nil {
		return s.Logger
	}
	return slog.Default()
}

func (s *Server) clock() time.Time {
	if s.now != nil {
		return s.now()
	}
	return time.Now()
}

// Start begins sharing. It does nothing if already sharing.
func (s *Server) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.srv != nil {
		return nil
	}

	if s.cert == nil {
		cert, err := loadOrCreateIdentity(filepath.Join(s.Dir, identityFile))
		if err != nil {
			return fmt.Errorf("load sharing key: %w", err)
		}
		s.cert = cert
		s.fingerprint = Fingerprint(cert.Certificate[0])
	}

	addr := s.Addr
	if addr == "" {
		addr = ":" + Port
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("can't share on port %s: %w", Port, err)
	}

	srv := &http.Server{
		Handler:           s.handler(),
		ReadHeaderTimeout: 10 * time.Second,
		TLSConfig: &tls.Config{
			Certificates: []tls.Certificate{*s.cert},
			MinVersion:   tls.VersionTLS13,
		},
		ErrorLog: slog.NewLogLogger(s.log().Handler(), slog.LevelDebug),
	}
	s.srv = srv
	s.addr = ln.Addr()
	go func() {
		if err := srv.ServeTLS(ln, "", ""); err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.log().Warn("sharing stopped", "error", err)
		}
	}()
	s.log().Info("sharing models", "addr", ln.Addr().String())
	return nil
}

// Stop ends sharing and cancels any pairing in progress.
func (s *Server) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pairing = nil
	if s.srv == nil {
		return nil
	}
	err := s.srv.Close()
	s.srv, s.addr = nil, nil
	return err
}

// Running reports whether this computer is sharing.
func (s *Server) Running() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.srv != nil
}

// ListenAddr is where the server is listening, or nil if it isn't.
func (s *Server) ListenAddr() net.Addr {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.addr
}

// StartPairing makes a new pairing code, replacing any earlier one.
func (s *Server) StartPairing() (code string, expires time.Time, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.srv == nil {
		return "", time.Time{}, errors.New("turn on sharing first")
	}
	p := &pairing{code: newCode(), expires: s.clock().Add(pairingLifetime)}
	s.pairing = p
	return p.code, p.expires, nil
}

func (s *Server) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST "+pairPath, s.pair)

	// Paired devices can only run models. Anything else, such as deleting
	// models or reading this computer's ollama.com account, isn't reachable.
	proxy := s.engineProxy()
	for _, route := range []string{
		"GET /api/tags",
		"POST /api/show",
		"GET /api/version",
		"HEAD /api/version",
		"GET /api/status",
		"POST /api/chat",
		"POST /api/pull",
		"GET /api/experimental/model-recommendations",
	} {
		mux.Handle(route, s.requireDevice(proxy))
	}
	return mux
}

func (s *Server) pair(w http.ResponseWriter, r *http.Request) {
	var req pairRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	s.mu.Lock()
	p := s.pairing
	if p == nil || s.clock().After(p.expires) {
		s.pairing = nil
		s.mu.Unlock()
		http.Error(w, ErrBadCode.Error(), http.StatusForbidden)
		return
	}
	// The proof covers the certificate this device sees. If someone in
	// between presented their own, it won't match ours.
	if !hmac.Equal(req.Proof, clientProof(p.code, s.fingerprint)) {
		p.failures++
		if p.failures >= maxPairingFailures {
			s.pairing = nil
		}
		s.mu.Unlock()
		s.log().Warn("pairing attempt with the wrong code")
		http.Error(w, ErrBadCode.Error(), http.StatusForbidden)
		return
	}
	// Each code pairs one device.
	s.pairing = nil
	code, fingerprint := p.code, s.fingerprint
	s.mu.Unlock()

	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = "Device"
	}
	if len(name) > 64 {
		name = name[:64]
	}
	token := newToken()
	device := Device{ID: newToken()[:16], Name: name, TokenHash: HashToken(token), CreatedAt: time.Now()}
	if err := s.Devices.AddShareDevice(device); err != nil {
		s.log().Error("failed to save paired device", "error", err)
		http.Error(w, "failed to save device", http.StatusInternalServerError)
		return
	}
	s.log().Info("paired a device")

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(pairResponse{Token: token, Proof: serverProof(code, fingerprint, token)})
}

func (s *Server) requireDevice(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || token == "" {
			http.Error(w, ErrNotPaired.Error(), http.StatusUnauthorized)
			return
		}
		devices, err := s.Devices.ShareDevices()
		if err != nil {
			http.Error(w, "failed to check device", http.StatusInternalServerError)
			return
		}
		hash := HashToken(token)
		found := false
		for _, d := range devices {
			if subtle.ConstantTimeCompare(hash, d.TokenHash) == 1 {
				found = true
			}
		}
		if !found {
			http.Error(w, ErrNotPaired.Error(), http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) engineProxy() http.Handler {
	return &httputil.ReverseProxy{
		Rewrite: func(r *httputil.ProxyRequest) {
			target := s.Engine()
			r.SetURL(target)
			// The engine only accepts local names.
			r.Out.Host = "localhost"
			// Only pass on what the engine needs. The token, and anything
			// identifying the device's own UI, stays here.
			r.Out.Header.Del("Authorization")
			r.Out.Header.Del("Cookie")
			r.Out.Header.Del("Origin")
			r.Out.Header.Del("Referer")
		},
		// Stream replies as they're generated.
		FlushInterval: -1,
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			s.log().Warn("sharing: model server unavailable", "error", err)
			http.Error(w, "the model server on this computer isn't running", http.StatusBadGateway)
		},
	}
}

// loadOrCreateIdentity loads this computer's sharing key and certificate,
// making them the first time. Devices pin the certificate, so it must stay
// the same.
func loadOrCreateIdentity(path string) (*tls.Certificate, error) {
	data, err := os.ReadFile(path)
	if err == nil {
		cert, err := tls.X509KeyPair(data, data)
		if err != nil {
			return nil, fmt.Errorf("%s is damaged: %w", path, err)
		}
		return &cert, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 127))
	if err != nil {
		return nil, err
	}
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "HardLlama"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().AddDate(100, 0, 0),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return nil, err
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, err
	}
	data = append(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}),
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})...)

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	// O_EXCL so two copies starting at once can't end up with different keys.
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		return loadOrCreateIdentity(path)
	}
	if err != nil {
		return nil, err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(path)
		return nil, err
	}
	if err := f.Close(); err != nil {
		return nil, err
	}
	cert, err := tls.X509KeyPair(data, data)
	if err != nil {
		return nil, err
	}
	return &cert, nil
}
