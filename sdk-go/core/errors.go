package core

import "fmt"

// UnsupportedError reports that an API is unavailable on the selected
// control-plane or envd version.
type UnsupportedError struct {
	Operation string
	Reason    string
}

func (e *UnsupportedError) Error() string {
	if e.Reason == "" {
		return fmt.Sprintf("%s is not supported", e.Operation)
	}
	return fmt.Sprintf("%s is not supported: %s", e.Operation, e.Reason)
}

// SandboxError is the base error type for all sandbox-related errors
type SandboxError struct {
	Message    string
	StatusCode int
	Err        error
}

func (e *SandboxError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.Err)
	}
	return e.Message
}

func (e *SandboxError) Unwrap() error {
	return e.Err
}

// SandboxNotFoundError indicates the sandbox was not found
type SandboxNotFoundError struct {
	SandboxID string
}

func (e *SandboxNotFoundError) Error() string {
	return fmt.Sprintf("sandbox not found: %s", e.SandboxID)
}

// FileNotFoundError indicates a file was not found
type FileNotFoundError struct {
	Path string
}

func (e *FileNotFoundError) Error() string {
	return fmt.Sprintf("file not found: %s", e.Path)
}

// TimeoutError indicates an operation timed out
type TimeoutError struct {
	Operation string
	Duration  string
}

func (e *TimeoutError) Error() string {
	return fmt.Sprintf("timeout: %s exceeded %s", e.Operation, e.Duration)
}

// AuthenticationError indicates authentication failed
type AuthenticationError struct {
	Message string
}

func (e *AuthenticationError) Error() string {
	return fmt.Sprintf("authentication error: %s", e.Message)
}

// RateLimitError indicates rate limit was exceeded
type RateLimitError struct {
	RetryAfter string
}

func (e *RateLimitError) Error() string {
	return fmt.Sprintf("rate limit exceeded, retry after: %s", e.RetryAfter)
}

// InvalidArgumentError indicates invalid arguments were provided
type InvalidArgumentError struct {
	Argument string
	Reason   string
}

func (e *InvalidArgumentError) Error() string {
	return fmt.Sprintf("invalid argument %s: %s", e.Argument, e.Reason)
}

// NotEnoughSpaceError indicates insufficient space
type NotEnoughSpaceError struct {
	Required  int64
	Available int64
}

func (e *NotEnoughSpaceError) Error() string {
	return fmt.Sprintf("not enough space: required %d, available %d", e.Required, e.Available)
}

// GitAuthError indicates git authentication failed
type GitAuthError struct {
	Repository string
	Reason     string
}

func (e *GitAuthError) Error() string {
	return fmt.Sprintf("git auth error for %s: %s", e.Repository, e.Reason)
}

// GitUpstreamError indicates a git upstream error
type GitUpstreamError struct {
	Operation string
	Reason    string
}

func (e *GitUpstreamError) Error() string {
	return fmt.Sprintf("git upstream error during %s: %s", e.Operation, e.Reason)
}

// BuildError indicates a template build error
type BuildError struct {
	TemplateID string
	BuildID    string
	Reason     string
}

func (e *BuildError) Error() string {
	return fmt.Sprintf("build error for template %s (build %s): %s", e.TemplateID, e.BuildID, e.Reason)
}

// InvalidImageReferenceError indicates an invalid image reference
type InvalidImageReferenceError struct {
	Reference string
	Reason    string
}

func (e *InvalidImageReferenceError) Error() string {
	return fmt.Sprintf("invalid image reference %s: %s", e.Reference, e.Reason)
}

// RegistryAuthenticationError indicates registry authentication failed
type RegistryAuthenticationError struct {
	Registry string
	Reason   string
}

func (e *RegistryAuthenticationError) Error() string {
	return fmt.Sprintf("registry authentication error for %s: %s", e.Registry, e.Reason)
}

// ImageConfigError indicates an image configuration error
type ImageConfigError struct {
	Image  string
	Reason string
}

func (e *ImageConfigError) Error() string {
	return fmt.Sprintf("image config error for %s: %s", e.Image, e.Reason)
}

// TemplateBuildTimeoutError indicates a template build timed out
type TemplateBuildTimeoutError struct {
	TemplateID string
	BuildID    string
	Duration   string
}

func (e *TemplateBuildTimeoutError) Error() string {
	return fmt.Sprintf("template build timeout for %s (build %s) after %s", e.TemplateID, e.BuildID, e.Duration)
}

// SandboxCreateError indicates sandbox creation failed
type SandboxCreateError struct {
	TemplateID string
	Reason     string
}

func (e *SandboxCreateError) Error() string {
	return fmt.Sprintf("sandbox creation error for template %s: %s", e.TemplateID, e.Reason)
}

// FileUploadError indicates file upload failed
type FileUploadError struct {
	Path   string
	Reason string
}

func (e *FileUploadError) Error() string {
	return fmt.Sprintf("file upload error for %s: %s", e.Path, e.Reason)
}

// SecretError indicates a secret management error
type SecretError struct {
	SecretName string
	Reason     string
}

func (e *SecretError) Error() string {
	return fmt.Sprintf("secret error for %s: %s", e.SecretName, e.Reason)
}

// VolumeError indicates a volume operation error
type VolumeError struct {
	VolumeID string
	Reason   string
}

func (e *VolumeError) Error() string {
	return fmt.Sprintf("volume error for %s: %s", e.VolumeID, e.Reason)
}

// CommandExitError indicates a command exited with non-zero status
type CommandExitError struct {
	Command  string
	ExitCode int
	Stderr   string
}

func (e *CommandExitError) Error() string {
	return fmt.Sprintf("command '%s' exited with code %d: %s", e.Command, e.ExitCode, e.Stderr)
}

// TemplateError indicates a template-related error
type TemplateError struct {
	TemplateID string
	Reason     string
}

func (e *TemplateError) Error() string {
	return fmt.Sprintf("template error for %s: %s", e.TemplateID, e.Reason)
}

// NotFoundError indicates a generic resource was not found
type NotFoundError struct {
	Resource string
	ID       string
}

func (e *NotFoundError) Error() string {
	return fmt.Sprintf("%s not found: %s", e.Resource, e.ID)
}
