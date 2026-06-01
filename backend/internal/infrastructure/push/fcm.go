package push

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/airhost/backend/internal/application/port"
	"github.com/airhost/backend/internal/domain/pushtoken"
)

// fcmServiceAccount mirrors the fields we need from a Google service account
// JSON. The full file contains more keys; the rest are ignored.
type fcmServiceAccount struct {
	ClientEmail string `json:"client_email"`
	PrivateKey  string `json:"private_key"`
	TokenURI    string `json:"token_uri"`
	ProjectID   string `json:"project_id"`
}

// FCMConfig holds the options to build an FCM HTTP v1 sender.
type FCMConfig struct {
	// ServiceAccountJSON is the raw bytes of the service account file. When
	// empty the constructor returns an error.
	ServiceAccountJSON []byte
	// BaseURL overrides the FCM endpoint root (used by tests against a mock).
	// Defaults to https://fcm.googleapis.com.
	BaseURL string
	// HTTPClient overrides the HTTP client. Defaults to a 10s-timeout client.
	HTTPClient *http.Client
	// Now overrides the clock (tests). Defaults to time.Now.
	Now func() time.Time
}

// FCMSender delivers pushes via Firebase Cloud Messaging HTTP v1 (Android-first;
// also handles browsers registered through Firebase as platform="web"). It
// mints an OAuth2 access token from the service account on demand and caches
// it until shortly before expiry.
type FCMSender struct {
	sa         fcmServiceAccount
	privKey    *rsa.PrivateKey
	baseURL    string
	httpClient *http.Client
	now        func() time.Time

	tokenMu     sync.Mutex
	accessToken string
	accessExp   time.Time
}

// NewFCMSender parses the service account, extracts the RSA private key, and
// returns a ready-to-use sender. Returns an error when the JSON is malformed
// or the key cannot be parsed.
func NewFCMSender(cfg FCMConfig) (*FCMSender, error) {
	if len(cfg.ServiceAccountJSON) == 0 {
		return nil, errors.New("fcm: service account JSON is required")
	}
	var sa fcmServiceAccount
	if err := json.Unmarshal(cfg.ServiceAccountJSON, &sa); err != nil {
		return nil, fmt.Errorf("fcm: parse service account: %w", err)
	}
	if sa.ProjectID == "" || sa.ClientEmail == "" || sa.PrivateKey == "" {
		return nil, errors.New("fcm: service account missing required fields")
	}
	if sa.TokenURI == "" {
		sa.TokenURI = "https://oauth2.googleapis.com/token"
	}
	key, err := parseRSAPrivateKey([]byte(sa.PrivateKey))
	if err != nil {
		return nil, fmt.Errorf("fcm: parse private key: %w", err)
	}
	base := cfg.BaseURL
	if base == "" {
		base = "https://fcm.googleapis.com"
	}
	cli := cfg.HTTPClient
	if cli == nil {
		cli = &http.Client{Timeout: 10 * time.Second}
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &FCMSender{sa: sa, privKey: key, baseURL: base, httpClient: cli, now: now}, nil
}

// Send delivers payload to each device individually (FCM HTTP v1 accepts one
// recipient per call) and reports per-device results so the multiplex can
// prune dead tokens.
func (s *FCMSender) Send(ctx context.Context, devices []pushtoken.Token, payload port.PushPayload) []port.PushSendResult {
	out := make([]port.PushSendResult, 0, len(devices))
	for _, d := range devices {
		invalid, err := s.sendOne(ctx, d.Token, payload)
		out = append(out, port.PushSendResult{Token: d, Err: err, Invalid: invalid})
	}
	return out
}

func (s *FCMSender) sendOne(ctx context.Context, deviceToken string, payload port.PushPayload) (invalid bool, err error) {
	tok, err := s.accessTokenOrMint(ctx)
	if err != nil {
		return false, err
	}
	body, err := json.Marshal(map[string]any{
		"message": map[string]any{
			"token": deviceToken,
			"notification": map[string]string{
				"title": payload.Title,
				"body":  payload.Body,
			},
			"data": payload.Data,
		},
	})
	if err != nil {
		return false, err
	}
	endpoint := s.baseURL + "/v1/projects/" + url.PathEscape(s.sa.ProjectID) + "/messages:send"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return false, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return false, nil
	}
	// FCM signals a dead token with 404 NOT_FOUND or 410 GONE in the v1 API,
	// or with errorCode UNREGISTERED / INVALID_ARGUMENT for malformed tokens.
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone ||
		strings.Contains(string(respBody), "UNREGISTERED") {
		return true, fmt.Errorf("fcm: %s", strings.TrimSpace(string(respBody)))
	}
	return false, fmt.Errorf("fcm: %s: %s", resp.Status, strings.TrimSpace(string(respBody)))
}

// accessTokenOrMint returns a cached access token if still fresh, otherwise
// performs the OAuth2 JWT-bearer exchange and caches the result.
func (s *FCMSender) accessTokenOrMint(ctx context.Context) (string, error) {
	s.tokenMu.Lock()
	defer s.tokenMu.Unlock()
	now := s.now()
	if s.accessToken != "" && now.Before(s.accessExp.Add(-60*time.Second)) {
		return s.accessToken, nil
	}
	jwt, err := s.signAssertion(now)
	if err != nil {
		return "", err
	}
	form := url.Values{}
	form.Set("grant_type", "urn:ietf:params:oauth:grant-type:jwt-bearer")
	form.Set("assertion", jwt)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.sa.TokenURI, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("fcm: token exchange %s: %s", resp.Status, strings.TrimSpace(string(respBody)))
	}
	var tr struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
		TokenType   string `json:"token_type"`
	}
	if err := json.Unmarshal(respBody, &tr); err != nil {
		return "", fmt.Errorf("fcm: token exchange decode: %w", err)
	}
	if tr.AccessToken == "" {
		return "", errors.New("fcm: token exchange returned no access_token")
	}
	s.accessToken = tr.AccessToken
	s.accessExp = now.Add(time.Duration(tr.ExpiresIn) * time.Second)
	return s.accessToken, nil
}

// signAssertion builds and RS256-signs the JWT used to request the FCM scope.
func (s *FCMSender) signAssertion(now time.Time) (string, error) {
	header := base64URLJSON(map[string]string{"alg": "RS256", "typ": "JWT"})
	claims := base64URLJSON(map[string]any{
		"iss":   s.sa.ClientEmail,
		"scope": "https://www.googleapis.com/auth/firebase.messaging",
		"aud":   s.sa.TokenURI,
		"iat":   now.Unix(),
		"exp":   now.Add(time.Hour).Unix(),
	})
	signingInput := header + "." + claims
	digest := sha256.Sum256([]byte(signingInput))
	sig, err := rsa.SignPKCS1v15(rand.Reader, s.privKey, crypto.SHA256, digest[:])
	if err != nil {
		return "", err
	}
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

// parseRSAPrivateKey accepts a PEM-encoded RSA private key in either PKCS#1
// ("RSA PRIVATE KEY") or PKCS#8 ("PRIVATE KEY") form — GCP service accounts
// use the latter.
func parseRSAPrivateKey(pemBytes []byte) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, errors.New("no PEM data found")
	}
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	any, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	rsaKey, ok := any.(*rsa.PrivateKey)
	if !ok {
		return nil, errors.New("PEM key is not RSA")
	}
	return rsaKey, nil
}

func base64URLJSON(v any) string {
	b, _ := json.Marshal(v)
	return base64.RawURLEncoding.EncodeToString(b)
}
