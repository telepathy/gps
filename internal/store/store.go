package store

import "gps/internal/model"

// Store abstracts the data access layer so that handlers and the simulator
// can work with either the in-memory (mock) or MySQL implementation.
type Store interface {
	// --- Product tree ---
	GetSilos() []model.Silo
	GetReposBySilo(siloID string) []model.Repo
	GetRepo(id string) *model.Repo
	GetSilo(id string) *model.Silo
	GetAllRepos() []model.Repo
	UpdateRepoBranch(repoID, branch string) (*model.Repo, error)

	// --- Plans ---
	CreatePlan(req model.CreatePlanRequest) *model.ReleasePlan
	GetPlan(id string) *model.ReleasePlan
	GetPlans() []*model.ReleasePlan
	UpdateVersions(planID string, versions map[string]string) error
	ConfirmPlan(planID string) error
	ConfirmPendingExternal(planID string, gas []string) error
	SetPlanRunning(planID string)
	SetPlanPhase(planID string, phase model.ReleasePhase)
	SetModuleStatus(planID, moduleID string, status model.ModuleStatus, errMsg string)
	CompletePlan(planID string, status model.PlanStatus)
	GetProgress(planID string) *model.ReleaseProgress

	// --- History ---
	GetHistory() []model.HistoryEntry

	// --- Users & RBAC ---
	FindOrCreateUser(in *model.User) (*model.User, bool, error)
	GetUserByUsername(username string) *model.User
	GetUserByID(id int) *model.User
	ListUsers() []*model.User
	CountUsers() int
	ImportUser(entry model.ImportUserEntry) (bool, error)
	GetRoles() []*model.Role
	SetUserRoles(userID int, roles []string) error
	SetUserAllowedSilos(userID int, allowedSilos string) error
	UserActions(u *model.User) map[string]bool
}
