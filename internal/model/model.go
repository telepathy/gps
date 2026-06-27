package model

import (
	"time"
)

// --- Product Tree ---

type Silo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Desc string `json:"desc"`
}

type Repo struct {
	ID            string `json:"id"`
	SiloID        string `json:"silo_id"`
	Name          string `json:"name"`
	URL           string `json:"url"`
	ReleaseBranch string `json:"release_branch"`
	JDK           string `json:"jdk"` // JDK 大版本（"8"/"17"/"21"），默认 "21"
}

// Module represents a Gradle subproject identified by GA (group:artifact).
// Modules are plan-scoped — they are generated at plan creation time, not globally.
type Module struct {
	ID             string `json:"id"`              // = GA "group:artifact"
	Group          string `json:"group"`            // Maven group (e.g. "com.csdc.spot")
	Artifact       string `json:"artifact"`         // Maven artifact (= akasha join key)
	GradlePath     string `json:"gradle_path"`      // ":core:api", empty for pending-external
	RepoID         string `json:"repo_id"`          // empty for pending-external
	SiloID         string `json:"silo_id"`          // empty for pending-external
	Name           string `json:"name"`             // display name
	Kind           string `json:"kind"`             // "internal" | "pending-external"
	CurrentVersion string `json:"current_version"`  // internal: release version; pending-external: confirmed akasha version
}

// Node kind constants.
const (
	KindInternal        = "internal"
	KindPendingExternal = "pending-external"
)

// RepoView is a Repo enriched with its silo name and a per-request CanEdit flag
// (whether the current user may configure this repo's release branch).
type RepoView struct {
	Repo
	SiloName string `json:"silo_name"`
	CanEdit  bool   `json:"can_edit"`
}

// UpdateRepoBranchRequest sets a repo's release branch.
type UpdateRepoBranchRequest struct {
	ReleaseBranch string `json:"release_branch" binding:"required"`
}

// UpdateRepoJDKRequest sets a repo's JDK version.
type UpdateRepoJDKRequest struct {
	JDK string `json:"jdk" binding:"required"`
}

// ActiveBranchResponse is returned by the active-branch lookup API.
type ActiveBranchResponse struct {
	RepositoryPath string `json:"repositoryPath"`
	ActiveBranch   string `json:"activeBranch"`
}

// SyncResult reports the outcome of reconciling the local product tree
// with the latest data from dalaran.
type SyncResult struct {
	SilosAdded   int `json:"silos_added"`
	SilosDeleted int `json:"silos_deleted"`
	ReposAdded   int `json:"repos_added"`
	ReposDeleted int `json:"repos_deleted"`
}

// --- Dependency Graph ---

// DepEdge represents a dependency edge between two modules (GA nodes).
// From is depended upon by To (To depends on From, so From must release first).
type DepEdge struct {
	From      string `json:"from"`       // GA of the depended-upon module
	To        string `json:"to"`         // GA of the dependent module
	CrossRepo bool   `json:"cross_repo"` // true = cross-repo (via akasha); false = repo-internal
}

type DependencyGraph struct {
	Nodes       []string  `json:"nodes"`        // GA list
	Edges       []DepEdge `json:"edges"`
	SortedOrder []string  `json:"sorted_order"` // GA topo order
}

// --- Release Plan ---

type ModuleStatus string

const (
	StatusPending   ModuleStatus = "PENDING"
	StatusTagged    ModuleStatus = "TAGGED"
	StatusReleasing ModuleStatus = "RELEASING"
	StatusSuccess   ModuleStatus = "SUCCESS"
	StatusFailed    ModuleStatus = "FAILED"
	StatusSkipped   ModuleStatus = "SKIPPED"
	StatusRetrying  ModuleStatus = "RETRYING"
)

type PlanStatus string

const (
	PlanDraft     PlanStatus = "DRAFT"
	PlanConfirmed PlanStatus = "CONFIRMED"
	PlanRunning   PlanStatus = "RUNNING"
	PlanCompleted PlanStatus = "COMPLETED"
	PlanAborted   PlanStatus = "ABORTED"
)

type ReleasePhase string

const (
	PhaseNone                    ReleasePhase = "NONE"
	PhaseTagging                 ReleasePhase = "TAGGING"
	PhaseAnalyzing               ReleasePhase = "ANALYZING"
	PhaseGatePendingExternal     ReleasePhase = "GATE_PENDING_EXTERNAL"
	PhaseReleasing               ReleasePhase = "RELEASING"
	PhaseCompleted               ReleasePhase = "COMPLETED"
)

type FailureStrategy string

const (
	StrategyAbort FailureStrategy = "ABORT"
	StrategySkip  FailureStrategy = "SKIP"
	StrategyRetry FailureStrategy = "RETRY"
)

// PlanModuleEntry is a per-module record within a release plan.
type PlanModuleEntry struct {
	ModuleID      string       `json:"module_id"`     // = GA
	ModuleName    string       `json:"module_name"`
	Kind          string       `json:"kind"`          // "internal" | "pending-external"
	Group         string       `json:"group"`
	Artifact      string       `json:"artifact"`
	GradlePath    string       `json:"gradle_path"`
	RepoID        string       `json:"repo_id"`       // empty for pending-external
	RepoName      string       `json:"repo_name"`
	SiloID        string       `json:"silo_id"`       // empty for pending-external
	SiloName      string       `json:"silo_name"`
	PrevVersion   string       `json:"prev_version"`
	TargetVersion string       `json:"target_version"`
	IsOverridden  bool         `json:"is_overridden"`
	Status        ModuleStatus `json:"status"`
	PipelineID    string       `json:"pipeline_id,omitempty"`
	StartTime     *time.Time   `json:"start_time,omitempty"`
	EndTime       *time.Time   `json:"end_time,omitempty"`
	ErrorMsg      string       `json:"error_msg,omitempty"`
	RetryCount    int          `json:"retry_count"`
}

type ReleasePlan struct {
	ID              string            `json:"id"`
	SiloIDs         []string          `json:"silo_ids"`
	DmsBranch       string            `json:"dms_branch"`
	Concurrency     int               `json:"concurrency"`
	FailureStrategy FailureStrategy   `json:"failure_strategy"`
	MaxRetries      int               `json:"max_retries"`
	Status          PlanStatus        `json:"status"`
	Phase           ReleasePhase      `json:"phase"`
	Modules         []PlanModuleEntry `json:"modules"`
	DepGraph        *DependencyGraph  `json:"dep_graph,omitempty"`
	CreatedAt       time.Time         `json:"created_at"`
	StartedAt       *time.Time        `json:"started_at,omitempty"`
	CompletedAt     *time.Time        `json:"completed_at,omitempty"`
}

// --- Release Progress ---

type ReleaseProgress struct {
	PlanID       string            `json:"plan_id"`
	Phase        ReleasePhase      `json:"phase"`
	Status       PlanStatus        `json:"status"`
	TotalModules int               `json:"total_modules"`
	Pending      int               `json:"pending"`
	Tagged       int               `json:"tagged"`
	Releasing    int               `json:"releasing"`
	Succeeded    int               `json:"succeeded"`
	Failed       int               `json:"failed"`
	Skipped      int               `json:"skipped"`
	Modules      []PlanModuleEntry `json:"modules"`
}

// --- SSE Events ---

type SSEEvent struct {
	Type string      `json:"type"` // phase_change, module_status, module_log, plan_complete
	Data interface{} `json:"data"`
}

type PhaseChangeEvent struct {
	Phase ReleasePhase `json:"phase"`
}

type ModuleStatusEvent struct {
	ModuleID string       `json:"module_id"` // GA
	Status   ModuleStatus `json:"status"`
	ErrorMsg string       `json:"error_msg,omitempty"`
}

type ModuleLogEvent struct {
	ModuleID  string `json:"module_id"` // GA
	Line      string `json:"line"`
	Timestamp int64  `json:"timestamp"`
}

type PlanCompleteEvent struct {
	Status    PlanStatus `json:"status"`
	Succeeded int        `json:"succeeded"`
	Failed    int        `json:"failed"`
	Skipped   int        `json:"skipped"`
}

// --- API Requests ---

type CreatePlanRequest struct {
	SiloIDs         []string        `json:"silo_ids" binding:"required"`
	DmsBranch       string          `json:"dms_branch" binding:"required"`
	Concurrency     int             `json:"concurrency"`
	FailureStrategy FailureStrategy `json:"failure_strategy"`
	MaxRetries      int             `json:"max_retries"`
}

type UpdateVersionsRequest struct {
	Versions map[string]string `json:"versions"` // repo_id -> version
}

// ConfirmExternalRequest confirms that pending-external modules exist at
// the correct version in the specified akasha branch.
type ConfirmExternalRequest struct {
	GAs []string `json:"gas"` // list of GA strings to confirm
}

// --- History ---

type HistoryEntry struct {
	PlanID       string     `json:"plan_id"`
	SiloIDs      []string   `json:"silo_ids"`
	SiloNames    []string   `json:"silo_names"`
	Status       PlanStatus `json:"status"`
	TotalModules int        `json:"total_modules"`
	Succeeded    int        `json:"succeeded"`
	Failed       int        `json:"failed"`
	Skipped      int        `json:"skipped"`
	Duration     string     `json:"duration"`
	CreatedAt    time.Time  `json:"created_at"`
	CompletedAt  *time.Time `json:"completed_at,omitempty"`
}
