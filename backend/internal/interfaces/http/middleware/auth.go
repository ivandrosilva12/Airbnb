package middleware

import (
	"net/http"
	"strings"

	"github.com/airhost/backend/internal/domain/user"
	"github.com/airhost/backend/internal/infrastructure/auth"
	"github.com/airhost/backend/internal/interfaces/http/response"
	"github.com/gin-gonic/gin"
)

// SyncFunc loads/provisions the local user for a verified set of token claims.
// It is supplied by the composition root, adapting the user application service
// so this package does not depend on it directly.
type SyncFunc func(c *gin.Context, claims auth.Claims) (*user.User, error)

// NewAuthMiddleware builds a gin middleware that requires a valid bearer token,
// verifies it against Keycloak, and attaches the local user to the context.
func NewAuthMiddleware(verifier *auth.Verifier, sync SyncFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		raw := bearerToken(c.GetHeader("Authorization"))
		if raw == "" {
			// The browser EventSource API cannot set headers, so SSE clients pass
			// the token as a query parameter instead.
			raw = c.Query("access_token")
		}
		if raw == "" {
			response.FailMessage(c, http.StatusUnauthorized, "missing bearer token")
			return
		}
		claims, err := verifier.Verify(c.Request.Context(), raw)
		if err != nil {
			response.FailMessage(c, http.StatusUnauthorized, "invalid or expired token")
			return
		}
		u, err := sync(c, *claims)
		if err != nil {
			response.Fail(c, err)
			return
		}
		if !u.IsActive {
			response.FailMessage(c, http.StatusForbidden, "account is disabled")
			return
		}
		SetCurrentUser(c, u)
		c.Next()
	}
}

// RequireHost ensures the authenticated user can act as a host.
func RequireHost() gin.HandlerFunc {
	return func(c *gin.Context) {
		u, ok := CurrentUser(c)
		if !ok {
			response.FailMessage(c, http.StatusUnauthorized, "authentication required")
			return
		}
		if !u.IsHost() {
			response.FailMessage(c, http.StatusForbidden, "host role required")
			return
		}
		c.Next()
	}
}

// RequireAdmin ensures the authenticated user is a platform administrator.
func RequireAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		u, ok := CurrentUser(c)
		if !ok {
			response.FailMessage(c, http.StatusUnauthorized, "authentication required")
			return
		}
		if u.Role != user.RoleAdmin {
			response.FailMessage(c, http.StatusForbidden, "admin role required")
			return
		}
		c.Next()
	}
}

func bearerToken(header string) string {
	const prefix = "Bearer "
	if len(header) > len(prefix) && strings.EqualFold(header[:len(prefix)], prefix) {
		return strings.TrimSpace(header[len(prefix):])
	}
	return ""
}
