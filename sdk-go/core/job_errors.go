package core

import (
	"encoding/json"
	"fmt"
)

// MigrationConflictError preserves reason codes, including unknown values.
type MigrationConflictError struct {
	Message string
	Reason  string
}

func (e *MigrationConflictError) Error() string { return e.Message }

type TemplateAliasConflictError struct{ MigrationConflictError }

func (e *TemplateAliasConflictError) Unwrap() error { return &e.MigrationConflictError }

type ResourceAlreadyMigratingError struct{ MigrationConflictError }

func (e *ResourceAlreadyMigratingError) Unwrap() error { return &e.MigrationConflictError }

type CancelTooLateError struct{ MigrationConflictError }

func (e *CancelTooLateError) Unwrap() error { return &e.MigrationConflictError }

type SharedDependencyError struct{ MigrationConflictError }

func (e *SharedDependencyError) Unwrap() error { return &e.MigrationConflictError }

type MigrationNotSupportedError struct{ Message string }

func (e *MigrationNotSupportedError) Error() string { return e.Message }

type JobFailedError struct{ Status MigrationStatus }

func (e *JobFailedError) Error() string {
	return fmt.Sprintf("migration job %s failed: %d/%d items failed", e.Status.JobID, e.Status.Failed, e.Status.Total)
}

type JobCancelledError struct{ Status MigrationStatus }

func (e *JobCancelledError) Error() string {
	return fmt.Sprintf("migration job %s cancelled: %d/%d items cancelled", e.Status.JobID, e.Status.Cancelled, e.Status.Total)
}

func jobResponseError(status int, body []byte, retryAfter string) error {
	var envelope struct {
		Reason  string `json:"reason"`
		Message string `json:"message"`
	}
	_ = json.Unmarshal(body, &envelope)
	detail := envelope.Reason
	if envelope.Message != "" {
		if detail != "" {
			detail += " - "
		}
		detail += envelope.Message
	}
	if detail == "" {
		detail = "request failed"
	}
	message := fmt.Sprintf("%d: %s", status, detail)
	reason := envelope.Reason
	conflict := MigrationConflictError{Message: message, Reason: reason}
	switch reason {
	case "RESOURCE_NOT_FOUND":
		return &NotFoundError{Resource: "migration job", ID: message}
	case "MIGRATION_TEMPLATE_ALIAS_CONFLICT":
		return &TemplateAliasConflictError{conflict}
	case "MIGRATION_RESOURCE_ALREADY_MIGRATING":
		return &ResourceAlreadyMigratingError{conflict}
	case "MIGRATION_PAST_CANCEL_LINE":
		return &CancelTooLateError{conflict}
	case "MIGRATION_SHARED_DEPENDENCY":
		return &SharedDependencyError{conflict}
	case "MIGRATION_CONFLICT":
		return &conflict
	case "MIGRATION_NOT_SUPPORTED":
		return &MigrationNotSupportedError{Message: message}
	case "UNAUTHORIZED":
		return &AuthenticationError{Message: message}
	}
	switch status {
	case 429:
		return fmt.Errorf("%s: %w", message, &RateLimitError{RetryAfter: retryAfter})
	case 401:
		return &AuthenticationError{Message: message}
	case 501:
		return &MigrationNotSupportedError{Message: message}
	case 409:
		if conflict.Reason == "" {
			conflict.Reason = "MIGRATION_CONFLICT"
		}
		return &conflict
	case 400:
		return &InvalidArgumentError{Argument: "migration", Reason: message}
	default:
		return &SandboxError{Message: message, StatusCode: status}
	}
}
