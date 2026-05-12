package handler

import (
	"gps/internal/mock"
	"net/http"

	"github.com/gin-gonic/gin"
)

type SiloHandler struct {
	store *mock.Store
}

func NewSiloHandler(store *mock.Store) *SiloHandler {
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

func (h *SiloHandler) GetModulesByRepo(c *gin.Context) {
	repoID := c.Param("id")
	modules := h.store.GetModulesByRepo(repoID)
	c.JSON(http.StatusOK, gin.H{"modules": modules})
}
