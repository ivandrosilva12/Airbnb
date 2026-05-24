//go:build integration

// This file is excluded from the default build. Run it against a real Keycloak:
//
//	docker compose up -d keycloak                 # or any Keycloak with the airhost realm
//	cd backend && go test -tags=integration ./internal/interfaces/http/ -run Integration -v
//
// It drives the real OIDC auth middleware end-to-end: a real access token minted
// by Keycloak (password grant) is verified against the realm's JWKS, the local
// user is provisioned, and a protected endpoint returns the profile. Without a
// reachable Keycloak the test skips, so it is safe to leave enabled in CI.
package http_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	userapp "github.com/airhost/backend/internal/application/user"
	"github.com/airhost/backend/internal/config"
	domainuser "github.com/airhost/backend/internal/domain/user"
	"github.com/airhost/backend/internal/infrastructure/auth"
	"github.com/airhost/backend/internal/infrastructure/persistence/memory"
	"github.com/airhost/backend/internal/interfaces/http/handler"
	"github.com/airhost/backend/internal/interfaces/http/middleware"
	"github.com/gin-gonic/gin"
)

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// keycloakReachable probes the realm's OIDC discovery document.
func keycloakReachable(issuer string) bool {
	client := &http.Client{Timeout: 3 * time.Second}
	res, err := client.Get(strings.TrimRight(issuer, "/") + "/.well-known/openid-configuration")
	if err != nil {
		return false
	}
	defer res.Body.Close()
	return res.StatusCode == http.StatusOK
}

// passwordGrantToken mints an access token via Keycloak's direct access grant.
func passwordGrantToken(t *testing.T, issuer, clientID, username, password string) string {
	t.Helper()
	form := url.Values{
		"grant_type": {"password"},
		"client_id":  {clientID},
		"username":   {username},
		"password":   {password},
		"scope":      {"openid email profile"},
	}
	endpoint := strings.TrimRight(issuer, "/") + "/protocol/openid-connect/token"
	res, err := http.Post(endpoint, "application/x-www-form-urlencoded", strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatalf("token request: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("token endpoint status %d", res.StatusCode)
	}
	var out struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		t.Fatalf("decode token: %v", err)
	}
	if out.AccessToken == "" {
		t.Fatal("empty access token")
	}
	return out.AccessToken
}

// TestIntegration_RealKeycloakAuth verifies the full auth path against a live
// Keycloak: real token → real OIDC verifier → user provisioning → /me.
func TestIntegration_RealKeycloakAuth(t *testing.T) {
	issuer := envOr("KEYCLOAK_ISSUER", "http://localhost:8080/realms/airhost")
	clientID := envOr("KEYCLOAK_TEST_CLIENT", "airhost-web")
	apiClientID := envOr("KEYCLOAK_CLIENT_ID", "airhost-api")
	username := envOr("KEYCLOAK_TEST_USER", "host@airhost.dev")
	password := envOr("KEYCLOAK_TEST_PASSWORD", "host123")

	if !keycloakReachable(issuer) {
		t.Skipf("Keycloak not reachable at %s; set KEYCLOAK_ISSUER and start Keycloak to run", issuer)
	}

	ctx := context.Background()
	gin.SetMode(gin.TestMode)

	// Real OIDC verifier (discovers the provider, fetches JWKS).
	verifier, err := auth.NewVerifier(ctx, config.KeycloakConfig{Issuer: issuer, ClientID: apiClientID})
	if err != nil {
		t.Fatalf("new verifier: %v", err)
	}

	// Minimal router exercising the real auth middleware + user provisioning.
	userRepo := memory.NewUserRepository()
	userSvc := userapp.NewService(userRepo)
	syncFn := func(c *gin.Context, claims auth.Claims) (*domainuser.User, error) {
		return userSvc.SyncFromIdentity(c.Request.Context(), userapp.Identity{
			Subject:  claims.Subject,
			Email:    claims.Email,
			FullName: claims.FullName(),
		})
	}
	authMW := middleware.NewAuthMiddleware(verifier, syncFn)
	userHandler := handler.NewUserHandler(userSvc)

	r := gin.New()
	api := r.Group("/api/v1")
	api.Use(authMW)
	api.GET("/me", userHandler.Me)

	call := func(authHeader string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
		if authHeader != "" {
			req.Header.Set("Authorization", authHeader)
		}
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec
	}

	// A real Keycloak token authenticates and provisions the local user.
	token := passwordGrantToken(t, issuer, clientID, username, password)
	rec := call("Bearer " + token)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /me with real token: status %d (body: %s)", rec.Code, rec.Body.String())
	}
	var profile map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &profile); err != nil {
		t.Fatalf("decode profile: %v", err)
	}
	if profile["email"] != username {
		t.Fatalf("profile email = %v, want %s", profile["email"], username)
	}

	// A garbage token is rejected by the real verifier.
	if rec := call("Bearer not-a-real-token"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("GET /me with bad token: status %d, want 401", rec.Code)
	}

	// A missing token is rejected.
	if rec := call(""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("GET /me without token: status %d, want 401", rec.Code)
	}
}
