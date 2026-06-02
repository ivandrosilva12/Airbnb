package openapi

import (
	"sort"
	"strings"
	"unicode"
)

// Route is the slim subset of gin.RouteInfo the generator needs. It is
// declared here (rather than taking gin.RouteInfo directly) so the
// package can be tested without dragging in a full router and to keep
// gin out of the generator's import set — Generate is pure data.
type Route struct {
	Method  string // GET, POST, ...
	Path    string // "/api/v1/properties/:id" — gin format
	Handler string // "github.com/.../handler.(*PropertyHandler).Search-fm"
}

// publicAPIPaths lists the routes that are reachable WITHOUT a bearer
// token. Everything else under /api/v1 is treated as bearerAuth-required.
// The list is exact-match on the gin path template so a new public route
// has to be added here on purpose — fail-closed by default.
//
// Webhooks are a separate case: they are unauthenticated to user tokens
// but require a provider-signed payload. We document them as "no security
// requirement" so a client doesn't try to send a JWT, and rely on the
// summary text to call out the signature requirement.
var publicAPIPaths = map[string]map[string]bool{
	"GET": {
		"/api/v1/amenities":                          true,
		"/api/v1/properties":                         true,
		"/api/v1/properties/:id":                     true,
		"/api/v1/properties/:id/availability":        true,
		"/api/v1/properties/:id/calendar.ics":        true,
		"/api/v1/properties/:id/reviews":             true,
		"/api/v1/properties/:id/reviews/summary":     true,
		"/api/v1/shared/collections/:token":          true,
	},
	"POST": {
		"/api/v1/webhooks/payments/:provider": true,
		"/api/v1/webhooks/connect/:provider":  true,
		"/api/v1/webhooks/alerts":             true,
	},
}

// operationalPaths are the non-API endpoints exposed for ops tooling —
// always public, never under /api/v1, no per-user state.
var operationalPaths = map[string]bool{
	"/healthz":        true,
	"/readyz":         true,
	"/metrics":        true,
	"/openapi.yaml":   true,
	"/openapi.json":   true,
}

// Generate builds the OpenAPI document from the router's registered
// routes. The output is deterministic — paths and operations are sorted
// — so a regression test can diff two generations and spot drift.
func Generate(routes []Route, info Info) Document {
	doc := Document{
		OpenAPI: "3.0.3",
		Info:    info,
		Servers: []Server{
			{URL: "/", Description: "Same-origin (the host serving this spec)"},
		},
		Paths: map[string]PathItem{},
		Components: Components{
			SecuritySchemes: map[string]*SecurityScheme{
				"bearerAuth": {
					Type:         "http",
					Scheme:       "bearer",
					BearerFormat: "JWT",
					Description:  "Keycloak-issued access token. Required for all /api/v1/** routes except the public reads and signed webhooks.",
				},
			},
			Schemas: defaultSchemas(),
		},
	}

	tags := map[string]Tag{}
	for _, r := range routes {
		if r.Method == "" || r.Path == "" {
			continue
		}
		oaPath := convertPath(r.Path)
		item := doc.Paths[oaPath]

		tag := tagFromHandler(r.Handler)
		op := buildOperation(r, tag)
		setMethod(&item, r.Method, op)

		// Path parameters are attached at the PathItem level (per OpenAPI
		// 3.0 recommendation) so they aren't repeated on each method.
		if len(item.Parameters) == 0 {
			item.Parameters = pathParams(r.Path)
		}
		doc.Paths[oaPath] = item

		if tag != "" {
			tags[tag] = Tag{Name: tag}
		}
	}

	// Deterministic tag ordering for diffable output.
	names := make([]string, 0, len(tags))
	for n := range tags {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		doc.Tags = append(doc.Tags, tags[n])
	}
	return doc
}

// convertPath rewrites gin's :param syntax into OpenAPI's {param}
// syntax. Gin also supports *catch-all, which we don't currently use,
// but we translate it as {catchall} for safety so an accidental
// introduction doesn't produce an invalid spec.
func convertPath(gin string) string {
	if gin == "" {
		return gin
	}
	parts := strings.Split(gin, "/")
	for i, p := range parts {
		if strings.HasPrefix(p, ":") {
			parts[i] = "{" + p[1:] + "}"
		} else if strings.HasPrefix(p, "*") {
			parts[i] = "{" + p[1:] + "}"
		}
	}
	return strings.Join(parts, "/")
}

// pathParams extracts the named path parameters from a gin path
// template, producing a Parameter list ready to attach to a PathItem.
func pathParams(ginPath string) []Parameter {
	var out []Parameter
	for _, p := range strings.Split(ginPath, "/") {
		if !strings.HasPrefix(p, ":") && !strings.HasPrefix(p, "*") {
			continue
		}
		name := strings.TrimLeft(p, ":*")
		out = append(out, Parameter{
			Name:     name,
			In:       "path",
			Required: true,
			Schema:   &Schema{Type: "string"},
		})
	}
	return out
}

// tagFromHandler peels the type name out of a gin handler signature
// like "github.com/.../handler.(*PropertyHandler).Search-fm" and turns
// it into a lowercase tag (PropertyHandler → "property"). Falls back
// to "" when the handler doesn't follow the convention — e.g. inline
// closures from r.GET("/metrics", gin.WrapH(...)) — which then end up
// untagged, which is fine for ops endpoints.
func tagFromHandler(handler string) string {
	// Look for "(*XxxHandler)" anywhere in the symbol.
	i := strings.Index(handler, "(*")
	if i < 0 {
		return ""
	}
	rest := handler[i+2:]
	j := strings.Index(rest, ")")
	if j < 0 {
		return ""
	}
	typ := rest[:j] // "PropertyHandler"
	typ = strings.TrimSuffix(typ, "Handler")
	if typ == "" {
		return ""
	}
	// Lowercase first letter so it groups nicely in Swagger UI.
	r := []rune(typ)
	r[0] = unicode.ToLower(r[0])
	return string(r)
}

// operationIDFromHandler turns "(*PropertyHandler).Search-fm" into
// "propertySearch" — readable, unique-per-operation, and a stable
// identifier clients can codegen method names from.
func operationIDFromHandler(handler string) string {
	tag := tagFromHandler(handler)
	if tag == "" {
		// Synthesise something stable from the last component so the
		// document still has unique operationIds even for inline handlers.
		last := handler[strings.LastIndex(handler, "/")+1:]
		return strings.Map(func(r rune) rune {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
				return r
			}
			return -1
		}, last)
	}
	// Look for "Handler).Method" — the method name we want is after the ").".
	i := strings.Index(handler, ").")
	if i < 0 {
		return tag
	}
	method := handler[i+2:]
	method = strings.TrimSuffix(method, "-fm") // gin's method-value suffix
	if method == "" {
		return tag
	}
	return tag + method
}

// buildOperation assembles a single Operation. Security is decided by
// the path prefix: operational + webhooks + listed public reads = no
// security; everything else under /api/v1 requires bearerAuth.
func buildOperation(r Route, tag string) *Operation {
	op := &Operation{
		OperationID: operationIDFromHandler(r.Handler),
		Responses: map[string]Response{
			"200": {Description: "Successful response"},
			"400": errorResponse("Bad request"),
			"500": errorResponse("Server error"),
		},
	}
	if tag != "" {
		op.Tags = []string{tag}
	}
	// 201 instead of 200 for the obvious creators (POST that doesn't
	// just toggle state). We add 201 alongside 200 — the wire shape
	// proves which one the handler picks, this is documentation.
	if r.Method == "POST" {
		op.Responses["201"] = Response{Description: "Resource created"}
	}
	if requiresAuth(r.Method, r.Path) {
		op.Security = []SecurityRequirement{{"bearerAuth": []string{}}}
		op.Responses["401"] = errorResponse("Unauthenticated")
		op.Responses["403"] = errorResponse("Forbidden")
	}
	// Admin and host routes carry the prefix in the tag so a Swagger UI
	// user can immediately tell what gate they sit behind.
	if strings.HasPrefix(r.Path, "/api/v1/admin/") {
		op.Tags = append(op.Tags, "admin")
	} else if strings.HasPrefix(r.Path, "/api/v1/host/") {
		op.Tags = append(op.Tags, "host")
	}
	return op
}

// requiresAuth returns true when a route is gated by the auth
// middleware (and a client therefore needs a bearer token). It is
// fail-closed: anything not explicitly listed as public requires auth.
func requiresAuth(method, path string) bool {
	if operationalPaths[path] {
		return false
	}
	if !strings.HasPrefix(path, "/api/v1/") {
		// Anything outside /api/v1 that isn't an operational endpoint is
		// some odd ad-hoc route — keep it secured by default.
		return true
	}
	if exact, ok := publicAPIPaths[method]; ok && exact[path] {
		return false
	}
	return true
}

// errorResponse builds a Response that points at the shared Error
// envelope schema, so every documented failure shares the same shape.
func errorResponse(desc string) Response {
	return Response{
		Description: desc,
		Content: map[string]Media{
			"application/json": {Schema: &Schema{Ref: "#/components/schemas/Error"}},
		},
	}
}

// setMethod attaches op to the right slot on item. Kept as a switch
// rather than a map[string]**Operation because PathItem's method fields
// are typed pointers (per the spec) and we want a compile-time check.
func setMethod(item *PathItem, method string, op *Operation) {
	switch strings.ToUpper(method) {
	case "GET":
		item.Get = op
	case "POST":
		item.Post = op
	case "PUT":
		item.Put = op
	case "PATCH":
		item.Patch = op
	case "DELETE":
		item.Delete = op
	}
}

// defaultSchemas returns the small set of component schemas the
// document references. Limited to the response shapes every endpoint
// reuses — the per-DTO schemas are out of scope for S46.
func defaultSchemas() map[string]*Schema {
	return map[string]*Schema{
		// Error envelope — every failure path returns this shape, mapped
		// in interfaces/http/response/response.go.
		"Error": {
			Type: "object",
			Properties: map[string]*Schema{
				"error":   {Type: "string"},
				"code":    {Type: "string"},
				"details": {Type: "object"},
			},
			Required: []string{"error", "code"},
		},
		// Paged envelope — used by every List endpoint that paginates.
		"Page": {
			Type: "object",
			Properties: map[string]*Schema{
				"items": {Type: "array", Items: &Schema{Type: "object"}},
				"total": {Type: "integer", Format: "int64"},
			},
			Required: []string{"items", "total"},
		},
	}
}
