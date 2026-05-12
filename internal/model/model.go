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
}

type Module struct {
	ID             string `json:"id"`
	RepoID         string `json:"repo_id"`
	SiloID         string `json:"silo_id"`
	Name           string `json:"name"`
	CurrentVersion string `json:"current_version"`
	Layer          int    `json:"layer"` // 0-4, for DAG layout
}

// --- Dependency Graph ---

type DepEdge struct {
	From string `json:"from"` // module_id depended upon
	To   string `json:"to"`   // module_id that depends on From
}

type DependencyGraph struct {
	Nodes       []string  `json:"nodes"`
	Edges       []DepEdge `json:"edges"`
	SortedOrder []string  `json:"sorted_order"`
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
	PhaseNone       ReleasePhase = "NONE"
	PhaseTagging    ReleasePhase = "TAGGING"
	PhaseAnalyzing  ReleasePhase = "ANALYZING"
	PhaseReleasing  ReleasePhase = "RELEASING"
	PhaseCompleted  ReleasePhase = "COMPLETED"
)

type FailureStrategy string

const (
	StrategyAbort FailureStrategy = "ABORT"
	StrategySkip  FailureStrategy = "SKIP"
	StrategyRetry FailureStrategy = "RETRY"
)

type PlanModuleEntry struct {
	ModuleID      string       `json:"module_id"`
	ModuleName    string       `json:"module_name"`
	RepoID        string       `json:"repo_id"`
	RepoName      string       `json:"repo_name"`
	SiloID        string       `json:"silo_id"`
	SiloName      string       `json:"silo_name"`
	Layer         int          `json:"layer"`
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
	PlanID       string       `json:"plan_id"`
	Phase        ReleasePhase `json:"phase"`
	Status       PlanStatus   `json:"status"`
	TotalModules int          `json:"total_modules"`
	Pending      int          `json:"pending"`
	Tagged       int          `json:"tagged"`
	Releasing    int          `json:"releasing"`
	Succeeded    int          `json:"succeeded"`
	Failed       int          `json:"failed"`
	Skipped      int          `json:"skipped"`
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
	ModuleID string       `json:"module_id"`
	Status   ModuleStatus `json:"status"`
	ErrorMsg string       `json:"error_msg,omitempty"`
}

type ModuleLogEvent struct {
	ModuleID  string `json:"module_id"`
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
	SiloIDs         []string          `json:"silo_ids" binding:"required"`
	DmsBranch       string            `json:"dms_branch" binding:"required"`
	Concurrency     int               `json:"concurrency"`
	FailureStrategy FailureStrategy   `json:"failure_strategy"`
	MaxRetries      int               `json:"max_retries"`
}

type UpdateVersionsRequest struct {
	Versions map[string]string `json:"versions"` // module_id -> version
}

// --- History ---

type HistoryEntry struct {
	PlanID      string     `json:"plan_id"`
	SiloIDs     []string   `json:"silo_ids"`
	SiloNames   []string   `json:"silo_names"`
	Status      PlanStatus `json:"status"`
	TotalModules int       `json:"total_modules"`
	Succeeded   int        `json:"succeeded"`
	Failed      int        `json:"failed"`
	Skipped     int        `json:"skipped"`
	Duration    string     `json:"duration"`
	CreatedAt   time.Time  `json:"created_at"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
}
