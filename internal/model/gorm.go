package model

import (
	"time"

	"gorm.io/datatypes"
)

// Table name mappings (gps_ prefix).

func (GPSSilo) TableName() string                { return "gps_silos" }
func (GPSRepo) TableName() string                { return "gps_repos" }
func (GPSReleasePlan) TableName() string         { return "gps_release_plans" }
func (GPSPlanModule) TableName() string          { return "gps_plan_modules" }
func (GPSPlanDepEdge) TableName() string         { return "gps_plan_dep_edges" }
func (GPSPlanTopoOrder) TableName() string       { return "gps_plan_topo_orders" }
func (GPSPlanGradleSubproject) TableName() string { return "gps_plan_gradle_subprojects" }
func (GPSReleaseHistory) TableName() string      { return "gps_release_histories" }
func (GPSUser) TableName() string                { return "gps_users" }
func (GPSRole) TableName() string                { return "gps_roles" }
func (GPSUserRole) TableName() string            { return "gps_user_roles" }

// --- Product tree (global, from dalaran) ---

type GPSSilo struct {
	ID   string `gorm:"primaryKey;size:64"`
	Name string `gorm:"size:128;not null"`
	Desc string `gorm:"size:512"`
}

type GPSRepo struct {
	ID            string `gorm:"primaryKey;size:64"`
	SiloID        string `gorm:"size:64;index;not null"`
	Name          string `gorm:"size:256;not null"`
	URL           string `gorm:"size:512"`
	ReleaseBranch string `gorm:"size:128;not null;default:main"`
	JDK           string `gorm:"size:8;not null;default:17"`
}

// --- Release plan ---

type GPSReleasePlan struct {
	ID              string                       `gorm:"primaryKey;size:64"`
	SiloIDs         datatypes.JSONType[[]string] `gorm:"not null"`
	DmsBranch       string                       `gorm:"size:128;not null"`
	Concurrency     int                          `gorm:"not null;default:4"`
	FailureStrategy string                       `gorm:"size:16;not null;default:ABORT"`
	MaxRetries      int                          `gorm:"not null;default:3"`
	Status          string                       `gorm:"size:16;not null;default:DRAFT;index"`
	Phase           string                       `gorm:"size:32;not null;default:NONE"`
	CreatedAt       time.Time                    `gorm:"autoCreateTime"`
	StartedAt       *time.Time
	CompletedAt     *time.Time
}

// GPSPlanModule is the per-module record within a plan.
// PK is (PlanID, GA) — GA is the module's canonical identity.
type GPSPlanModule struct {
	PlanID        string     `gorm:"primaryKey;size:64"`
	GA            string     `gorm:"primaryKey;size:255;column:ga"` // group:artifact
	ModuleName    string     `gorm:"size:256;not null"`
	Kind          string     `gorm:"size:24;not null;default:internal"` // internal | pending-external
	GroupID       string     `gorm:"size:128;not null"`
	Artifact      string     `gorm:"size:128;not null;index"`
	GradlePath    string     `gorm:"size:255"`
	RepoID        string     `gorm:"size:64;index"`
	RepoName      string     `gorm:"size:256"`
	SiloID        string     `gorm:"size:64"`
	SiloName      string     `gorm:"size:128"`
	PrevVersion   string     `gorm:"size:32"`
	TargetVersion string     `gorm:"size:32"`
	IsOverridden  bool       `gorm:"not null;default:false"`
	Status        string     `gorm:"size:16;not null;default:PENDING;index"`
	PipelineID    string     `gorm:"size:128"`
	StartTime     *time.Time
	EndTime       *time.Time
	ErrorMsg      string `gorm:"type:text"`
	RetryCount    int    `gorm:"not null;default:0"`
}

// GPSPlanDepEdge is a dependency edge within a plan.
// FromGA is depended upon by ToGA.
type GPSPlanDepEdge struct {
	PlanID    string `gorm:"primaryKey;size:64"`
	FromGA    string `gorm:"primaryKey;size:255;column:from_ga"`
	ToGA      string `gorm:"primaryKey;size:255;column:to_ga"`
	CrossRepo bool   `gorm:"not null;default:false"`
}

// GPSPlanTopoOrder stores the topological sort result for a plan.
type GPSPlanTopoOrder struct {
	PlanID string `gorm:"primaryKey;size:64"`
	Seq    int    `gorm:"primaryKey"`
	GA     string `gorm:"size:255;not null"`
}

// GPSPlanGradleSubproject stores the gradlePath→GA mapping per repo for audit.
type GPSPlanGradleSubproject struct {
	PlanID     string `gorm:"primaryKey;size:64"`
	RepoID     string `gorm:"primaryKey;size:64"`
	GradlePath string `gorm:"primaryKey;size:255"`
	GA         string `gorm:"size:255;not null"`
}

// --- History ---

type GPSReleaseHistory struct {
	PlanID       string                       `gorm:"primaryKey;size:64"`
	SiloIDs      datatypes.JSONType[[]string] `gorm:"not null"`
	SiloNames    datatypes.JSONType[[]string] `gorm:"not null"`
	Status       string                       `gorm:"size:16;not null"`
	TotalModules int                          `gorm:"not null"`
	Succeeded    int                          `gorm:"not null;default:0"`
	Failed       int                          `gorm:"not null;default:0"`
	Skipped      int                          `gorm:"not null;default:0"`
	Duration     string                       `gorm:"size:32"`
	CreatedAt    time.Time                    `gorm:"autoCreateTime"`
	CompletedAt  *time.Time
}

// --- Users & RBAC ---

type GPSUser struct {
	ID           int                          `gorm:"primaryKey;autoIncrement"`
	Username     string                       `gorm:"size:128;uniqueIndex;not null"`
	Name         string                       `gorm:"size:256"`
	Email        string                       `gorm:"size:256"`
	AvatarURL    string                       `gorm:"size:512"`
	GitlabID     int64                        `gorm:"index"`
	Roles        datatypes.JSONType[[]string] `gorm:"not null"`
	AllowedSilos string                       `gorm:"size:512"`
	CreatedAt    time.Time                    `gorm:"autoCreateTime"`
}

type GPSRole struct {
	Name        string                       `gorm:"primaryKey;size:64"`
	Description string                       `gorm:"size:256"`
	Actions     datatypes.JSONType[[]string] `gorm:"not null"`
}

type GPSUserRole struct {
	UserID   int    `gorm:"primaryKey"`
	RoleName string `gorm:"primaryKey;size:64"`
}
