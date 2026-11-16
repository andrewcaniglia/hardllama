// Package share lets HardLlama on one computer run models for paired
// devices, and lets a device pair with and use another computer.
//
// The sharing computer listens on Port with TLS, using its own self-signed
// certificate. A device pairs once with a short-lived code shown on the
// sharing computer. Each side proves it knows the code, bound to the
// certificate the device actually sees, so neither a stranger nor anyone
// in between, such as the local network or a VPN provider, can pair or
// impersonate the sharing computer without the code. The device then pins
// the certificate and uses a random token, which the sharing computer only
// stores hashed.
package share

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base32"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
)

// Port is where a sharing computer listens.
const Port = "11443"

// pairPath is the endpoint devices pair with.
const pairPath = "/hardllama/pair"

const (
	clientProofContext = "hardllama-pair-client-v1"
	serverProofContext = "hardllama-pair-server-v1"

	// codeLength is in base32 characters, 5 bits each.
	codeLength = 16
)

var (
	ErrBadCode     = errors.New("the pairing code is wrong or has expired. Make a new one on the other computer")
	ErrNotPaired   = errors.New("this device isn't paired")
	ErrPinMismatch = errors.New("the other computer's key has changed, so HardLlama won't connect. If you reinstalled HardLlama there, pair again")
)

var codeEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

// newCode returns a random pairing code, grouped for reading.
func newCode() string {
	b := make([]byte, codeLength*5/8)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	c := codeEncoding.EncodeToString(b)
	return c[0:4] + "-" + c[4:8] + "-" + c[8:12] + "-" + c[12:16]
}

// normalizeCode accepts a code as typed: any case, with or without dashes
// and spaces.
func normalizeCode(code string) string {
	code = strings.ToUpper(code)
	return strings.Map(func(r rune) rune {
		if r == '-' || r == ' ' {
			return -1
		}
		return r
	}, code)
}

func proof(code, context string, parts ...[]byte) []byte {
	mac := hmac.New(sha256.New, []byte(normalizeCode(code)))
	mac.Write([]byte(context))
	for _, p := range parts {
		mac.Write([]byte{0})
		mac.Write(p)
	}
	return mac.Sum(nil)
}

func clientProof(code, fingerprint string) []byte {
	return proof(code, clientProofContext, []byte(fingerprint))
}

func serverProof(code, fingerprint, token string) []byte {
	return proof(code, serverProofContext, []byte(fingerprint), []byte(token))
}

// Fingerprint identifies a certificate.
func Fingerprint(cert []byte) string {
	sum := sha256.Sum256(cert)
	return hex.EncodeToString(sum[:])
}

func peerFingerprint(cs tls.ConnectionState) string {
	if len(cs.PeerCertificates) == 0 {
		return ""
	}
	return Fingerprint(cs.PeerCertificates[0].Raw)
}

func newToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// HashToken is how the sharing computer stores a device's token.
func HashToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

type pairRequest struct {
	Name  string `json:"name"`
	Proof []byte `json:"proof"`
}

type pairResponse struct {
	Token string `json:"token"`
	Proof []byte `json:"proof"`
}
