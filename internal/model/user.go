package model

import "time"

// --- User & RBAC ---

// Action constants — fine-grained permissions aggregated from a user's roles.
const (
	ActionManage  = "manage"  // 用户/角色管理
	ActionCreate  = "create"  // 创建发版计划
	ActionRelease = "release" // 执行/中止/重试发版、确认计划、改版本
	ActionView    = "view"    // 查看（列表、详情、进度、历史、SSE）
)

// Built-in role names.
const (
	RoleAdmin    = "admin"
	RoleReleaser = "releaser"
	RoleViewer   = "viewer"
)

// User is an authenticated identity, sourced from GitLab SSO (username only)
// or the built-in mock admin. RBAC is GPS-internal.
type User struct {
	ID           int       `json:"id"`
	Username     string    `json:"username"` // 来自 GitLab，唯一
	Email        string    `json:"email"`
	AvatarURL    string    `json:"avatar_url"`
	GitlabID     int64     `json:"gitlab_id"`
	Roles        []string  `json:"roles"`         // 角色名列表，如 ["releaser"]
	AllowedSilos string    `json:"allowed_silos"` // "" / "*" / "silo-001,silo-002"
	CreatedAt    time.Time `json:"created_at"`
}

// Role maps a name to a set of permitted actions.
type Role struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Actions     []string `json:"actions"` // manage/create/release/view
}

// GitlabUser is the subset of GitLab's /api/v4/user response GPS consumes.
type GitlabUser struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
	Email    string `json:"email"`
	Name     string `json:"name"`
	Avatar   string `json:"avatar_url"`
}

// --- Auth API Requests ---

type MockLoginRequest struct {
	Username string `json:"username" binding:"required"`
}

type SetUserRolesRequest struct {
	Roles []string `json:"roles"`
}

type UpdateUserAccessRequest struct {
	AllowedSilos string `json:"allowed_silos"`
}

// ImportUserEntry is one row in a batch import. Roles/AllowedSilos are optional;
// an empty Roles list defaults to [viewer]. Identity is still managed by GitLab
// SSO — imported users are pre-registered and bound to GitLab on first login.
type ImportUserEntry struct {
	Username     string   `json:"username"`
	Email        string   `json:"email"`
	Roles        []string `json:"roles"`
	AllowedSilos string   `json:"allowed_silos"`
}

type ImportUsersRequest struct {
	Users []ImportUserEntry `json:"users"`
}

type ImportUsersResult struct {
	Created []string          `json:"created"`
	Skipped []string          `json:"skipped"` // already exist
	Failed  map[string]string `json:"failed"`  // username -> reason
}
