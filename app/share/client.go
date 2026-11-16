package share

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Pairing is what a device keeps after pairing with a sharing computer.
// The token is a secret, so it must be stored encrypted.
type Pairing struct {
	Address     string `json:"address"`
	Fingerprint string `json:"fingerprint"`
	Token       string `json:"token"`
}

// NormalizeAddress accepts a host, optionally with a port, and returns it
// with the sharing port filled in.
func NormalizeAddress(address string) (string, error) {
	address = strings.TrimSpace(address)
	address = strings.TrimPrefix(address, "https://")
	address = strings.TrimSuffix(address, "/")
	if address == "" || strings.ContainsAny(address, "/?#@ ") {
		return "", errors.New("enter the other computer's address, such as 192.168.1.20 or laptop.local")
	}
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		// No port given.
		host, port = strings.Trim(address, "[]"), Port
	}
	if host == "" || port == "" {
		return "", errors.New("enter the other computer's address, such as 192.168.1.20 or laptop.local")
	}
	return net.JoinHostPort(host, port), nil
}

// URL is the base address requests go to.
func (p Pairing) URL() *url.URL {
	return &url.URL{Scheme: "https", Host: p.Address}
}

// Transport connects only to the paired computer, checking its key on every
// connection, and sends this device's token.
func (p Pairing) Transport() http.RoundTripper {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.TLSClientConfig = pinnedTLS(func(fingerprint string) error {
		if fingerprint != p.Fingerprint {
			return ErrPinMismatch
		}
		return nil
	})
	return &tokenTransport{base: t, token: p.Token}
}

// pinnedTLS trusts exactly the certificate check accepts, instead of
// certificate authorities: the sharing computer's certificate is self-signed.
func pinnedTLS(check func(fingerprint string) error) *tls.Config {
	return &tls.Config{
		MinVersion: tls.VersionTLS13,
		// Verification is done by VerifyConnection against the pin.
		InsecureSkipVerify: true,
		VerifyConnection: func(cs tls.ConnectionState) error {
			return check(peerFingerprint(cs))
		},
	}
}

type tokenTransport struct {
	base  http.RoundTripper
	token string
}

func (t *tokenTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	r := req.Clone(req.Context())
	r.Header.Set("Authorization", "Bearer "+t.token)
	return t.base.RoundTrip(r)
}

// Pair pairs this device with the sharing computer at address, using the
// code shown there. name is how this device appears on the other computer.
func Pair(ctx context.Context, address, code, name string) (Pairing, error) {
	address, err := NormalizeAddress(address)
	if err != nil {
		return Pairing{}, err
	}
	if len(normalizeCode(code)) != codeLength {
		return Pairing{}, fmt.Errorf("the pairing code has %d letters and numbers", codeLength)
	}

	// Record the certificate of the one connection the request is sent on.
	var (
		mu          sync.Mutex
		fingerprint string
	)
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.DisableKeepAlives = true
	t.TLSClientConfig = pinnedTLS(func(fp string) error {
		mu.Lock()
		defer mu.Unlock()
		if fingerprint != "" && fingerprint != fp {
			return ErrPinMismatch
		}
		fingerprint = fp
		return nil
	})
	client := &http.Client{Transport: t, Timeout: 30 * time.Second}

	// The fingerprint is only known once connected, so connect first.
	conn, err := tls.DialWithDialer(&net.Dialer{Timeout: 15 * time.Second}, "tcp", address, t.TLSClientConfig)
	if err != nil {
		return Pairing{}, fmt.Errorf("can't reach HardLlama at %s. Check the address, and that sharing is on there: %w", address, err)
	}
	conn.Close()
	mu.Lock()
	seen := fingerprint
	mu.Unlock()

	body, _ := json.Marshal(pairRequest{Name: name, Proof: clientProof(code, seen)})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, (&url.URL{Scheme: "https", Host: address, Path: pairPath}).String(), bytes.NewReader(body))
	if err != nil {
		return Pairing{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		if errors.Is(err, ErrPinMismatch) {
			return Pairing{}, errors.New("the other computer's key changed during pairing, so someone may be in between. Pairing stopped")
		}
		return Pairing{}, fmt.Errorf("can't reach HardLlama at %s: %w", address, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusForbidden {
		return Pairing{}, ErrBadCode
	}
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return Pairing{}, fmt.Errorf("pairing failed: %s", strings.TrimSpace(string(msg)))
	}

	var out pairResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&out); err != nil {
		return Pairing{}, fmt.Errorf("pairing failed: %w", err)
	}
	// Only the real sharing computer knows the code, so only it can prove
	// it for the certificate this device saw.
	if out.Token == "" || !hmac.Equal(out.Proof, serverProof(code, seen, out.Token)) {
		return Pairing{}, errors.New("the other computer couldn't prove it showed this code, so someone may be in between. Pairing stopped")
	}
	return Pairing{Address: address, Fingerprint: seen, Token: out.Token}, nil
}
