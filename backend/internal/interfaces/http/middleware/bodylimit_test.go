package middleware

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestMaxBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(MaxBody(8))
	r.POST("/x", func(c *gin.Context) {
		if _, err := io.ReadAll(c.Request.Body); err != nil {
			c.Status(http.StatusRequestEntityTooLarge)
			return
		}
		c.Status(http.StatusOK)
	})

	do := func(contentType, body string) int {
		req := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(body))
		req.Header.Set("Content-Type", contentType)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec.Code
	}

	if code := do("application/json", "1234"); code != http.StatusOK {
		t.Fatalf("small body: status = %d, want 200", code)
	}
	if code := do("application/json", strings.Repeat("a", 32)); code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized body: status = %d, want 413", code)
	}
	// Multipart is exempt (uploads enforce their own limit), so a large body reads fine.
	if code := do("multipart/form-data; boundary=z", strings.Repeat("a", 32)); code != http.StatusOK {
		t.Fatalf("multipart exempt: status = %d, want 200", code)
	}
}
