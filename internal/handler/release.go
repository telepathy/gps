package handler

import (
	"encoding/json"
	"fmt"
	"gps/internal/mock"
	"gps/internal/model"
	"gps/internal/sse"
	"gps/internal/store"
	"net/http"

	"github.com/gin-gonic/gin"
)

type ReleaseHandler struct {
	store     store.Store
	broker    *sse.Broker
	simulator *mock.Simulator
}

func NewReleaseHandler(store store.Store, broker *sse.Broker, simulator *mock.Simulator) *ReleaseHandler {
	return &ReleaseHandler{store: store, broker: broker, simulator: simulator}
}

// authorizeRelease enforces the release action plus silo-scope for a plan.
func (h *ReleaseHandler) authorizeRelease(c *gin.Context, planID string) bool {
	if !requireAction(c, h.store, model.ActionRelease) {
		return false
	}
	plan := h.store.GetPlan(planID)
	if plan == nil {
		c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"error": "plan not found"})
		return false
	}
	return requireSilos(c, currentUser(c, h.store), plan.SiloIDs)
}

func (h *ReleaseHandler) Execute(c *gin.Context) {
	planID := c.Param("id")
	if !h.authorizeRelease(c, planID) {
		return
	}
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

	ch := h.broker.Subscribe(planID)
	defer h.broker.Unsubscribe(planID, ch)

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
	if !h.authorizeRelease(c, planID) {
		return
	}
	h.simulator.Abort(planID)
	c.JSON(http.StatusOK, gin.H{"status": "aborted", "plan_id": planID})
}

func (h *ReleaseHandler) RetryModule(c *gin.Context) {
	planID := c.Param("id")
	moduleID := c.Param("mid")
	if !h.authorizeRelease(c, planID) {
		return
	}
	if err := h.simulator.RetryModule(planID, moduleID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "retrying", "plan_id": planID, "module_id": moduleID})
}
