package middleware

import "github.com/gin-gonic/gin"

// SecurityHeaders sets conservative baseline response headers. The API serves
// JSON only, so a deny-all CSP and frame/sniff protections are safe and guard
// against any accidental HTML rendering or clickjacking. HSTS is ignored by
// browsers over plain HTTP, so it is harmless in local dev and effective once
// the API is served over TLS.
func SecurityHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		h := c.Writer.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
		h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		c.Next()
	}
}
