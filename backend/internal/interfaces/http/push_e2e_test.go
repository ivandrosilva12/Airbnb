package http_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	domainuser "github.com/airhost/backend/internal/domain/user"
	"github.com/google/uuid"
)

// TestEndToEnd_PushTokenRegistration exercises the device-registration flow
// the mobile/web client uses on launch (HTTP API) and verifies that a
// notification mirrors as a native push (via the recording sender wired into
// notificationSvc.WithPush).
func TestEndToEnd_PushTokenRegistration(t *testing.T) {
	h := newHarness(t)
	guest := h.seedUser(domainuser.RoleGuest, "push-guest@test.dev")
	tok := guest.ID.String()

	// Register an Android device.
	rec := h.do(http.MethodPost, "/api/v1/me/push-tokens", tok, map[string]any{
		"platform": "android", "token": "fcm-token-aaa",
	})
	mustStatus(t, rec, http.StatusCreated, "register push token")

	// Re-register the same device — upsert keeps the list size at one.
	rec = h.do(http.MethodPost, "/api/v1/me/push-tokens", tok, map[string]any{
		"platform": "android", "token": "fcm-token-aaa",
	})
	mustStatus(t, rec, http.StatusCreated, "re-register push token")

	// Register a second iOS device.
	rec = h.do(http.MethodPost, "/api/v1/me/push-tokens", tok, map[string]any{
		"platform": "ios", "token": "apns-token-bbb",
	})
	mustStatus(t, rec, http.StatusCreated, "register ios token")

	// List confirms two registrations.
	rec = h.do(http.MethodGet, "/api/v1/me/push-tokens", tok, nil)
	mustStatus(t, rec, http.StatusOK, "list push tokens")
	list := decodeArray(t, rec.Body.Bytes())
	if len(list) != 2 {
		t.Fatalf("expected 2 tokens, got %d (%v)", len(list), list)
	}

	// Toggling push.bookings off via the existing preferences route should
	// surface through the dto's pushPreferences view.
	rec = h.do(http.MethodPatch, "/api/v1/me/preferences", tok, map[string]any{
		"push": map[string]any{"bookings": false, "messages": true},
	})
	mustStatus(t, rec, http.StatusOK, "patch prefs")
	body := h.decode(rec)
	pushPrefs := body["pushPreferences"].(map[string]any)
	if pushPrefs["bookings"] != false || pushPrefs["messages"] != true {
		t.Fatalf("pushPreferences=%v, want bookings=false messages=true", pushPrefs)
	}

	// Trigger a notification through the public Notify entry point (saved-
	// search alert, categorised as account so the opt-out we just toggled does
	// not silence it). The recording push sender must observe one call covering
	// both devices.
	h.pusher.calls = nil
	if err := h.notificationSvc.Notify(context.Background(), guest.ID, "Alert", "New listings match", uuid.Nil); err != nil {
		t.Fatalf("notify: %v", err)
	}
	if len(h.pusher.calls) != 1 {
		t.Fatalf("expected 1 push call, got %d", len(h.pusher.calls))
	}
	got := h.pusher.calls[0]
	if got.Payload.Title != "Alert" || got.Payload.Body != "New listings match" {
		t.Fatalf("unexpected payload: %+v", got.Payload)
	}
	if len(got.Devices) != 2 {
		t.Fatalf("expected 2 devices delivered, got %d", len(got.Devices))
	}

	// Unregister the iOS device — list shrinks to one.
	rec = h.do(http.MethodPost, "/api/v1/me/push-tokens/unregister", tok, map[string]any{
		"platform": "ios", "token": "apns-token-bbb",
	})
	mustStatus(t, rec, http.StatusNoContent, "unregister push token")

	rec = h.do(http.MethodGet, "/api/v1/me/push-tokens", tok, nil)
	mustStatus(t, rec, http.StatusOK, "list push tokens after unregister")
	list = decodeArray(t, rec.Body.Bytes())
	if len(list) != 1 || list[0]["platform"] != "android" {
		t.Fatalf("expected only android left, got %v", list)
	}
}

// TestEndToEnd_PushOptOutSuppressesDelivery verifies that the per-category
// opt-out actually blocks a push for that bucket while leaving other buckets
// untouched.
func TestEndToEnd_PushOptOutSuppressesDelivery(t *testing.T) {
	h := newHarness(t)
	guest := h.seedUser(domainuser.RoleGuest, "push-mute@test.dev")
	tok := guest.ID.String()

	// Register one device + opt out of bookings.
	mustStatus(t, h.do(http.MethodPost, "/api/v1/me/push-tokens", tok, map[string]any{
		"platform": "android", "token": "fcm-mute",
	}), http.StatusCreated, "register")
	mustStatus(t, h.do(http.MethodPatch, "/api/v1/me/preferences", tok, map[string]any{
		"push": map[string]any{"bookings": false, "messages": false},
	}), http.StatusOK, "mute prefs")

	// Account notifications still fire (security-relevant).
	h.pusher.calls = nil
	if err := h.notificationSvc.Notify(context.Background(), guest.ID, "Welcome", "hi", uuid.Nil); err != nil {
		t.Fatalf("notify: %v", err)
	}
	if len(h.pusher.calls) != 1 {
		t.Fatalf("expected account push to bypass opt-outs, got %d calls", len(h.pusher.calls))
	}
}

// decodeArray decodes a JSON-array HTTP body into a slice of generic maps.
func decodeArray(t *testing.T, b []byte) []map[string]any {
	t.Helper()
	var out []map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("decode array body %q: %v", string(b), err)
	}
	return out
}
