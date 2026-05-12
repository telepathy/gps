package handler

import (
	"encoding/json"
	"fmt"
	"gps/internal/mock"
	"net/http"

	"github.com/gin-gonic/gin"
)

type ReleaseHandler struct {
	store     *mock.Store
	simulator *mock.Simulator
}

func NewReleaseHandler(store *mock.Store, simulator *mock.Simulator) *ReleaseHandler {
	return &ReleaseHandler{store: store, simulator: simulator}
}

func (h *ReleaseHandler) Execute(c *gin.Context) {
	planID := c.Param("id")
	if err := h.simulator.Start(planID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "started", "plan_id": planID})
}

func (h *ReleaseHandler) GetProgress(c *gin.Context) {
	planID := c.Param("id")
	progress := h.store.GetProgress(planID)
	if progress == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "plan not found"})
		return
	}
	c.JSON(http.StatusOK, progress)
}

func (h *ReleaseHandler) SSEEvents(c *gin.Context) {
	planID := c.Param("id")

	w := c.Writer
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	ch := h.store.Subscribe(planID)
	defer h.store.Unsubscribe(planID, ch)

	clientGone := c.Request.Context().Done()

	// Send initial comment to confirm connection
	fmt.Fprintf(w, ": connected\n\n")
	w.(http.Flusher).Flush()

	for {
		select {
		case <-clientGone:
			return
		case event, ok := <-ch:
			if !ok {
				return
			}
			data, _ := json.Marshal(event)
			fmt.Fprintf(w, "data: %s\n\n", data)
			w.(http.Flusher).Flush()
		}
	}
}

func (h *ReleaseHandler) Abort(c *gin.Context) {
	planID := c.Param("id")
	h.simulator.Abort(planID)
	c.JSON(http.StatusOK, gin.H{"status": "aborted", "plan_id": planID})
}
