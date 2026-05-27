// Package auth provides OIDC token verification backed by Keycloak.
package auth

import (
	"context"
	"fmt"
	"time"

	"github.com/airhost/backend/internal/config"
	"github.com/coreos/go-oidc/v3/oidc"
)

// Claims holds the subset of token claims the application cares about.
type Claims struct {
	Subject           string `json:"sub"`
	Email             string `json:"email"`
	EmailVerified     bool   `json:"email_verified"`
	Name              string `json:"name"`
	PreferredUsername string `json:"preferred_username"`
	RealmAccess       struct {
		Roles []string `json:"roles"`
	} `json:"realm_access"`
}

// FullName returns the best available display name.
func (c Claims) FullName() string {
	if c.Name != "" {
		return c.Name
	}
	return c.PreferredUsername
}

// Verifier validates Keycloak-issued JWT access tokens against the realm's
// public keys (JWKS), which go-oidc fetches and caches automatically.
type Verifier struct {
	verifier *oidc.IDTokenVerifier
}

// NewVerifier builds a Verifier by discovering the OIDC provider. It retries
// briefly because Keycloak may still be starting in a fresh environment.
func NewVerifier(ctx context.Context, cfg config.KeycloakConfig) (*Verifier, error) {
	// Tokens carry cfg.Issuer (the browser-facing URL). When the backend reaches
	// Keycloak at a different internal address (cfg.DiscoveryURL, e.g. inside
	// Docker), fetch the discovery document and JWKS from there while still
	// validating the canonical issuer. InsecureIssuerURLContext tells go-oidc to
	// accept the issuer/discovery-URL divergence; the discovery doc's backchannel
	// endpoints (jwks_uri) then point at the reachable internal host.
	discoverURL := cfg.Issuer
	discoverCtx := ctx
	if cfg.DiscoveryURL != "" && cfg.DiscoveryURL != cfg.Issuer {
		discoverURL = cfg.DiscoveryURL
		discoverCtx = oidc.InsecureIssuerURLContext(ctx, cfg.Issuer)
	}

	var (
		provider *oidc.Provider
		err      error
	)
	for attempt := 0; attempt < 10; attempt++ {
		provider, err = oidc.NewProvider(discoverCtx, discoverURL)
		if err == nil {
			break
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(3 * time.Second):
		}
	}
	if err != nil {
		return nil, fmt.Errorf("discover oidc provider at %s: %w", discoverURL, err)
	}

	oidcCfg := &oidc.Config{
		// Keycloak access tokens are typically audienced to "account", not the
		// API client, so we verify issuer + signature and skip the audience
		// check. Authorization is enforced separately via roles/claims.
		SkipClientIDCheck: true,
	}
	return &Verifier{verifier: provider.Verifier(oidcCfg)}, nil
}

// Verify validates a raw bearer token and returns its claims.
func (v *Verifier) Verify(ctx context.Context, rawToken string) (*Claims, error) {
	idToken, err := v.verifier.Verify(ctx, rawToken)
	if err != nil {
		return nil, fmt.Errorf("verify token: %w", err)
	}
	var claims Claims
	if err := idToken.Claims(&claims); err != nil {
		return nil, fmt.Errorf("decode claims: %w", err)
	}
	return &claims, nil
}
