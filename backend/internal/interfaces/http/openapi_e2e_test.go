package http_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// TestOpenAPI_DocumentReachable_PublicAndUnauthed proves the spec is
// served on both routes without auth (codegen clients pull it before
// they have any tokens) and that it contains the canonical top-level
// keys we promise. If a future change accidentally puts the spec
// behind auth this test trips before clients break in the field.
func TestOpenAPI_DocumentReachable_PublicAndUnauthed(t *testing.T) {
	h := newHarness(t)

	// YAML route — empty bearer token to be sure auth is not required.
	rec := h.do(http.MethodGet, "/openapi.yaml", "", nil)
	mustStatus(t, rec, http.StatusOK, "GET /openapi.yaml")
	if !strings.HasPrefix(rec.Body.String(), "openapi:") {
		t.Fatalf("YAML doc should start with 'openapi:', got: %q", rec.Body.String()[:50])
	}

	// JSON route.
	rec = h.do(http.MethodGet, "/openapi.json", "", nil)
	mustStatus(t, rec, http.StatusOK, "GET /openapi.json")
	var doc map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatalf("json decode: %v", err)
	}
	if doc["openapi"] != "3.0.3" {
		t.Errorf("openapi version = %v, want 3.0.3", doc["openapi"])
	}
	for _, k := range []string{"info", "paths", "components"} {
		if _, ok := doc[k]; !ok {
			t.Errorf("doc missing top-level key %q", k)
		}
	}
}

// TestOpenAPI_DocumentsEveryRoute_NoDrift is the meaningful contract:
// every route registered on the live router must appear in the spec,
// at the right path and method. If a developer adds an endpoint and
// forgets that the spec generator handles it automatically, this test
// confirms it actually does — and if someone bypasses the standard
// gin registration in a way the generator misses, the test fails.
func TestOpenAPI_DocumentsEveryRoute_NoDrift(t *testing.T) {
	h := newHarness(t)

	// Pull the doc.
	rec := h.do(http.MethodGet, "/openapi.json", "", nil)
	mustStatus(t, rec, http.StatusOK, "fetch spec")
	var doc struct {
		Paths map[string]map[string]any `json:"paths"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatalf("decode spec: %v", err)
	}

	// Walk the gin route table and confirm every entry is documented.
	var missing []string
	for _, r := range h.router.Routes() {
		oaPath := strings.ReplaceAll(strings.ReplaceAll(r.Path, ":", "{"), "{", "{") // start
		// Mirror the generator's :param → {param} rewrite via a small helper.
		oaPath = ginToOA(r.Path)
		methods, ok := doc.Paths[oaPath]
		if !ok {
			missing = append(missing, r.Method+" "+r.Path+" (path absent)")
			continue
		}
		if _, ok := methods[strings.ToLower(r.Method)]; !ok {
			missing = append(missing, r.Method+" "+r.Path+" (method absent)")
		}
	}
	if len(missing) > 0 {
		t.Fatalf("OpenAPI spec missing %d route(s):\n  %s", len(missing), strings.Join(missing, "\n  "))
	}
}

// TestOpenAPI_AdminRoutesCarry_BearerAuth confirms a representative
// admin route ended up with the bearerAuth security requirement after
// passing through the generator's classify-by-prefix step.
//
// The spec is decoded into a loose map[string]any because PathItem
// also contains a `parameters` array alongside the per-method
// operations, and a typed struct that tried to declare both would
// have to mirror the full OpenAPI shape. The probe walks the result
// with type assertions instead.
func TestOpenAPI_AdminRoutesCarry_BearerAuth(t *testing.T) {
	h := newHarness(t)
	doc := fetchSpecPaths(t, h)

	op := pickOperation(t, doc, "/api/v1/admin/audit", "get")
	sec := assertSliceField(t, op, "security")
	if len(sec) != 1 {
		t.Fatalf("admin audit GET should have 1 security req, got %d (%+v)", len(sec), sec)
	}
	if _, ok := sec[0].(map[string]any)["bearerAuth"]; !ok {
		t.Errorf("admin audit GET missing bearerAuth, got %+v", sec[0])
	}
	tags := assertSliceField(t, op, "tags")
	if !sliceContainsAny(tags, "admin") {
		t.Errorf("admin audit GET missing 'admin' tag, got %v", tags)
	}
}

// TestOpenAPI_PublicSearchRoute_HasNoBearerAuth proves the public
// listing search remained unauthenticated through the classification —
// a regression here would lock anonymous browsing.
func TestOpenAPI_PublicSearchRoute_HasNoBearerAuth(t *testing.T) {
	h := newHarness(t)
	doc := fetchSpecPaths(t, h)

	op := pickOperation(t, doc, "/api/v1/properties", "get")
	if sec, ok := op["security"]; ok {
		if list, _ := sec.([]any); len(list) != 0 {
			t.Errorf("public search GET should have 0 security reqs, got %d", len(list))
		}
	}
}

// fetchSpecPaths hits /openapi.json and returns paths as a loose map.
func fetchSpecPaths(t *testing.T, h *harness) map[string]any {
	t.Helper()
	rec := h.do(http.MethodGet, "/openapi.json", "", nil)
	mustStatus(t, rec, http.StatusOK, "fetch spec")
	var doc struct {
		Paths map[string]any `json:"paths"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatalf("decode spec: %v", err)
	}
	return doc.Paths
}

// pickOperation drills paths[path][method], asserting both layers
// exist and the method node is an object. Returns the operation map.
func pickOperation(t *testing.T, paths map[string]any, path, method string) map[string]any {
	t.Helper()
	item, ok := paths[path]
	if !ok {
		t.Fatalf("%s missing from spec", path)
	}
	asMap, ok := item.(map[string]any)
	if !ok {
		t.Fatalf("%s pathitem is not an object: %T", path, item)
	}
	opAny, ok := asMap[method]
	if !ok {
		t.Fatalf("%s %s missing from pathitem", method, path)
	}
	op, ok := opAny.(map[string]any)
	if !ok {
		t.Fatalf("%s %s is not an object: %T", method, path, opAny)
	}
	return op
}

// assertSliceField returns op[field] as a []any, failing the test if
// it is absent or the wrong shape.
func assertSliceField(t *testing.T, op map[string]any, field string) []any {
	t.Helper()
	v, ok := op[field]
	if !ok {
		t.Fatalf("operation missing field %q", field)
	}
	s, ok := v.([]any)
	if !ok {
		t.Fatalf("field %q is not an array: %T", field, v)
	}
	return s
}

func sliceContainsAny(s []any, v string) bool {
	for _, x := range s {
		if str, ok := x.(string); ok && str == v {
			return true
		}
	}
	return false
}

// ginToOA replicates the generator's path rewrite for assertion use.
// Kept local to the test so a drift between this and the generator's
// own convertPath causes the test to fail, not silently agree.
func ginToOA(in string) string {
	parts := strings.Split(in, "/")
	for i, p := range parts {
		if strings.HasPrefix(p, ":") {
			parts[i] = "{" + p[1:] + "}"
		} else if strings.HasPrefix(p, "*") {
			parts[i] = "{" + p[1:] + "}"
		}
	}
	return strings.Join(parts, "/")
}

func sliceContains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}
