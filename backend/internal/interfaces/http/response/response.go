// Package response centralises HTTP response shaping and domain-error mapping.
package response

import (
	"errors"
	"net/http"

	"github.com/airhost/backend/internal/domain/shared"
	"github.com/gin-gonic/gin"
)

// Error is the JSON error envelope returned to clients. Details is an
// optional structured payload that lets the client render a tailored UI
// (e.g. the threshold + currency that triggered a KYC step-up).
type Error struct {
	Error   string         `json:"error"`
	Code    string         `json:"code"`
	Details map[string]any `json:"details,omitempty"`
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
	out := Error{Error: err.Error(), Code: code}
	if details := extractDetails(err); details != nil {
		out.Details = details
	}
	c.AbortWithStatusJSON(status, out)
}

// FailMessage writes a custom status and message.
func FailMessage(c *gin.Context, status int, msg string) {
	c.AbortWithStatusJSON(status, Error{Error: msg, Code: http.StatusText(status)})
}

func classify(err error) (int, string) {
	switch {
	case errors.Is(err, shared.ErrKYCStepUpRequired):
		// Checked BEFORE ErrValidation: a KYCStepUpError satisfies both, and
		// we want the more specific code to win so the client can route to a
		// verify-and-retry flow instead of treating it as a generic invariant.
		return http.StatusUnprocessableEntity, "kyc_step_up_required"
	case errors.Is(err, shared.ErrPayoutOnboardingIncomplete):
		// S170 — checked BEFORE the generic ErrConflict branch so the client
		// receives a distinct code and routes the host (not the guest) to
		// resume Stripe Connect onboarding. The structured details payload
		// carries the host id so admin tooling can deep-link.
		return http.StatusConflict, "host_payout_onboarding_incomplete"
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

// extractDetails pulls structured fields out of typed domain errors so the
// client can render a tailored UI without parsing the error message. Returns
// nil for errors that don't carry extra data.
func extractDetails(err error) map[string]any {
	var stepUp *shared.KYCStepUpError
	if errors.As(err, &stepUp) {
		return map[string]any{
			"thresholdCents": stepUp.ThresholdCents,
			"currency":       stepUp.Currency,
		}
	}
	var payoutGate *shared.PayoutOnboardingIncompleteError
	if errors.As(err, &payoutGate) {
		// hostId always present; onboardingUrl is omitted when empty so
		// the client renders a generic "ask the host to finish setup"
		// nudge rather than a broken link.
		details := map[string]any{
			"hostId": payoutGate.HostID.String(),
		}
		if payoutGate.OnboardingURL != "" {
			details["onboardingUrl"] = payoutGate.OnboardingURL
		}
		return details
	}
	return nil
}
