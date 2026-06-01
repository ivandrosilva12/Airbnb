package push

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/airhost/backend/internal/application/port"
	"github.com/airhost/backend/internal/domain/pushtoken"
)

// APNsConfig holds the inputs to build an APNs sender. TeamID is the Apple
// developer team id; KeyID identifies the .p8 signing key uploaded in the
// developer portal; PrivateKeyPEM is the .p8 contents; BundleID is the app's
// CFBundleIdentifier and becomes the apns-topic header.
type APNsConfig struct {
	TeamID        string
	KeyID         string
	PrivateKeyPEM []byte
	BundleID      string
	// BaseURL overrides the APNs endpoint (used by tests). Defaults to the
	// production server; use https://api.sandbox.push.apple.com for the sandbox
	// from the environment.
	BaseURL    string
	HTTPClient *http.Client
	Now        func() time.Time
}

// APNsSender delivers iOS pushes via Apple's HTTP/2 endpoint, signing requests
// with an ES256 JWT minted from the .p8 key. The provider token is cached for
// ~50 minutes (Apple rotates it at 1 h).
type APNsSender struct {
	cfg        APNsConfig
	privKey    *ecdsa.PrivateKey
	httpClient *http.Client
	now        func() time.Time

	tokenMu     sync.Mutex
	bearer      string
	bearerIssue time.Time
}

// NewAPNsSender parses the .p8 key and returns a ready-to-use sender.
func NewAPNsSender(cfg APNsConfig) (*APNsSender, error) {
	if cfg.TeamID == "" || cfg.KeyID == "" || cfg.BundleID == "" {
		return nil, errors.New("apns: TeamID, KeyID and BundleID are required")
	}
	if len(cfg.PrivateKeyPEM) == 0 {
		return nil, errors.New("apns: PrivateKeyPEM is required")
	}
	key, err := parseECPrivateKey(cfg.PrivateKeyPEM)
	if err != nil {
		return nil, fmt.Errorf("apns: parse private key: %w", err)
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.push.apple.com"
	}
	if cfg.HTTPClient == nil {
		// APNs requires HTTP/2 which the stdlib client negotiates over TLS.
		cfg.HTTPClient = &http.Client{Timeout: 10 * time.Second}
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &APNsSender{cfg: cfg, privKey: key, httpClient: cfg.HTTPClient, now: cfg.Now}, nil
}

// Send POSTs each device individually (APNs is one request per token) and
// reports per-device results so dead tokens (410 Unregistered / 400
// BadDeviceToken) can be pruned by the caller.
func (s *APNsSender) Send(ctx context.Context, devices []pushtoken.Token, payload port.PushPayload) []port.PushSendResult {
	out := make([]port.PushSendResult, 0, len(devices))
	for _, d := range devices {
		invalid, err := s.sendOne(ctx, d.Token, payload)
		out = append(out, port.PushSendResult{Token: d, Err: err, Invalid: invalid})
	}
	return out
}

func (s *APNsSender) sendOne(ctx context.Context, deviceToken string, payload port.PushPayload) (invalid bool, err error) {
	bearer, err := s.providerToken()
	if err != nil {
		return false, err
	}
	body, err := buildAPNsPayload(payload)
	if err != nil {
		return false, err
	}
	url := s.cfg.BaseURL + "/3/device/" + deviceToken
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return false, err
	}
	req.Header.Set("authorization", "bearer "+bearer)
	req.Header.Set("apns-topic", s.cfg.BundleID)
	req.Header.Set("apns-push-type", "alert")
	req.Header.Set("content-type", "application/json")
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return false, nil
	}
	// 410 Unregistered means the token is permanently invalid.
	// 400 BadDeviceToken means malformed.
	if resp.StatusCode == http.StatusGone ||
		strings.Contains(string(respBody), "BadDeviceToken") ||
		strings.Contains(string(respBody), "Unregistered") {
		return true, fmt.Errorf("apns: %s", strings.TrimSpace(string(respBody)))
	}
	return false, fmt.Errorf("apns: %s: %s", resp.Status, strings.TrimSpace(string(respBody)))
}

// buildAPNsPayload assembles the JSON body APNs expects (aps.alert + custom
// keys merged in alongside aps).
func buildAPNsPayload(p port.PushPayload) ([]byte, error) {
	root := map[string]any{
		"aps": map[string]any{
			"alert": map[string]string{
				"title": p.Title,
				"body":  p.Body,
			},
			"sound": "default",
		},
	}
	for k, v := range p.Data {
		// Don't let "aps" be overwritten by application data.
		if k == "aps" {
			continue
		}
		root[k] = v
	}
	return json.Marshal(root)
}

// providerToken returns a cached APNs provider token, minting a fresh one
// every ~50 minutes (Apple rejects tokens older than 1 h).
func (s *APNsSender) providerToken() (string, error) {
	s.tokenMu.Lock()
	defer s.tokenMu.Unlock()
	now := s.now()
	if s.bearer != "" && now.Sub(s.bearerIssue) < 50*time.Minute {
		return s.bearer, nil
	}
	header := base64URLJSON(map[string]string{"alg": "ES256", "kid": s.cfg.KeyID, "typ": "JWT"})
	claims := base64URLJSON(map[string]any{"iss": s.cfg.TeamID, "iat": now.Unix()})
	signingInput := header + "." + claims
	digest := sha256.Sum256([]byte(signingInput))
	r, sigS, err := ecdsa.Sign(rand.Reader, s.privKey, digest[:])
	if err != nil {
		return "", err
	}
	// APNs (and the JWS spec) want the raw r||s signature, not the ASN.1 form;
	// pad each component to the curve's byte size (32 for P-256).
	sig := encodeECSignature(r, sigS, 32)
	s.bearer = signingInput + "." + base64.RawURLEncoding.EncodeToString(sig)
	s.bearerIssue = now
	return s.bearer, nil
}

// encodeECSignature returns the raw r||s representation used by JWS / APNs,
// left-padding each component to size bytes.
func encodeECSignature(r, s *big.Int, size int) []byte {
	out := make([]byte, 2*size)
	rb := r.Bytes()
	sb := s.Bytes()
	copy(out[size-len(rb):size], rb)
	copy(out[2*size-len(sb):], sb)
	return out
}

// parseECPrivateKey accepts a PEM-encoded EC private key (.p8 from Apple uses
// PKCS#8) and returns the P-256 key APNs expects.
func parseECPrivateKey(pemBytes []byte) (*ecdsa.PrivateKey, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, errors.New("no PEM data found")
	}
	if key, err := x509.ParseECPrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	any, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	ec, ok := any.(*ecdsa.PrivateKey)
	if !ok {
		return nil, errors.New("PEM key is not ECDSA")
	}
	return ec, nil
}
