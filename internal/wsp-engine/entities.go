package workspaceEngine

import (
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
)

type Workflow struct {
	Id              uuid.UUID
	ExternalId 		int64  // an id of a workflow defined outside the engine, e.g. of an OK platform workflow.
	Name            string
	Status          string
	WorkspaceId     uuid.UUID
	Timestamp       time.Time
	Path            string
	Type            string
	HttpUrlToRepo   string
}

type WorkflowSchedule struct {
	Id             uuid.UUID
	WorkflowId     uuid.UUID
	Start          bool
	End            bool
	Timestamp      time.Time
	Name           string
	CronExpression string
	TimeZone       string
}

type WorkflowParameter struct {
	Id            uuid.UUID
	WorkflowId    uuid.UUID
	Name          string
	Description   string
	Usage         string
	Displayed     bool
	Type          string
	Format        string
	Default       string
	Reqiured      bool
	AllowedValues pq.StringArray `gorm:"type:text[]"`
	Masked        bool
	ActualValues  pq.StringArray `gorm:"type:text[]"`
}

type App struct {
	Id                int64          `json:"id"`
	ProjectId         int64          `json:"project_id"`
	Name              string         `json:"name,omitempty"`
	NameWithNamespace string         `json:"name_with_namespace,omitempty"`
	Description       string         `json:"description,omitempty"`
	Path              string         `json:"path,omitempty"`
	PathWithNamespace string         `json:"path_with_namespace,omitempty"`
	DefaultBranch     string         `json:"default_branch,omitempty"`
	Topics            pq.StringArray `gorm:"type:text[]" json:"topics,omitempty"`
	HttpUrlToRepo     string         `json:"http_url_to_repo,omitempty"`
	WebUrl            string         `json:"web_url,omitempty"`
	StartCount        float32        `json:"start_count,omitempty"`
	Parameters        []AppParameter `json:"parameters,omitempty"`
	Type              string         `json:"type"`
	Status            string
}

type AppParameter struct {
	Id            int64 `json:"id"`
	AppId         int64
	Name          string         `json:"name,omitempty"`
	Description   string         `json:"description,omitempty"`
	Usage         string         `json:"usage,omitempty"`
	Displayed     bool           `json:"displayed,omitempty"`
	Type          string         `json:"type,omitempty"`
	Format        string         `json:"format,omitempty"`
	Default       string         `json:"default,omitempty"`
	Required      bool           `json:"required,omitempty"`
	AllowedValues pq.StringArray `gorm:"type:text[]" json:"allowed_values,omitempty"`
	Masked        bool           `json:"masked,omitempty"`
	ActualValues  pq.StringArray `gorm:"type:text[]" json:"actual_values,omitempty"`
}