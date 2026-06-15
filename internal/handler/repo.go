package handler

import (
	"net/http"

	"gps/internal/model"
	"gps/internal/store"

	"github.com/gin-gonic/gin"
)

type RepoHandler struct {
	store store.Store
}

func NewRepoHandler(store store.Store) *RepoHandler {
	return &RepoHandler{store: store}
}

// ListRepos GET /api/repos
// Returns all repos enriched with silo name and a per-user can_edit flag.
// Visible to any authenticated user; editability is gated by silo-scope.
func (h *RepoHandler) ListRepos(c *gin.Context) {
	u := currentUser(c, h.store)
	if u == nil {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	canRelease := h.store.UserActions(u)[model.ActionRelease]

	siloNames := map[string]string{}
	for _, s := range h.store.GetSilos() {
		siloNames[s.ID] = s.Name
	}

	repos := h.store.GetAllRepos()
	views := make([]model.RepoView, 0, len(repos))
	for _, r := range repos {
		views = append(views, model.RepoView{
			Repo:     r,
			SiloName: siloNames[r.SiloID],
			CanEdit:  canRelease && canReleaseSilos(u, []string{r.SiloID}),
		})
	}
	c.JSON(http.StatusOK, gin.H{"repos": views})
}

// UpdateRepoBranch PUT /api/repos/:id/branch
// Requires the release action plus silo-scope over the repo's silo.
func (h *RepoHandler) UpdateRepoBranch(c *gin.Context) {
	if !requireAction(c, h.store, model.ActionRelease) {
		return
	}
	repoID := c.Param("id")
	repo := h.store.GetRepo(repoID)
	if repo == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "repo not found"})
		return
	}
	if !requireSilos(c, currentUser(c, h.store), []string{repo.SiloID}) {
		return
	}
	var req model.UpdateRepoBranchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	updated, err := h.store.UpdateRepoBranch(repoID, req.ReleaseBranch)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, updated)
}
