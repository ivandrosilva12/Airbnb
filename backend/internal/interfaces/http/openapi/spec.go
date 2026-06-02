// Package openapi builds an OpenAPI 3.0 description of the live router.
//
// The document is generated from gin.RouteInfo at composition time, so it
// can never drift from the routes that are actually registered — anything
// you can call you can also discover in the spec, and anything in the spec
// resolves to a real handler. Hand-curating a separate YAML file (the
// usual approach) was rejected for exactly that drift risk; instead the
// "specification" is the route table, and this package serialises it.
//
// The coverage is intentionally skeletal: every route gets a method, path,
// path-param list, security requirement, tag and operationId, but request
// bodies and per-operation response schemas are left to a follow-up slice.
// That is enough for an external client to enumerate the surface, generate
// a typed client stub for the path+method shape, and learn which routes
// require a bearer token — the immediate value S46 was scoped to deliver.
package openapi

// Document is the root OpenAPI 3.0 object. Only the fields we actually
// emit are modelled — the goal is a faithful subset, not full coverage of
// the spec (which would dwarf the value here).
type Document struct {
	OpenAPI    string                `json:"openapi" yaml:"openapi"`
	Info       Info                  `json:"info" yaml:"info"`
	Servers    []Server              `json:"servers,omitempty" yaml:"servers,omitempty"`
	Tags       []Tag                 `json:"tags,omitempty" yaml:"tags,omitempty"`
	Paths      map[string]PathItem   `json:"paths" yaml:"paths"`
	Components Components            `json:"components,omitempty" yaml:"components,omitempty"`
	Security   []SecurityRequirement `json:"security,omitempty" yaml:"security,omitempty"`
}

// Info holds the document-level metadata. Version is the release/build
// tag the API was generated from, not the spec version (that's OpenAPI).
type Info struct {
	Title       string `json:"title" yaml:"title"`
	Version     string `json:"version" yaml:"version"`
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
}

// Server describes a base URL clients should target. The empty `{host}`
// placeholder lets a client point the generated stub at any environment
// without re-running the generator.
type Server struct {
	URL         string `json:"url" yaml:"url"`
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
}

// Tag groups operations by bounded context. The generator derives the tag
// from the handler type name (e.g. PropertyHandler → "property").
type Tag struct {
	Name        string `json:"name" yaml:"name"`
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
}

// PathItem holds the set of operations registered at a single path. We
// only emit methods that are actually routed — clients that read this
// document can rely on absence to mean "the server will reject this
// method with 404/405".
type PathItem struct {
	Get        *Operation `json:"get,omitempty" yaml:"get,omitempty"`
	Post       *Operation `json:"post,omitempty" yaml:"post,omitempty"`
	Put        *Operation `json:"put,omitempty" yaml:"put,omitempty"`
	Patch      *Operation `json:"patch,omitempty" yaml:"patch,omitempty"`
	Delete     *Operation `json:"delete,omitempty" yaml:"delete,omitempty"`
	Parameters []Parameter `json:"parameters,omitempty" yaml:"parameters,omitempty"`
}

// Operation is a single (method, path) entry. Responses always include
// the common error envelope refs so a client knows it never has to parse
// an unstructured error string.
type Operation struct {
	OperationID string                `json:"operationId" yaml:"operationId"`
	Tags        []string              `json:"tags,omitempty" yaml:"tags,omitempty"`
	Summary     string                `json:"summary,omitempty" yaml:"summary,omitempty"`
	Parameters  []Parameter           `json:"parameters,omitempty" yaml:"parameters,omitempty"`
	Security    []SecurityRequirement `json:"security,omitempty" yaml:"security,omitempty"`
	Responses   map[string]Response   `json:"responses" yaml:"responses"`
}

// Parameter describes a path or query parameter on an operation. Body
// parameters use OpenAPI 3.0's requestBody object — we don't model that
// here because individual request schemas are out of scope for S46.
type Parameter struct {
	Name        string  `json:"name" yaml:"name"`
	In          string  `json:"in" yaml:"in"` // path | query | header
	Required    bool    `json:"required,omitempty" yaml:"required,omitempty"`
	Description string  `json:"description,omitempty" yaml:"description,omitempty"`
	Schema      *Schema `json:"schema,omitempty" yaml:"schema,omitempty"`
}

// Response is a single status-code branch. Content carries the media-type
// → schema map.
type Response struct {
	Description string             `json:"description" yaml:"description"`
	Content     map[string]Media   `json:"content,omitempty" yaml:"content,omitempty"`
}

// Media bundles a schema with its media type. Only schema is used.
type Media struct {
	Schema *Schema `json:"schema,omitempty" yaml:"schema,omitempty"`
}

// Schema is a minimal JSON-schema subset — enough to express the few
// component shapes we ship (Error envelope + the paged-list wrapper).
type Schema struct {
	Ref        string             `json:"$ref,omitempty" yaml:"$ref,omitempty"`
	Type       string             `json:"type,omitempty" yaml:"type,omitempty"`
	Format     string             `json:"format,omitempty" yaml:"format,omitempty"`
	Properties map[string]*Schema `json:"properties,omitempty" yaml:"properties,omitempty"`
	Required   []string           `json:"required,omitempty" yaml:"required,omitempty"`
	Items      *Schema            `json:"items,omitempty" yaml:"items,omitempty"`
}

// Components hosts the reusable bits — securitySchemes and schemas — so
// per-operation objects can ref them rather than inline-repeating.
type Components struct {
	Schemas         map[string]*Schema         `json:"schemas,omitempty" yaml:"schemas,omitempty"`
	SecuritySchemes map[string]*SecurityScheme `json:"securitySchemes,omitempty" yaml:"securitySchemes,omitempty"`
}

// SecurityScheme describes one authentication option. The router only
// uses bearer JWTs (Keycloak), so the document only declares one.
type SecurityScheme struct {
	Type         string `json:"type" yaml:"type"`
	Scheme       string `json:"scheme,omitempty" yaml:"scheme,omitempty"`
	BearerFormat string `json:"bearerFormat,omitempty" yaml:"bearerFormat,omitempty"`
	Description  string `json:"description,omitempty" yaml:"description,omitempty"`
}

// SecurityRequirement is the per-operation list of OR'd auth options.
// {bearerAuth: []} means "this operation requires the bearerAuth scheme
// with no extra scopes" — the only shape we emit.
type SecurityRequirement map[string][]string
