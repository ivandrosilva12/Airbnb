package openapi

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestGenerate_PathConversion confirms gin's :param syntax is rewritten
// into OpenAPI's {param} form, and that catch-all (*) is translated to
// the same brace form so an accidental introduction yields a valid spec.
func TestGenerate_PathConversion(t *testing.T) {
	cases := []struct{ in, want string }{
		{"/api/v1/properties/:id", "/api/v1/properties/{id}"},
		{"/api/v1/properties/:id/photos/:photoId", "/api/v1/properties/{id}/photos/{photoId}"},
		{"/api/v1/webhooks/payments/:provider", "/api/v1/webhooks/payments/{provider}"},
		{"/api/v1/files/*path", "/api/v1/files/{path}"},
		{"/healthz", "/healthz"},
	}
	for _, c := range cases {
		if got := convertPath(c.in); got != c.want {
			t.Errorf("convertPath(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestGenerate_PathParams_ExtractedAtPathItem proves the path-param
// list is populated for templated routes (shared at PathItem level per
// OAS3) and empty for static paths.
func TestGenerate_PathParams_ExtractedAtPathItem(t *testing.T) {
	doc := Generate([]Route{
		{Method: "GET", Path: "/api/v1/properties/:id", Handler: "h.(*PropertyHandler).Get-fm"},
		{Method: "GET", Path: "/api/v1/amenities", Handler: "h.(*PropertyHandler).Amenities-fm"},
	}, Info{Title: "t", Version: "v"})

	withParam := doc.Paths["/api/v1/properties/{id}"]
	if len(withParam.Parameters) != 1 || withParam.Parameters[0].Name != "id" || withParam.Parameters[0].In != "path" || !withParam.Parameters[0].Required {
		t.Fatalf("expected one required path param 'id', got %+v", withParam.Parameters)
	}

	staticItem := doc.Paths["/api/v1/amenities"]
	if len(staticItem.Parameters) != 0 {
		t.Fatalf("static path should have no parameters, got %+v", staticItem.Parameters)
	}
}

// TestGenerate_TagAndOperationID_FromHandler confirms the handler-name
// heuristic produces sensible tags and unique operationIds. PropertyHandler.Search
// must become tag "property" + operationId "propertySearch".
func TestGenerate_TagAndOperationID_FromHandler(t *testing.T) {
	handler := "github.com/airhost/backend/internal/interfaces/http/handler.(*PropertyHandler).Search-fm"
	if got := tagFromHandler(handler); got != "property" {
		t.Errorf("tagFromHandler = %q, want property", got)
	}
	if got := operationIDFromHandler(handler); got != "propertySearch" {
		t.Errorf("operationIDFromHandler = %q, want propertySearch", got)
	}
}

// TestGenerate_TagFallback_OnInlineHandler shows we don't crash on the
// handful of inline closures (gin.WrapH for /metrics etc) — the tag
// goes empty, the operationId is synthesised from the symbol so
// uniqueness is preserved.
func TestGenerate_TagFallback_OnInlineHandler(t *testing.T) {
	if got := tagFromHandler("func1"); got != "" {
		t.Errorf("expected empty tag for non-handler symbol, got %q", got)
	}
	if got := operationIDFromHandler("func1"); got == "" {
		t.Errorf("expected non-empty operationID fallback, got empty")
	}
}

// TestGenerate_AuthRequired_DefaultsClosed is the cornerstone of the
// security model: anything under /api/v1/** that isn't explicitly on
// the publicAPIPaths allowlist requires bearerAuth. This test exists
// so a new private endpoint can't accidentally be marked public.
func TestGenerate_AuthRequired_DefaultsClosed(t *testing.T) {
	doc := Generate([]Route{
		// Listed public: no security.
		{Method: "GET", Path: "/api/v1/properties", Handler: "h.(*PropertyHandler).Search-fm"},
		// Not listed: bearerAuth required.
		{Method: "POST", Path: "/api/v1/bookings", Handler: "h.(*BookingHandler).Create-fm"},
		// Admin route: bearerAuth + extra "admin" tag.
		{Method: "POST", Path: "/api/v1/admin/properties/:id/suspend", Handler: "h.(*PropertyHandler).AdminSuspend-fm"},
		// Operational: no security.
		{Method: "GET", Path: "/healthz", Handler: "h.(*HealthHandler).Live-fm"},
		// Webhook: no security (signature, not bearer).
		{Method: "POST", Path: "/api/v1/webhooks/payments/:provider", Handler: "h.(*PaymentWebhookHandler).Handle-fm"},
	}, Info{Title: "t", Version: "v"})

	public := doc.Paths["/api/v1/properties"].Get
	if public == nil || len(public.Security) != 0 {
		t.Fatalf("public read should have no security, got %+v", public)
	}

	private := doc.Paths["/api/v1/bookings"].Post
	if private == nil || len(private.Security) != 1 {
		t.Fatalf("private route should have one security req, got %+v", private)
	}
	if _, ok := private.Security[0]["bearerAuth"]; !ok {
		t.Errorf("expected bearerAuth, got %+v", private.Security[0])
	}
	if _, ok := private.Responses["401"]; !ok {
		t.Errorf("authed route should declare 401, got %+v", private.Responses)
	}

	admin := doc.Paths["/api/v1/admin/properties/{id}/suspend"].Post
	if admin == nil {
		t.Fatalf("admin route missing from document")
	}
	if !containsTag(admin.Tags, "admin") {
		t.Errorf("admin route missing 'admin' tag, got %v", admin.Tags)
	}

	healthz := doc.Paths["/healthz"].Get
	if healthz == nil || len(healthz.Security) != 0 {
		t.Errorf("healthz should be public, got %+v", healthz)
	}

	webhook := doc.Paths["/api/v1/webhooks/payments/{provider}"].Post
	if webhook == nil || len(webhook.Security) != 0 {
		t.Errorf("webhook should have no bearerAuth (signed), got %+v", webhook)
	}
}

// TestGenerate_ContainsBearerAuthScheme + Components.Schemas — the
// shared bits (Error envelope, Page, bearerAuth) MUST appear in
// components so per-operation refs resolve.
func TestGenerate_Components_Present(t *testing.T) {
	doc := Generate(nil, Info{Title: "t", Version: "v"})
	if doc.Components.SecuritySchemes["bearerAuth"] == nil {
		t.Errorf("bearerAuth security scheme missing")
	}
	if doc.Components.Schemas["Error"] == nil {
		t.Errorf("Error component schema missing")
	}
	if doc.Components.Schemas["Page"] == nil {
		t.Errorf("Page component schema missing")
	}
}

// TestGenerate_JSONRoundTrip checks the document marshals to valid
// JSON and the canonical fields are present at the top level. We don't
// pull in a full OpenAPI validator (vendored swagger libs are big) —
// the structural check + the per-field tests above + the live-server
// drift test together cover the same ground.
func TestGenerate_JSONRoundTrip(t *testing.T) {
	doc := Generate([]Route{
		{Method: "GET", Path: "/api/v1/me", Handler: "h.(*UserHandler).Me-fm"},
	}, Info{Title: "AirHost API", Version: "test", Description: "x"})

	b, err := MarshalJSON(doc)
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}
	if !strings.Contains(string(b), `"openapi": "3.0.3"`) {
		t.Errorf("expected openapi version pin, got: %s", b)
	}

	var probe map[string]any
	if err := json.Unmarshal(b, &probe); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, k := range []string{"openapi", "info", "paths", "components"} {
		if _, ok := probe[k]; !ok {
			t.Errorf("missing top-level key %q in JSON output", k)
		}
	}
}

// TestGenerate_YAML_DeterministicTagOrder confirms a regeneration with
// the same input produces byte-identical YAML — a precondition for any
// diff-based drift check in CI.
func TestGenerate_YAML_DeterministicTagOrder(t *testing.T) {
	routes := []Route{
		{Method: "GET", Path: "/api/v1/zeta", Handler: "h.(*ZetaHandler).Get-fm"},
		{Method: "GET", Path: "/api/v1/alpha", Handler: "h.(*AlphaHandler).Get-fm"},
		{Method: "GET", Path: "/api/v1/beta", Handler: "h.(*BetaHandler).Get-fm"},
	}
	first, err := MarshalYAML(Generate(routes, Info{Title: "t", Version: "v"}))
	if err != nil {
		t.Fatalf("first marshal: %v", err)
	}
	second, err := MarshalYAML(Generate(routes, Info{Title: "t", Version: "v"}))
	if err != nil {
		t.Fatalf("second marshal: %v", err)
	}
	if string(first) != string(second) {
		t.Fatalf("YAML output not deterministic across regenerations")
	}
	// And the tag list should be alphabetised — alpha before beta before zeta.
	got := string(first)
	idxA, idxB, idxZ := strings.Index(got, "name: alpha"), strings.Index(got, "name: beta"), strings.Index(got, "name: zeta")
	if !(idxA > 0 && idxA < idxB && idxB < idxZ) {
		t.Errorf("expected alpha < beta < zeta in tag order, got A=%d B=%d Z=%d", idxA, idxB, idxZ)
	}
}

func containsTag(tags []string, want string) bool {
	for _, t := range tags {
		if t == want {
			return true
		}
	}
	return false
}
