package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// MaxBody caps the size of request bodies by wrapping them in an
// http.MaxBytesReader, so an oversized payload errors while being read rather
// than being buffered whole (memory-amplification DoS). Multipart requests are
// exempt — file uploads enforce their own, larger limit in the handler.
func MaxBody(max int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		if max > 0 && c.Request.Body != nil && !strings.HasPrefix(c.ContentType(), "multipart/") {
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, max)
		}
		c.Next()
	}
}
