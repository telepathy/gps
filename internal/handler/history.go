package handler

import (
	"gps/internal/store"
	"net/http"

	"github.com/gin-gonic/gin"
)

type HistoryHandler struct {
	store store.Store
}

func NewHistoryHandler(store store.Store) *HistoryHandler {
	return &HistoryHandler{store: store}
}

func (h *HistoryHandler) ListHistory(c *gin.Context) {
	history := h.store.GetHistory()
	c.JSON(http.StatusOK, gin.H{"history": history})
}

func (h *HistoryHandler) GetHistoryDetail(c *gin.Context) {
	planID := c.Param("id")
	// For history, check plans store first, then provide snapshot from history
	plan := h.store.GetPlan(planID)
	if plan != nil {
		c.JSON(http.StatusOK, plan)
		return
	}
	// Return from history entries
	for _, entry := range h.store.GetHistory() {
		if entry.PlanID == planID {
			c.JSON(http.StatusOK, entry)
			return
		}
	}
	c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
}
