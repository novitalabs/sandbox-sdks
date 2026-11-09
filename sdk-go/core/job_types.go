package core

import "time"

// JobStatus preserves unknown future wire values. Reverting is not terminal.
type JobStatus string

const (
	JobMigrating JobStatus = "migrating"
	JobCompleted JobStatus = "completed"
	JobReverting JobStatus = "reverting"
	JobCancelled JobStatus = "cancelled"
	JobFailed    JobStatus = "failed"
)

func (s JobStatus) IsTerminal() bool { return s == JobCompleted || s == JobFailed || s == JobCancelled }

type JobType string

const (
	JobSandboxBatch JobType = "sandbox_batch"
	JobTemplate     JobType = "template"
	JobCustomer     JobType = "customer"
)

type MigrationItemStatus struct {
	ItemID       string    `json:"itemId"`
	ResourceType string    `json:"resourceType"`
	ResourceID   string    `json:"resourceId"`
	Status       JobStatus `json:"status"`
}

// MigrationStatus includes per-item results on Get/Cancel/Wait. An implicit
// base template is reported in Items but is not counted in Total.
type MigrationStatus struct {
	JobID     string                `json:"jobId"`
	Status    JobStatus             `json:"status"`
	Items     []MigrationItemStatus `json:"items"`
	Completed int                   `json:"completed"`
	Failed    int                   `json:"failed"`
	Cancelled int                   `json:"cancelled"`
	Total     int                   `json:"total"`
}
type JobPage struct {
	Items      []MigrationStatus `json:"items"`
	NextCursor string            `json:"nextCursor"`
}

// JobCreateOptions targets the region selected by the client's Domain/APIURL.
// TemplateAlias may be omitted for sandbox migration. Distribution requires a
// nonblank alias; multiple templates require AliasForResource. Aliases are
// preserved verbatim. Set at most one alias option.
type JobCreateOptions struct {
	SourceRegion     string
	TemplateAlias    *string
	AliasForResource func(resourceID string) string
}
type JobListOptions struct {
	ResourceID   string
	Type         JobType
	Status       JobStatus
	SourceRegion string
	TargetRegion string
	Limit        *int
	Cursor       string
}
type JobWaitOptions struct {
	// Timeout defaults to 30 minutes. A pointer to zero expires without polling.
	Timeout *time.Duration
	// PollInterval defaults to 10 seconds when zero.
	PollInterval time.Duration
	// OnProgress runs synchronously for every poll, including the terminal one.
	OnProgress func(MigrationStatus)
}
