package shared

import "time"

// DriftCategory represents the category of an environment drift item.
type DriftCategory string

const (
	DriftCategoryRuntime     DriftCategory = "runtime"
	DriftCategoryTools       DriftCategory = "tools"
	DriftCategoryEnvVars     DriftCategory = "env_vars"
	DriftCategoryAgentConfig DriftCategory = "agent_config"
	DriftCategoryContext     DriftCategory = "context"
	DriftCategoryGit         DriftCategory = "git"
	DriftCategoryDocs        DriftCategory = "docs"
)

// DriftStatus represents the status of a drift item.
type DriftStatus string

const (
	DriftStatusMissing  DriftStatus = "missing"
	DriftStatusMismatch DriftStatus = "mismatch"
	DriftStatusOK       DriftStatus = "ok"
)

// DriftItem represents a single environment requirement that has drifted.
type DriftItem struct {
	Category DriftCategory `json:"category"`
	Item     string        `json:"item"`
	Expected string        `json:"expected"`
	Actual   string        `json:"actual"`
	Status   DriftStatus   `json:"status"`
}

// ProvisionStatus represents the overall status of a provisioning run.
type ProvisionStatus string

const (
	ProvisionStatusCompleted ProvisionStatus = "completed"
	ProvisionStatusPartial   ProvisionStatus = "partial"
	ProvisionStatusFailed    ProvisionStatus = "failed"
)

// ProvisionAction represents a single action taken during provisioning.
type ProvisionAction struct {
	Category DriftCategory `json:"category"`
	Item     string        `json:"item"`
	Action   string        `json:"action"`
	Path     string        `json:"path"`
}

// ProvisionPending represents a risky fix that requires manual approval.
type ProvisionPending struct {
	Category DriftCategory `json:"category"`
	Item     string        `json:"item"`
	Reason   string        `json:"reason"`
	Command  string        `json:"command"`
}

// ProvisionResult represents the structured result of a provisioning run.
type ProvisionResult struct {
	Status    ProvisionStatus    `json:"status"`
	Applied   []ProvisionAction  `json:"applied"`
	Pending   []ProvisionPending `json:"pending"`
	Timestamp time.Time          `json:"timestamp"`
}

// ProvisionEvent represents an event emitted during provisioning.
type ProvisionEvent struct {
	Type string             `json:"type"`
	Data ProvisionEventData `json:"data"`
}

// ProvisionEventData contains data for a provision event.
type ProvisionEventData struct {
	Category      DriftCategory `json:"category"`
	Item          string        `json:"item"`
	Expected      string        `json:"expected"`
	Actual        string        `json:"actual"`
	SuggestedCmd  string        `json:"suggested_command"`
	ApprovalToken string        `json:"approval_token"`
}
