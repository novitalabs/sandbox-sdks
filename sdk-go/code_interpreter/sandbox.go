package code_interpreter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/novitalabs/sandbox-sdks/sdk-go/core"
)

// JupyterPort is the port the in-sandbox Jupyter server listens on.
const JupyterPort = 49999

// DefaultTimeout is the default execution timeout.
const DefaultTimeout = 60 * time.Second

// DefaultTemplate is the template the code interpreter uses when none is given.
const DefaultTemplate = "code-interpreter-v1"

// Sandbox runs code inside a sandbox through its Jupyter server.
//
// It embeds the core sandbox, so filesystem, commands and lifecycle APIs are
// available directly.
type Sandbox struct {
	*core.Sandbox

	http *http.Client
}

// CreateSandbox creates a sandbox from the code-interpreter template and
// returns a handle to it.
//
// When opts.Template and opts.Image are both empty, the code-interpreter
// template is used.
func CreateSandbox(ctx context.Context, opts *core.SandboxOpts) (*Sandbox, error) {
	if opts == nil {
		opts = &core.SandboxOpts{}
	} else {
		clone := *opts
		opts = &clone
	}
	if opts.Template == "" && opts.Image == "" {
		opts.Template = DefaultTemplate
	}

	inner, err := core.CreateSandbox(ctx, opts)
	if err != nil {
		return nil, err
	}

	return &Sandbox{Sandbox: inner, http: &http.Client{}}, nil
}

// Connect attaches to an existing code-interpreter sandbox.
func Connect(ctx context.Context, sandboxID string, opts *core.SandboxOpts) (*Sandbox, error) {
	inner, err := core.Connect(ctx, sandboxID, opts)
	if err != nil {
		return nil, err
	}

	return &Sandbox{Sandbox: inner, http: &http.Client{}}, nil
}

// RunCodeOpts are options for executing code.
type RunCodeOpts struct {
	// Context runs the code in an existing context. Mutually exclusive with Language.
	Context *Context
	// Language runs the code in a context for that language, matching the
	// default context when one already exists. Mutually exclusive with Context.
	Language string
	// Envs are environment variables made available to the execution.
	Envs map[string]string

	// OnStdout receives stdout output as it arrives.
	OnStdout func(OutputMessage)
	// OnStderr receives stderr output as it arrives.
	OnStderr func(OutputMessage)
	// OnResult receives each result as it arrives.
	OnResult func(*Result)
	// OnError receives the execution error, if any.
	OnError func(*ExecutionError)

	// Timeout bounds the whole execution. Defaults to 60 seconds.
	// Zero disables the timeout.
	Timeout *time.Duration
	// RequestTimeout bounds the initial request and any HTTP error response body.
	RequestTimeout *time.Duration
}

// CreateCodeContextOpts are options for creating a context.
type CreateCodeContextOpts struct {
	// Cwd is the working directory for the context. Defaults to /home/user.
	Cwd string
	// Language the context executes. Defaults to python.
	Language string
	// RequestTimeout overrides the default request timeout.
	RequestTimeout *time.Duration
}

// CodeContextRequestOpts are options for context management calls.
type CodeContextRequestOpts struct {
	// RequestTimeout overrides the default request timeout.
	RequestTimeout *time.Duration
}

// runCodeRequest is the JSON body for POST /execute.
type runCodeRequest struct {
	Code      string            `json:"code"`
	ContextID string            `json:"context_id,omitempty"`
	Language  string            `json:"language,omitempty"`
	EnvVars   map[string]string `json:"env_vars,omitempty"`
}

// RunCode executes code and returns the resulting execution.
//
// The execution's Error field is populated when the code raised an exception;
// the returned Go error is reserved for transport and protocol failures.
func (s *Sandbox) RunCode(ctx context.Context, code string, opts *RunCodeOpts) (*Execution, error) {
	if opts == nil {
		opts = &RunCodeOpts{}
	}
	if opts.Context != nil && opts.Language != "" {
		return nil, &core.InvalidArgumentError{
			Argument: "Context/Language",
			Reason:   "provide either context or language, not both",
		}
	}

	payload := runCodeRequest{
		Code:    code,
		EnvVars: opts.Envs,
	}
	if opts.Context != nil {
		payload.ContextID = opts.Context.ID
	}
	if opts.Language != "" {
		payload.Language = opts.Language
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	// The request timeout covers connecting and receiving the response
	// headers; the execution timeout then bounds reading the stream.
	ctx = nonNilContext(ctx)
	reqCtx, cancelCause := context.WithCancelCause(ctx)
	cancelReq := func() { cancelCause(context.Canceled) }
	requestTimeout := s.requestTimeout(opts.RequestTimeout)
	stopRequestTimer := startCancelTimer(func() { cancelCause(requestTimeoutError(context.DeadlineExceeded)) }, requestTimeout)
	defer stopRequestTimer()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, s.jupyterURL()+"/execute", bytes.NewReader(body))
	if err != nil {
		stopRequestTimer()
		cancelReq()
		return nil, err
	}
	s.applyHeaders(req)

	res, err := s.http.Do(req)
	if err != nil {
		cause := context.Cause(reqCtx)
		cancelReq()
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if cause != nil {
			return nil, requestTimeoutError(cause)
		}
		return nil, requestTimeoutError(err)
	}
	defer func() {
		res.Body.Close()
		cancelReq()
	}()

	// Successful streams release the header deadline. Error responses keep
	// that deadline until their body has been read as well.
	if res.StatusCode >= 200 && res.StatusCode < 300 {
		stopRequestTimer()
	}
	timeout := opts.Timeout
	if timeout == nil {
		value := DefaultTimeout
		timeout = &value
	}
	stopExecutionTimer := startCancelTimer(func() { cancelCause(executionTimeoutError(context.DeadlineExceeded)) }, timeout)
	defer stopExecutionTimer()
	if err := extractError(res); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if cause := context.Cause(reqCtx); cause != nil {
			return nil, executionTimeoutError(cause)
		}
		return nil, err
	}
	execution := &Execution{}
	readErr := readLines(res.Body, func(line string) error {
		// Scanner may already have buffered more lines when a callback cancels.
		// Check before mutating the execution or dispatching another callback.
		if cause := context.Cause(reqCtx); cause != nil {
			return cause
		}
		return parseOutput(execution, line, opts.OnStdout, opts.OnStderr, opts.OnResult, opts.OnError)
	})
	if ctx.Err() != nil {
		return execution, ctx.Err()
	}
	if cause := context.Cause(reqCtx); cause != nil {
		return execution, executionTimeoutError(cause)
	}
	if readErr != nil {
		return execution, executionTimeoutError(readErr)
	}

	return execution, nil
}

// CreateCodeContext creates a new execution context.
func (s *Sandbox) CreateCodeContext(ctx context.Context, opts *CreateCodeContextOpts) (*Context, error) {
	if opts == nil {
		opts = &CreateCodeContextOpts{}
	}

	payload := map[string]string{}
	if opts.Language != "" {
		payload["language"] = opts.Language
	}
	if opts.Cwd != "" {
		payload["cwd"] = opts.Cwd
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	res, err := s.doContextRequest(ctx, http.MethodPost, "/contexts", body, opts.RequestTimeout)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	if err := extractError(res); err != nil {
		return nil, err
	}

	var executionContext Context
	if err := json.NewDecoder(res.Body).Decode(&executionContext); err != nil {
		return nil, err
	}
	return &executionContext, nil
}

// ListCodeContexts lists the execution contexts in the sandbox.
func (s *Sandbox) ListCodeContexts(ctx context.Context, opts *CodeContextRequestOpts) ([]Context, error) {
	if opts == nil {
		opts = &CodeContextRequestOpts{}
	}

	res, err := s.doContextRequest(ctx, http.MethodGet, "/contexts", nil, opts.RequestTimeout)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	if err := extractError(res); err != nil {
		return nil, err
	}

	// The endpoint has returned both a bare array and an object wrapping a
	// "contexts" field, so accept either shape.
	raw, err := readAllLimited(res.Body, 8<<20)
	if err != nil {
		return nil, err
	}

	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return nil, nil
	}

	if trimmed[0] == '[' {
		var contexts []Context
		if err := json.Unmarshal(trimmed, &contexts); err != nil {
			return nil, err
		}
		return contexts, nil
	}

	var wrapper struct {
		Contexts []Context `json:"contexts"`
	}
	if err := json.Unmarshal(trimmed, &wrapper); err != nil {
		return nil, err
	}
	return wrapper.Contexts, nil
}

// RemoveCodeContext deletes an execution context.
func (s *Sandbox) RemoveCodeContext(ctx context.Context, id string, opts *CodeContextRequestOpts) error {
	if opts == nil {
		opts = &CodeContextRequestOpts{}
	}

	res, err := s.doContextRequest(ctx, http.MethodDelete, "/contexts/"+url.PathEscape(id), nil, opts.RequestTimeout)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	return extractError(res)
}

// RestartCodeContext restarts an execution context, clearing its kernel state.
func (s *Sandbox) RestartCodeContext(ctx context.Context, id string, opts *CodeContextRequestOpts) error {
	if opts == nil {
		opts = &CodeContextRequestOpts{}
	}

	res, err := s.doContextRequest(ctx, http.MethodPost, "/contexts/"+url.PathEscape(id)+"/restart", nil, opts.RequestTimeout)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	return extractError(res)
}

// DeleteCodeContext is an alias for RemoveCodeContext.
func (s *Sandbox) DeleteCodeContext(ctx context.Context, id string, opts *CodeContextRequestOpts) error {
	return s.RemoveCodeContext(ctx, id, opts)
}

// doContextRequest issues a context-management request.
func (s *Sandbox) doContextRequest(
	ctx context.Context,
	method, path string,
	body []byte,
	requestTimeout *time.Duration,
) (*http.Response, error) {
	reqCtx, cancel := withTimeout(ctx, s.requestTimeout(requestTimeout))

	var reader *bytes.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	} else {
		reader = bytes.NewReader(nil)
	}

	req, err := http.NewRequestWithContext(reqCtx, method, s.jupyterURL()+path, reader)
	if err != nil {
		cancel()
		return nil, err
	}
	s.applyHeaders(req)

	res, err := s.http.Do(req)
	if err != nil {
		cancel()
		return nil, requestTimeoutError(err)
	}

	// The caller closes the body; cancellation is released there too via the
	// wrapped reader, so tie the cancel to the response lifetime.
	res.Body = &cancelOnCloseBody{ReadCloser: res.Body, cancel: cancel}
	return res, nil
}

// jupyterURL returns the base URL of the in-sandbox Jupyter server.
func (s *Sandbox) jupyterURL() string {
	return s.PortURL(JupyterPort)
}

// applyHeaders attaches the headers the Jupyter server expects.
func (s *Sandbox) applyHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")

	for k, v := range s.Connection().Headers {
		req.Header.Set(k, v)
	}

	if token := s.AccessToken(); token != "" {
		req.Header.Set("X-Access-Token", token)
	}
	if token := s.TrafficAccessToken(); token != "" {
		req.Header.Set("E2B-Traffic-Access-Token", token)
	}
}

// withTimeout derives a context with the given timeout, or returns the parent
// unchanged when the timeout is nil or non-positive.
func withTimeout(ctx context.Context, timeout *time.Duration) (context.Context, context.CancelFunc) {
	ctx = nonNilContext(ctx)
	if timeout == nil || *timeout <= 0 {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, *timeout)
}

func nonNilContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

// startCancelTimer applies a timeout that can be removed after response
// headers arrive. A context deadline cannot be cleared and would otherwise
// terminate a valid long-running execution stream.
func startCancelTimer(cancel context.CancelFunc, timeout *time.Duration) func() {
	if timeout == nil || *timeout <= 0 {
		return func() {}
	}
	timer := time.AfterFunc(*timeout, cancel)
	return func() { timer.Stop() }
}

// extractError maps a non-2xx Jupyter response onto a core error type.
func extractError(res *http.Response) error {
	if res.StatusCode >= 200 && res.StatusCode < 300 {
		return nil
	}

	body, err := readAllLimited(res.Body, 1<<20)
	if err != nil {
		return err
	}
	message := string(bytes.TrimSpace(body))

	switch res.StatusCode {
	case http.StatusBadGateway:
		return &core.TimeoutError{
			Operation: "sandbox",
			Duration:  message + ": the sandbox may have timed out; pass a longer timeout when creating or connecting to it",
		}
	case http.StatusNotFound:
		return &core.NotFoundError{Resource: "code context", ID: message}
	case http.StatusBadRequest:
		return &core.InvalidArgumentError{Argument: "", Reason: message}
	case http.StatusUnauthorized:
		return &core.AuthenticationError{Message: message}
	default:
		return &core.SandboxError{Message: fmt.Sprintf("%d %s", res.StatusCode, res.Status), StatusCode: res.StatusCode}
	}
}

// requestTimeoutError converts a cancelled request into a TimeoutError,
// leaving other failures untouched.
func requestTimeoutError(err error) error {
	if isTimeout(err) {
		return &core.TimeoutError{
			Operation: "request",
			Duration:  "the request timeout was exceeded; pass RequestTimeout to extend it",
		}
	}
	return err
}

// executionTimeoutError converts a cancelled stream read into a TimeoutError.
func executionTimeoutError(err error) error {
	if isTimeout(err) {
		return &core.TimeoutError{
			Operation: "execution",
			Duration:  "the execution timeout was exceeded; pass Timeout to extend it",
		}
	}
	return err
}

// isTimeout reports whether an error came from a deadline.
func isTimeout(err error) bool {
	if err == nil {
		return false
	}
	if err == context.DeadlineExceeded {
		return true
	}
	type timeout interface{ Timeout() bool }
	if t, ok := err.(timeout); ok {
		return t.Timeout()
	}
	// url.Error and similar wrap the underlying cause.
	type unwrapper interface{ Unwrap() error }
	if u, ok := err.(unwrapper); ok {
		return isTimeout(u.Unwrap())
	}
	return false
}

// readAllLimited reads a body up to a limit, so a misbehaving server cannot
// exhaust memory.
func readAllLimited(r io.Reader, limit int64) ([]byte, error) {
	return io.ReadAll(io.LimitReader(r, limit))
}

func (s *Sandbox) requestTimeout(explicit *time.Duration) *time.Duration {
	if explicit != nil {
		return explicit
	}
	value := s.Connection().GetSignalTimeout(nil)
	return &value
}
