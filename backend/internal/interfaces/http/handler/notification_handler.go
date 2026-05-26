package handler

import (
	notificationapp "github.com/airhost/backend/internal/application/notification"
	"github.com/airhost/backend/internal/interfaces/http/dto"
	"github.com/airhost/backend/internal/interfaces/http/response"
	"github.com/gin-gonic/gin"
)

// NotificationHandler exposes in-app notification endpoints.
type NotificationHandler struct {
	svc *notificationapp.Service
}

// NewNotificationHandler builds a NotificationHandler.
func NewNotificationHandler(svc *notificationapp.Service) *NotificationHandler {
	return &NotificationHandler{svc: svc}
}

// List returns the authenticated user's notifications plus the unread count.
func (h *NotificationHandler) List(c *gin.Context) {
	userID, ok := requireUser(c)
	if !ok {
		return
	}
	res, err := h.svc.List(c.Request.Context(), userID, pageFromQuery(c))
	if err != nil {
		response.Fail(c, err)
		return
	}
	unread, err := h.svc.UnreadCount(c.Request.Context(), userID)
	if err != nil {
		response.Fail(c, err)
		return
	}
	items := make([]dto.NotificationView, 0, len(res.Items))
	for _, n := range res.Items {
		items = append(items, dto.FromNotification(n))
	}
	response.OK(c, dto.NotificationListView{Items: items, Total: res.Total, Unread: unread})
}

// MarkRead marks a single notification as read.
func (h *NotificationHandler) MarkRead(c *gin.Context) {
	userID, ok := requireUser(c)
	if !ok {
		return
	}
	id, ok := pathUUID(c, "id")
	if !ok {
		return
	}
	if err := h.svc.MarkRead(c.Request.Context(), userID, id); err != nil {
		response.Fail(c, err)
		return
	}
	response.NoContent(c)
}

// MarkUnread marks a single notification unread again.
func (h *NotificationHandler) MarkUnread(c *gin.Context) {
	userID, ok := requireUser(c)
	if !ok {
		return
	}
	id, ok := pathUUID(c, "id")
	if !ok {
		return
	}
	if err := h.svc.MarkUnread(c.Request.Context(), userID, id); err != nil {
		response.Fail(c, err)
		return
	}
	response.NoContent(c)
}

// MarkAllRead marks every notification for the user as read.
func (h *NotificationHandler) MarkAllRead(c *gin.Context) {
	userID, ok := requireUser(c)
	if !ok {
		return
	}
	if err := h.svc.MarkAllRead(c.Request.Context(), userID); err != nil {
		response.Fail(c, err)
		return
	}
	response.NoContent(c)
}
