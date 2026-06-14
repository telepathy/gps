package handler

import (
	"net/http"
	"strconv"

	"gps/internal/mock"
	"gps/internal/model"

	"github.com/gin-gonic/gin"
)

type AdminHandler struct {
	store *mock.Store
}

func NewAdminHandler(store *mock.Store) *AdminHandler {
	return &AdminHandler{store: store}
}

// ListUsers GET /api/admin/users
func (h *AdminHandler) ListUsers(c *gin.Context) {
	if !requireAction(c, h.store, model.ActionManage) {
		return
	}
	c.JSON(http.StatusOK, gin.H{"users": h.store.ListUsers()})
}

// ListRoles GET /api/admin/roles
func (h *AdminHandler) ListRoles(c *gin.Context) {
	if !requireAction(c, h.store, model.ActionManage) {
		return
	}
	c.JSON(http.StatusOK, gin.H{"roles": h.store.GetRoles()})
}

// ImportUsers POST /api/admin/users/import
// Pre-registers users (GitLab SSO still manages identity). Existing usernames
// are skipped; invalid entries are reported in `failed`.
func (h *AdminHandler) ImportUsers(c *gin.Context) {
	if !requireAction(c, h.store, model.ActionManage) {
		return
	}
	var req model.ImportUsersRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	result := model.ImportUsersResult{
		Created: []string{},
		Skipped: []string{},
		Failed:  map[string]string{},
	}
	for _, entry := range req.Users {
		created, err := h.store.ImportUser(entry)
		switch {
		case err != nil:
			key := entry.Username
			if key == "" {
				key = "(empty)"
			}
			result.Failed[key] = err.Error()
		case created:
			result.Created = append(result.Created, entry.Username)
		default:
			result.Skipped = append(result.Skipped, entry.Username)
		}
	}
	c.JSON(http.StatusOK, result)
}

// SetUserRoles PUT /api/admin/users/:uid/roles
func (h *AdminHandler) SetUserRoles(c *gin.Context) {
	if !requireAction(c, h.store, model.ActionManage) {
		return
	}
	uid, err := strconv.Atoi(c.Param("uid"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid user id"})
		return
	}
	var req model.SetUserRolesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.store.SetUserRoles(uid, req.Roles); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, h.store.GetUserByID(uid))
}

// UpdateUserAccess PUT /api/admin/users/:uid/access
func (h *AdminHandler) UpdateUserAccess(c *gin.Context) {
	if !requireAction(c, h.store, model.ActionManage) {
		return
	}
	uid, err := strconv.Atoi(c.Param("uid"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid user id"})
		return
	}
	var req model.UpdateUserAccessRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.store.SetUserAllowedSilos(uid, req.AllowedSilos); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, h.store.GetUserByID(uid))
}
