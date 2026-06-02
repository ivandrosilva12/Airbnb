package openapi

import (
	"net/http"
	"sync/atomic"

	"github.com/gin-gonic/gin"
)

// Handler serves the cached OpenAPI document in both YAML and JSON. The
// cache is filled by Refresh once the router has finished registering
// routes — see NewRouter's call to OpenAPI.Refresh near the bottom.
//
// Using atomic.Value (rather than baking the bytes in at construction)
// solves a chicken-and-egg: the two openapi.* routes need to exist
// BEFORE Generate runs (so they appear in the spec), which means we
// can't have the bytes ready at handler-construction time. Atomic
// publication keeps the read path lock-free.
type Handler struct {
	yamlPayload atomic.Value // []byte
	jsonPayload atomic.Value // []byte
}

// NewHandler returns a Handler with empty payloads. Callers MUST invoke
// Refresh once routes are registered; until they do, the endpoints
// return an empty body with 503 so the failure mode is loud, not a
// silent empty spec.
func NewHandler() *Handler { return &Handler{} }

// Refresh regenerates the cached document from the current route set.
// Safe to call concurrently — atomic.Value handles the publication.
func (h *Handler) Refresh(routes []Route, info Info) error {
	doc := Generate(routes, info)
	y, err := MarshalYAML(doc)
	if err != nil {
		return err
	}
	j, err := MarshalJSON(doc)
	if err != nil {
		return err
	}
	h.yamlPayload.Store(y)
	h.jsonPayload.Store(j)
	return nil
}

// ServeYAML writes the cached YAML document. 503 until Refresh runs.
func (h *Handler) ServeYAML(c *gin.Context) {
	b, _ := h.yamlPayload.Load().([]byte)
	if len(b) == 0 {
		c.Status(http.StatusServiceUnavailable)
		return
	}
	c.Data(http.StatusOK, "application/yaml; charset=utf-8", b)
}

// ServeJSON writes the cached JSON document. 503 until Refresh runs.
func (h *Handler) ServeJSON(c *gin.Context) {
	b, _ := h.jsonPayload.Load().([]byte)
	if len(b) == 0 {
		c.Status(http.StatusServiceUnavailable)
		return
	}
	c.Data(http.StatusOK, "application/json; charset=utf-8", b)
}
