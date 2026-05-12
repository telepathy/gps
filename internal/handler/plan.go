package handler

import (
	"gps/internal/mock"
	"gps/internal/model"
	"net/http"

	"github.com/gin-gonic/gin"
)

type PlanHandler struct {
	store *mock.Store
}

func NewPlanHandler(store *mock.Store) *PlanHandler {
	return &PlanHandler{store: store}
}

func (h *PlanHandler) CreatePlan(c *gin.Context) {
	var req model.CreatePlanRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	plan := h.store.CreatePlan(req)
	c.JSON(http.StatusCreated, plan)
}

func (h *PlanHandler) GetPlan(c *gin.Context) {
	planID := c.Param("id")
	plan := h.store.GetPlan(planID)
	if plan == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "plan not found"})
		return
	}
	c.JSON(http.StatusOK, plan)
}

func (h *PlanHandler) ListPlans(c *gin.Context) {
	plans := h.store.GetPlans()
	c.JSON(http.StatusOK, gin.H{"plans": plans})
}

func (h *PlanHandler) UpdateVersions(c *gin.Context) {
	planID := c.Param("id")
	var req model.UpdateVersionsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.store.UpdateVersions(planID, req.Versions); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	plan := h.store.GetPlan(planID)
	c.JSON(http.StatusOK, plan)
}

func (h *PlanHandler) ConfirmPlan(c *gin.Context) {
	planID := c.Param("id")
	if err := h.store.ConfirmPlan(planID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	plan := h.store.GetPlan(planID)
	c.JSON(http.StatusOK, plan)
}
