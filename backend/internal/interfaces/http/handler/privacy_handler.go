package handler

import (
	privacyapp "github.com/airhost/backend/internal/application/privacy"
	"github.com/airhost/backend/internal/interfaces/http/dto"
	"github.com/airhost/backend/internal/interfaces/http/response"
	"github.com/gin-gonic/gin"
)

// PrivacyHandler exposes GDPR self-service endpoints.
type PrivacyHandler struct {
	svc *privacyapp.Service
}

// NewPrivacyHandler builds a PrivacyHandler.
func NewPrivacyHandler(svc *privacyapp.Service) *PrivacyHandler { return &PrivacyHandler{svc: svc} }

// Export returns all personal data the platform holds for the authenticated
// user as a downloadable JSON document (GDPR right of access / portability).
func (h *PrivacyHandler) Export(c *gin.Context) {
	id, ok := requireUser(c)
	if !ok {
		return
	}
	exp, err := h.svc.Export(c.Request.Context(), id)
	if err != nil {
		response.Fail(c, err)
		return
	}
	c.Header("Content-Disposition", `attachment; filename="airhost-data-export.json"`)
	response.OK(c, dto.FromExport(exp))
}

// Erase fulfils a right-to-erasure request for the authenticated user: the
// profile is anonymised, the account deactivated and preference data removed.
func (h *PrivacyHandler) Erase(c *gin.Context) {
	id, ok := requireUser(c)
	if !ok {
		return
	}
	if err := h.svc.Erase(c.Request.Context(), id); err != nil {
		response.Fail(c, err)
		return
	}
	response.NoContent(c)
}
