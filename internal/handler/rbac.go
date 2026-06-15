package handler

import (
	"net/http"
	"strings"

	"gps/internal/model"
	"gps/internal/store"

	"github.com/gin-gonic/gin"
)

// currentUser resolves the authenticated user from the gin context (set by
// RequireAuth). Returns nil if absent or unknown.
func currentUser(c *gin.Context, store store.Store) *model.User {
	uid, ok := c.Get("user_id")
	if !ok {
		return nil
	}
	id, ok := uid.(int)
	if !ok {
		return nil
	}
	return store.GetUserByID(id)
}

// requireAction enforces that the current user's roles grant the given action.
// On failure it writes a 401/403 response and returns false.
func requireAction(c *gin.Context, store store.Store, action string) bool {
	u := currentUser(c, store)
	if u == nil {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return false
	}
	if !store.UserActions(u)[action] {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "permission denied: requires " + action})
		return false
	}
	return true
}

// canReleaseSilos reports whether the user may operate on every silo in the set.
// admin / AllowedSilos="*" pass unconditionally.
func canReleaseSilos(u *model.User, siloIDs []string) bool {
	for _, r := range u.Roles {
		if r == model.RoleAdmin {
			return true
		}
	}
	if u.AllowedSilos == "*" {
		return true
	}
	allowed := map[string]bool{}
	for _, s := range strings.Split(u.AllowedSilos, ",") {
		if s = strings.TrimSpace(s); s != "" {
			allowed[s] = true
		}
	}
	for _, want := range siloIDs {
		if !allowed[want] {
			return false
		}
	}
	return true
}

// requireSilos checks canReleaseSilos and writes 403 on failure.
func requireSilos(c *gin.Context, u *model.User, siloIDs []string) bool {
	if !canReleaseSilos(u, siloIDs) {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "permission denied: silo not allowed"})
		return false
	}
	return true
}
