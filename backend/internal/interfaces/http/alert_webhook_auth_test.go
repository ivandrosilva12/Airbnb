package http_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	alertingapp "github.com/airhost/backend/internal/application/alerting"
	alertstateapp "github.com/airhost/backend/internal/application/alertstate"
	"github.com/airhost/backend/internal/interfaces/http/handler"
	"github.com/gin-gonic/gin"
)

// TestAlertWebhook_TokenAuth verifies the optional bearer-token guard on the
// Alertmanager webhook receiver: when a token is configured, only requests
// carrying it are accepted.
func TestAlertWebhook_TokenAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := handler.NewAlertHandler(alertingapp.NewService(newMemSilencer()), alertstateapp.NewService(), "s3cret")
	r := gin.New()
	r.POST("/webhooks/alerts", h.IngestNotification)

	post := func(authHeader string) int {
		body := []byte(`{"status":"firing","alerts":[{"status":"firing","fingerprint":"fp","labels":{"alertname":"X"}}]}`)
		req := httptest.NewRequest(http.MethodPost, "/webhooks/alerts", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if authHeader != "" {
			req.Header.Set("Authorization", authHeader)
		}
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec.Code
	}

	if code := post(""); code != http.StatusUnauthorized {
		t.Fatalf("no token: status = %d, want 401", code)
	}
	if code := post("Bearer wrong"); code != http.StatusUnauthorized {
		t.Fatalf("wrong token: status = %d, want 401", code)
	}
	if code := post("Bearer s3cret"); code != http.StatusOK {
		t.Fatalf("correct token: status = %d, want 200", code)
	}
}

// TestAlertWebhook_OpenWhenNoToken confirms the route stays open when no token
// is configured (internal-network default).
func TestAlertWebhook_OpenWhenNoToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := handler.NewAlertHandler(alertingapp.NewService(newMemSilencer()), alertstateapp.NewService(), "")
	r := gin.New()
	r.POST("/webhooks/alerts", h.IngestNotification)

	body := []byte(`{"status":"resolved","alerts":[]}`)
	req := httptest.NewRequest(http.MethodPost, "/webhooks/alerts", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("open route: status = %d, want 200", rec.Code)
	}
}
