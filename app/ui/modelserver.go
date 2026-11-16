//go:build windows || darwin

package ui

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"runtime"
	"sync"
	"time"

	"github.com/ollama/ollama/app/share"
	"github.com/ollama/ollama/app/store"
	"github.com/ollama/ollama/app/ui/responses"
	"github.com/ollama/ollama/envconfig"
)

// HardLlama can run models on another computer it's paired with, while chats
// stay saved on this one. See package share for how pairing keeps the
// connection private on any network.

// modelServer returns where models run: the paired computer, or this one.
// transport is nil for this one.
func (s *Server) modelServer() (target *url.URL, transport http.RoundTripper, remote bool) {
	if s.Store != nil {
		p, err := s.Store.ModelServer()
		if err != nil {
			s.log().Warn("failed to read paired computer, using this one", "error", err)
		} else if p != nil {
			return p.URL(), pairingTransport(*p), true
		}
	}
	return envconfig.ConnectableHost(), nil, false
}

// pairingTransports reuses one transport, and so its connections, per
// pairing.
var pairingTransports sync.Map

func pairingTransport(p share.Pairing) http.RoundTripper {
	if t, ok := pairingTransports.Load(p); ok {
		return t.(http.RoundTripper)
	}
	t, _ := pairingTransports.LoadOrStore(p, p.Transport())
	return t.(http.RoundTripper)
}

// deviceName is how this computer appears on the computer it pairs with. It
// deliberately doesn't use the computer's name, which often includes the
// owner's.
func deviceName() string {
	kind := "Computer"
	switch runtime.GOOS {
	case "darwin":
		kind = "Mac"
	case "windows":
		kind = "Windows PC"
	}
	return kind + ", paired " + time.Now().Format("Jan 2, 2006")
}

func writeJSON(w http.ResponseWriter, status int, v any) error {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	return json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, err error) error {
	return writeJSON(w, status, responses.Error{Error: err.Error()})
}

// storeError reports a change refused because chats are locked as such, and
// passes other errors on.
func storeError(w http.ResponseWriter, err error) error {
	if errors.Is(err, store.ErrVaultLocked) {
		return writeError(w, http.StatusForbidden, err)
	}
	return err
}

// requireUnlocked refuses a request while chats are locked. ok is false if
// it responded.
func (s *Server) requireUnlocked(w http.ResponseWriter) (ok bool, err error) {
	_, locked, err := s.Store.VaultStatus()
	if err != nil {
		return false, err
	}
	if locked {
		return false, writeError(w, http.StatusForbidden, store.ErrVaultLocked)
	}
	return true, nil
}

func (s *Server) getModelServer(w http.ResponseWriter, r *http.Request) error {
	p, err := s.Store.ModelServer()
	if err != nil {
		return err
	}
	var resp responses.ModelServerResponse
	if p != nil {
		resp.Address = p.Address
	}
	return writeJSON(w, http.StatusOK, resp)
}

func (s *Server) pairModelServer(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		Address string `json:"address"`
		Code    string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return fmt.Errorf("invalid request body: %w", err)
	}
	if ok, err := s.requireUnlocked(w); !ok {
		return err
	}

	p, err := share.Pair(r.Context(), req.Address, req.Code, deviceName())
	if err != nil {
		return writeError(w, http.StatusBadRequest, err)
	}
	if err := s.Store.SetModelServer(&p); err != nil {
		return storeError(w, err)
	}
	return writeJSON(w, http.StatusOK, responses.ModelServerResponse{Address: p.Address})
}

func (s *Server) unpairModelServer(w http.ResponseWriter, r *http.Request) error {
	if err := s.Store.SetModelServer(nil); err != nil {
		return storeError(w, err)
	}
	return writeJSON(w, http.StatusOK, responses.ModelServerResponse{})
}

// Sharing this computer's models.

func (s *Server) getShare(w http.ResponseWriter, r *http.Request) error {
	enabled, err := s.Store.ShareEnabled()
	if err != nil {
		return err
	}
	devices, err := s.Store.ShareDevices()
	if err != nil {
		return err
	}
	resp := responses.ShareResponse{
		Enabled:   enabled,
		Running:   s.Share != nil && s.Share.Running(),
		Port:      share.Port,
		Addresses: localAddresses(),
		Devices:   []responses.ShareDevice{},
	}
	for _, d := range devices {
		resp.Devices = append(resp.Devices, responses.ShareDevice{ID: d.ID, Name: d.Name, CreatedAt: d.CreatedAt})
	}
	return writeJSON(w, http.StatusOK, resp)
}

func (s *Server) setShare(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return fmt.Errorf("invalid request body: %w", err)
	}
	if s.Share == nil {
		return writeError(w, http.StatusBadRequest, errors.New("sharing isn't available"))
	}
	if err := s.Store.SetShareEnabled(req.Enabled); err != nil {
		return storeError(w, err)
	}
	if req.Enabled {
		if err := s.Share.Start(); err != nil {
			s.Store.SetShareEnabled(false)
			return writeError(w, http.StatusBadRequest, err)
		}
	} else if err := s.Share.Stop(); err != nil {
		return err
	}
	return s.getShare(w, r)
}

func (s *Server) startPairing(w http.ResponseWriter, r *http.Request) error {
	if ok, err := s.requireUnlocked(w); !ok {
		return err
	}
	if s.Share == nil {
		return writeError(w, http.StatusBadRequest, errors.New("sharing isn't available"))
	}
	code, expires, err := s.Share.StartPairing()
	if err != nil {
		return writeError(w, http.StatusBadRequest, err)
	}
	return writeJSON(w, http.StatusOK, responses.PairingCodeResponse{Code: code, ExpiresAt: expires})
}

func (s *Server) removeShareDevice(w http.ResponseWriter, r *http.Request) error {
	if err := s.Store.RemoveShareDevice(r.PathValue("id")); err != nil {
		return storeError(w, err)
	}
	return s.getShare(w, r)
}

// localAddresses lists this computer's network addresses, to enter on the
// other computer.
func localAddresses() []string {
	addresses := []string{}
	ifaces, err := net.Interfaces()
	if err != nil {
		return addresses
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			ipnet, ok := a.(*net.IPNet)
			if !ok || ipnet.IP.To4() == nil || ipnet.IP.IsLinkLocalUnicast() {
				continue
			}
			addresses = append(addresses, ipnet.IP.String())
		}
	}
	return addresses
}
