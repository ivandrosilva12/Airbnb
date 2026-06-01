// Package handler contains the Gin HTTP handlers (the interface adapters).
package handler

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/airhost/backend/internal/domain/shared"
	"github.com/airhost/backend/internal/interfaces/http/middleware"
	"github.com/airhost/backend/internal/interfaces/http/response"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// pathUUID parses a UUID path parameter, writing a 400 on failure.
func pathUUID(c *gin.Context, name string) (uuid.UUID, bool) {
	return parseUUID(c, c.Param(name), name)
}

// parseUUID parses an arbitrary string as a UUID, writing a 400 on failure.
func parseUUID(c *gin.Context, raw, name string) (uuid.UUID, bool) {
	id, err := uuid.Parse(raw)
	if err != nil {
		response.FailMessage(c, 400, "invalid "+name)
		return uuid.Nil, false
	}
	return id, true
}

// pageFromQuery reads limit/offset query params into a normalised Page.
func pageFromQuery(c *gin.Context) shared.Page {
	limit, _ := strconv.Atoi(c.Query("limit"))
	offset, _ := strconv.Atoi(c.Query("offset"))
	return shared.NewPage(limit, offset)
}

// bindJSON decodes the JSON request body into dst. On failure it writes a
// consistent 400 with a generic message — never echoing the validator's raw
// error, which would leak struct field names and binding tags — and returns
// false so the caller can return early.
func bindJSON(c *gin.Context, dst any) bool {
	if err := c.ShouldBindJSON(dst); err != nil {
		// An oversized body (capped by the MaxBody middleware) is a 413, not a
		// generic 400, so clients can distinguish "too large" from "malformed".
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			response.FailMessage(c, http.StatusRequestEntityTooLarge, "request body too large")
			return false
		}
		response.FailMessage(c, http.StatusBadRequest, "invalid or malformed request body")
		return false
	}
	return true
}

// requireUser fetches the authenticated user or aborts with 401.
func requireUser(c *gin.Context) (uuid.UUID, bool) {
	u, ok := middleware.CurrentUser(c)
	if !ok {
		response.FailMessage(c, 401, "authentication required")
		return uuid.Nil, false
	}
	return u.ID, true
}

