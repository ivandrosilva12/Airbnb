package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// TestSecurityHeaders_AllBaselinePresent is the canonical guard:
// every header the middleware promises to set MUST appear on a
// response, with the value the spec contract pins. If a future edit
// drops one (say a refactor splits the headers across middlewares
// and forgets one), this test trips before a deploy strips
// protection.
func TestSecurityHeaders_AllBaselinePresent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(SecurityHeaders())
	r.GET("/probe", func(c *gin.Context) { c.Status(http.StatusOK) })

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/probe", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	cases := []struct {
		header   string
		mustHave string // substring match — the exact value is checked separately for CSP/Permissions
	}{
		{"X-Content-Type-Options", "nosniff"},
		{"X-Frame-Options", "DENY"},
		{"Referrer-Policy", "no-referrer"},
		{"Content-Security-Policy", "default-src 'none'"},
		{"Content-Security-Policy", "frame-ancestors 'none'"},
		{"Strict-Transport-Security", "max-age=31536000"},
		{"Cross-Origin-Opener-Policy", "same-origin"},
		{"Cross-Origin-Resource-Policy", "same-site"},
	}
	for _, c := range cases {
		got := rec.Header().Get(c.header)
		if got == "" {
			t.Errorf("header %q missing", c.header)
			continue
		}
		if !strings.Contains(got, c.mustHave) {
			t.Errorf("header %q = %q, want substring %q", c.header, got, c.mustHave)
		}
	}
}

// TestSecurityHeaders_PermissionsPolicyDeniesPowerfulCapabilities
// asserts the Permissions-Policy locks down sensitive capabilities.
// Spelled out so a "trim to the essentials" refactor cannot quietly
// re-enable the camera or microphone for this origin.
func TestSecurityHeaders_PermissionsPolicyDeniesPowerfulCapabilities(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(SecurityHeaders())
	r.GET("/probe", func(c *gin.Context) { c.Status(http.StatusOK) })

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/probe", nil))
	pp := rec.Header().Get("Permissions-Policy")
	if pp == "" {
		t.Fatalf("Permissions-Policy missing")
	}
	for _, mustDeny := range []string{"camera=()", "microphone=()", "geolocation=()", "payment=()", "usb=()", "publickey-credentials-get=()"} {
		if !strings.Contains(pp, mustDeny) {
			t.Errorf("Permissions-Policy missing %q (got: %s)", mustDeny, pp)
		}
	}
}

// TestSecurityHeaders_HSTS_IncludesSubDomainsWithoutPreload —
// preload-listing is an irreversible commitment (the host is
// registered with browsers and de-listing takes months). The
// middleware deliberately omits "preload"; this test pins that
// choice so an over-eager edit can't add it without review.
func TestSecurityHeaders_HSTS_IncludesSubDomainsWithoutPreload(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(SecurityHeaders())
	r.GET("/probe", func(c *gin.Context) { c.Status(http.StatusOK) })

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/probe", nil))
	hsts := rec.Header().Get("Strict-Transport-Security")
	if !strings.Contains(hsts, "includeSubDomains") {
		t.Errorf("HSTS missing includeSubDomains: %q", hsts)
	}
	if strings.Contains(hsts, "preload") {
		t.Errorf("HSTS unexpectedly carries 'preload' — requires deliberate review: %q", hsts)
	}
}
