// Package response centralises HTTP response shaping and domain-error mapping.
package response

import (
	"errors"
	"net/http"

	"github.com/airhost/backend/internal/domain/shared"
	"github.com/gin-gonic/gin"
)

// Error is the JSON error envelope returned to clients.
type Error struct {
	Error string `json:"error"`
	Code  string `json:"code"`
}

// OK writes a 200 JSON response.
func OK(c *gin.Context, body any) { c.JSON(http.StatusOK, body) }

// Created writes a 201 JSON response.
func Created(c *gin.Context, body any) { c.JSON(http.StatusCreated, body) }

// NoContent writes a 204 response.
func NoContent(c *gin.Context) { c.Status(http.StatusNoContent) }

// Fail maps a (domain) error to the appropriate HTTP status and aborts.
func Fail(c *gin.Context, err error) {
	status, code := classify(err)
	c.AbortWithStatusJSON(status, Error{Error: err.Error(), Code: code})
}

// FailMessage writes a custom status and message.
func FailMessage(c *gin.Context, status int, msg string) {
	c.AbortWithStatusJSON(status, Error{Error: msg, Code: http.StatusText(status)})
}

func classify(err error) (int, string) {
	switch {
	case errors.Is(err, shared.ErrNotFound):
		return http.StatusNotFound, "not_found"
	case errors.Is(err, shared.ErrConflict):
		return http.StatusConflict, "conflict"
	case errors.Is(err, shared.ErrValidation):
		return http.StatusUnprocessableEntity, "validation_error"
	case errors.Is(err, shared.ErrForbidden):
		return http.StatusForbidden, "forbidden"
	default:
		return http.StatusInternalServerError, "internal_error"
	}
}
