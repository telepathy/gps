package handler

import (
	"gps/internal/store"
	"net/http"

	"github.com/gin-gonic/gin"
)

type SiloHandler struct {
	store store.Store
}

func NewSiloHandler(store store.Store) *SiloHandler {
	return &SiloHandler{store: store}
}

func (h *SiloHandler) ListSilos(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"silos": h.store.GetSilos()})
}

func (h *SiloHandler) GetReposBySilo(c *gin.Context) {
	siloID := c.Param("id")
	repos := h.store.GetReposBySilo(siloID)
	c.JSON(http.StatusOK, gin.H{"repos": repos})
}
