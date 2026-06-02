package middleware

import "github.com/gin-gonic/gin"

// SecurityHeaders sets conservative baseline response headers (S50).
//
// The API serves JSON only (plus two text/yaml + text/json OpenAPI spec
// payloads) — a deny-all CSP and frame/sniff protections are safe and
// guard against any accidental HTML rendering or clickjacking. HSTS is
// ignored by browsers over plain HTTP, so it is harmless in local dev
// and effective once the API is served over TLS.
//
// S50 widened the baseline beyond the original four headers to include:
//   - Permissions-Policy: explicitly denies every browser capability,
//     so a hijacked-and-rendered page from this origin cannot reach
//     for the user's camera, mic, geolocation, etc. The API never
//     legitimately serves HTML; locking the powers down costs nothing.
//   - Cross-Origin-Opener-Policy / Cross-Origin-Resource-Policy:
//     cross-origin isolation primitives. COOP=same-origin keeps any
//     accidentally-rendered page from sharing a browsing context with
//     a hostile opener. CORP=same-site stops other sites from loading
//     this origin's resources via <img>/<script>.
//   - Cross-Origin-Embedder-Policy: not set — the API does not need
//     SharedArrayBuffer or the strict embedding contract. Adding it
//     would require every cross-origin resource (e.g. spec viewers)
//     to carry CORP, which the OpenAPI viewer ecosystem does not
//     guarantee. Deferred until a concrete need exists.
//
// The CSP is hard-coded rather than config-driven because every
// reasonable production policy for a JSON API is identical to the
// current "default-src 'none'; frame-ancestors 'none'" — the operator
// has no useful knob to turn. A future BFF that serves HTML on the
// same host (e.g. a Swagger-UI page) would warrant a per-route CSP
// override, not a per-deployment one.
func SecurityHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		h := c.Writer.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
		h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		// S50 additions:
		h.Set("Permissions-Policy", apiPermissionsPolicy)
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		h.Set("Cross-Origin-Resource-Policy", "same-site")
		c.Next()
	}
}

// apiPermissionsPolicy denies every powerful browser capability for
// the API origin. Listed exhaustively so a UA that ignores omitted
// directives still hard-denies — Permissions-Policy is a closed
// enumeration by spec but vendors disagree on the default for new
// features. Empty parens = allow nothing; (self) on a few would
// matter only if the API ever started serving HTML.
const apiPermissionsPolicy = "accelerometer=(), autoplay=(), camera=(), display-capture=(), encrypted-media=(), fullscreen=(), geolocation=(), gyroscope=(), magnetometer=(), microphone=(), midi=(), payment=(), picture-in-picture=(), publickey-credentials-get=(), screen-wake-lock=(), sync-xhr=(), usb=(), web-share=(), xr-spatial-tracking=()"
