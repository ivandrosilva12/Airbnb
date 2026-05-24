package handler

import (
	"fmt"
	"net/http"
	"time"

	"github.com/airhost/backend/internal/infrastructure/realtime"
	"github.com/gin-gonic/gin"
)

// heartbeatInterval keeps idle SSE connections (and intermediaries) alive.
const heartbeatInterval = 25 * time.Second

// RealtimeHandler streams live updates to the authenticated user over SSE.
type RealtimeHandler struct {
	hub *realtime.Hub
}

// NewRealtimeHandler builds a RealtimeHandler.
func NewRealtimeHandler(hub *realtime.Hub) *RealtimeHandler { return &RealtimeHandler{hub: hub} }

// Stream is a Server-Sent Events endpoint. Because the browser EventSource API
// cannot set an Authorization header, the auth middleware also accepts the
// token via the access_token query parameter for this route.
func (h *RealtimeHandler) Stream(c *gin.Context) {
	userID, ok := requireUser(c)
	if !ok {
		return
	}
	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "streaming unsupported"})
		return
	}

	ch, cancel := h.hub.Subscribe(userID)
	defer cancel()

	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("X-Accel-Buffering", "no") // disable proxy buffering
	c.Writer.WriteHeader(http.StatusOK)
	fmt.Fprint(c.Writer, ": connected\n\n")
	flusher.Flush()

	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()

	ctx := c.Request.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case payload, open := <-ch:
			if !open {
				return
			}
			fmt.Fprintf(c.Writer, "data: %s\n\n", payload)
			flusher.Flush()
		case <-ticker.C:
			fmt.Fprint(c.Writer, ": ping\n\n")
			flusher.Flush()
		}
	}
}
