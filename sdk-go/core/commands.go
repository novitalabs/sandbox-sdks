package core

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
)

// processService is the Connect service name for envd's process service.
const processService = "process.Process"

// CommandStartOpts are options for starting or running a command.
type CommandStartOpts struct {
	// Background is retained for source compatibility. Start always returns a
	// handle without waiting for exit, and Run always waits.
	Background bool
	// Cwd is the working directory for the command.
	Cwd string
	// User runs the command as this user; this affects file ownership and
	// permission checks.
	User Username
	// Envs are extra environment variables for the command.
	Envs map[string]string
	// OnStdout is called with stdout chunks as they arrive.
	OnStdout func(string)
	// OnStderr is called with stderr chunks as they arrive.
	OnStderr func(string)
	// Stdin enables standard input, allowing SendStdin on the handle.
	//
	// Requires envd >= 0.3.0.
	Stdin bool
	// TimeoutMs is the total time the command may run, in milliseconds.
	// Zero disables the timeout.
	TimeoutMs *int
	// RequestTimeoutMs overrides the per-request timeout.
	RequestTimeoutMs *int
}

// CommandRequestOpts are options for operations that just address the sandbox.
type CommandRequestOpts struct {
	// User runs the operation as this user.
	User Username
	// RequestTimeoutMs overrides the per-request timeout.
	RequestTimeoutMs *int
}

// CommandConnectOpts are options for connecting to an existing process.
type CommandConnectOpts struct {
	CommandRequestOpts
	// OnStdout is called with stdout chunks as they arrive.
	OnStdout func(string)
	// OnStderr is called with stderr chunks as they arrive.
	OnStderr func(string)
}

type Signal int

const (
	SignalUnspecified Signal = 0
	SignalKill        Signal = 9
	SignalTerminate   Signal = 15
)

// Commands runs processes inside a sandbox.
type Commands struct {
	sandbox *Sandbox
}

// newCommands wires up the commands module for a sandbox.
func newCommands(sandbox *Sandbox) *Commands {
	return &Commands{sandbox: sandbox}
}

// headersFor returns the auth headers for a command request.
func (c *Commands) headersFor(user string) map[string]string {
	headers := map[string]string{}
	for k, v := range c.sandbox.baseHeaders {
		headers[k] = v
	}
	for k, v := range authenticationHeader(c.sandbox.envdVersion, user) {
		headers[k] = v
	}
	return headers
}

// resolveUser applies the default user on envd versions older than 0.4.0.
func (c *Commands) resolveUser(user Username) string {
	if user.Name == "" && compareVersions(c.sandbox.envdVersion, EnvdDefaultUser) < 0 {
		return DefaultUsername
	}
	return user.Name
}

// Start launches a command and returns a handle for its streams.
//
// Wait for completion, cancel ctx, or call Disconnect to release the stream.
// Disconnect leaves the remote process running.
func (c *Commands) Start(ctx context.Context, cmd string, opts *CommandStartOpts) (*CommandHandle, error) {
	if opts == nil {
		opts = &CommandStartOpts{}
	}
	if cmd == "" {
		return nil, &InvalidArgumentError{Argument: "cmd", Reason: "command cannot be empty"}
	}

	if opts.Stdin && compareVersions(c.sandbox.envdVersion, EnvdCommandsStdin) < 0 {
		return nil, &TemplateError{
			TemplateID: c.sandbox.sandboxID,
			Reason:     fmt.Sprintf("stdin requires envd >= %s, found %s", EnvdCommandsStdin, c.sandbox.envdVersion),
		}
	}

	user := c.resolveUser(opts.User)

	request := map[string]interface{}{
		"process": map[string]interface{}{
			"cmd":  "/bin/bash",
			"args": []string{"-l", "-c", cmd},
			"envs": opts.Envs,
			"cwd":  opts.Cwd,
		},
	}
	if compareVersions(c.sandbox.envdVersion, EnvdCommandsStdin) >= 0 {
		// Omitting this optional field leaves stdin open, even when disabled.
		request["stdin"] = opts.Stdin
	}

	handle := newCommandHandle(c.sandbox, cmd, opts)

	if err := handle.start(ctx, user, request); err != nil {
		return nil, err
	}
	return handle, nil
}

// Run executes a command and waits for it to finish.
//
// A non-zero exit code yields a *CommandExitError carrying the captured
// stdout and stderr. Streaming callbacks supplied in opts still fire.
func (c *Commands) Run(ctx context.Context, cmd string, opts *CommandStartOpts) (*CommandResult, error) {
	handle, err := c.Start(ctx, cmd, opts)
	if err != nil {
		return nil, err
	}
	return handle.Wait(ctx)
}

// List returns the processes currently running in the sandbox.
func (c *Commands) List(ctx context.Context, opts *CommandRequestOpts) ([]ProcessInfo, error) {
	if opts == nil {
		opts = &CommandRequestOpts{}
	}

	user := c.resolveUser(opts.User)

	ctx, cancel := c.sandbox.connection.contextWithTimeout(ctx, opts.RequestTimeoutMs)
	defer cancel()

	caller := newServiceCaller(c.sandbox, processService, c.headersFor(user))

	var res struct {
		Processes []struct {
			Config *struct {
				Cmd  string   `json:"cmd"`
				Args []string `json:"args"`
			} `json:"config"`
			PID uint32 `json:"pid"`
			Tag string `json:"tag"`
		} `json:"processes"`
	}

	if err := caller.call(ctx, "List", map[string]interface{}{}, &res); err != nil {
		return nil, handleProcessRPCError(err)
	}

	infos := make([]ProcessInfo, 0, len(res.Processes))
	for _, p := range res.Processes {
		info := ProcessInfo{
			PID: int(p.PID),
			Tag: p.Tag,
		}
		if p.Config != nil {
			info.Command = strings.TrimSpace(p.Config.Cmd + " " + strings.Join(p.Config.Args, " "))
		}
		infos = append(infos, info)
	}
	return infos, nil
}

// Kill terminates a process.
//
// It returns true when a process was signalled and false when no matching
// process was running.
func (c *Commands) Kill(ctx context.Context, pid int, opts *CommandRequestOpts) (bool, error) {
	if opts == nil {
		opts = &CommandRequestOpts{}
	}

	user := c.resolveUser(opts.User)

	ctx, cancel := c.sandbox.connection.contextWithTimeout(ctx, opts.RequestTimeoutMs)
	defer cancel()

	caller := newServiceCaller(c.sandbox, processService, c.headersFor(user))

	request := map[string]interface{}{
		"process": map[string]interface{}{"pid": pid},
		"signal":  "SIGNAL_SIGKILL",
	}

	if err := caller.call(ctx, "SendSignal", request, nil); err != nil {
		if rpc, ok := err.(*rpcError); ok && rpc.Code == CodeNotFound {
			return false, nil
		}
		return false, handleProcessRPCError(err)
	}
	return true, nil
}

// Connect attaches to a running process by PID and streams its output.
func (c *Commands) Connect(ctx context.Context, pid int, opts *CommandConnectOpts) (*CommandHandle, error) {
	if opts == nil {
		opts = &CommandConnectOpts{}
	}

	user := c.resolveUser(opts.User)

	handle := newCommandHandle(c.sandbox, "", &CommandStartOpts{
		OnStdout:         opts.OnStdout,
		OnStderr:         opts.OnStderr,
		RequestTimeoutMs: opts.RequestTimeoutMs,
	})
	handle.pid = pid

	request := map[string]interface{}{
		"process": map[string]interface{}{"pid": pid},
	}

	if err := handle.connect(ctx, user, request); err != nil {
		return nil, err
	}
	return handle, nil
}

// CommandHandle represents a running (or attached) command.
type CommandHandle struct {
	sandbox *Sandbox
	command string
	opts    *CommandStartOpts

	pid int

	mu        sync.Mutex
	stdout    strings.Builder
	stderr    strings.Builder
	exitCode  int
	exited    bool
	err       error
	done      chan struct{}
	started   chan struct{}
	startOnce sync.Once
	closeOnce sync.Once

	// streamCancel aborts the underlying stream.
	streamCancel context.CancelFunc
}

// newCommandHandle creates a handle with an initialised done channel.
func newCommandHandle(sandbox *Sandbox, command string, opts *CommandStartOpts) *CommandHandle {
	return &CommandHandle{
		sandbox: sandbox,
		command: command,
		opts:    opts,
		done:    make(chan struct{}),
		started: make(chan struct{}),
	}
}

// PID returns the process ID once the start event has been received.
func (h *CommandHandle) PID() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.pid
}

// start opens the process.start stream and consumes events in the background.
func (h *CommandHandle) start(ctx context.Context, user string, request map[string]interface{}) error {
	headers := map[string]string{}
	for k, v := range h.sandbox.baseHeaders {
		headers[k] = v
	}
	for k, v := range authenticationHeader(h.sandbox.envdVersion, user) {
		headers[k] = v
	}
	headers[KeepAlivePingHeader] = fmt.Sprint(KeepAlivePingIntervalSec)

	streamCtx, cancel := h.streamContext(ctx)
	h.streamCancel = cancel

	caller := newServiceCaller(h.sandbox, processService, headers)

	go func() {
		defer h.finish()

		err := caller.stream(streamCtx, "Start", request, h.handleEvent)
		h.completeStream(streamCtx, err)
		cancel()
	}()

	// Wait for the start event so the PID is available when Start returns.
	if err := h.waitForStart(ctx); err != nil {
		cancel()
		return err
	}
	return nil
}

// connect opens the process.connect stream for an existing process.
func (h *CommandHandle) connect(ctx context.Context, user string, request map[string]interface{}) error {
	headers := map[string]string{}
	for k, v := range h.sandbox.baseHeaders {
		headers[k] = v
	}
	for k, v := range authenticationHeader(h.sandbox.envdVersion, user) {
		headers[k] = v
	}
	headers[KeepAlivePingHeader] = fmt.Sprint(KeepAlivePingIntervalSec)

	streamCtx, cancel := h.streamContext(ctx)
	h.streamCancel = cancel

	caller := newServiceCaller(h.sandbox, processService, headers)

	go func() {
		defer h.finish()

		err := caller.stream(streamCtx, "Connect", request, h.handleEvent)
		h.completeStream(streamCtx, err)
		cancel()
	}()

	if err := h.waitForStart(ctx); err != nil {
		cancel()
		return err
	}
	return nil
}

// streamContext applies the command timeout (when set) on top of the caller's
// context.
func (h *CommandHandle) streamContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	if h.opts != nil && h.opts.TimeoutMs != nil && *h.opts.TimeoutMs > 0 {
		return context.WithTimeout(ctx, msToDuration(*h.opts.TimeoutMs))
	}
	return context.WithCancel(ctx)
}

// waitForStart blocks until the start event arrives, the stream ends, or the
// context is cancelled.
func (h *CommandHandle) waitForStart(ctx context.Context) error {
	waitCtx, cancel := h.sandbox.connection.contextWithTimeout(ctx, h.opts.RequestTimeoutMs)
	defer cancel()

	select {
	case <-h.started:
		return nil
	case <-h.done:
		h.mu.Lock()
		defer h.mu.Unlock()
		if h.err != nil {
			return h.err
		}
		if h.pid != 0 {
			return nil
		}
		return fmt.Errorf("process stream ended before the start event")
	case <-waitCtx.Done():
		return waitCtx.Err()
	}
}

// processEvent mirrors the process.ProcessEvent oneof.
type processEvent struct {
	Event *struct {
		Start *struct {
			PID uint32 `json:"pid"`
		} `json:"start"`
		Data *struct {
			Stdout []byte `json:"stdout"`
			Stderr []byte `json:"stderr"`
			Pty    []byte `json:"pty"`
		} `json:"data"`
		End *struct {
			ExitCode int32  `json:"exitCode"`
			Exited   bool   `json:"exited"`
			Status   string `json:"status"`
			Error    string `json:"error"`
		} `json:"end"`
		Keepalive *struct{} `json:"keepalive"`
	} `json:"event"`
}

// handleEvent consumes one streamed process event.
func (h *CommandHandle) handleEvent(payload []byte) error {
	var wrapper struct {
		Event *struct {
			Start *struct {
				PID uint32 `json:"pid"`
			} `json:"start"`
			Data *struct {
				Stdout []byte `json:"stdout"`
				Stderr []byte `json:"stderr"`
				Pty    []byte `json:"pty"`
			} `json:"data"`
			End *struct {
				ExitCode int32  `json:"exitCode"`
				Exited   bool   `json:"exited"`
				Status   string `json:"status"`
				Error    string `json:"error"`
			} `json:"end"`
		} `json:"event"`
	}
	if err := json.Unmarshal(payload, &wrapper); err != nil {
		return fmt.Errorf("invalid process event: %w", err)
	}
	if wrapper.Event == nil {
		return nil
	}

	if e := wrapper.Event.Start; e != nil {
		h.mu.Lock()
		h.pid = int(e.PID)
		h.mu.Unlock()
		h.startOnce.Do(func() { close(h.started) })
		return nil
	}

	if e := wrapper.Event.Data; e != nil {
		if len(e.Stdout) > 0 {
			out := string(e.Stdout)
			h.mu.Lock()
			h.stdout.WriteString(out)
			h.mu.Unlock()
			if h.opts != nil && h.opts.OnStdout != nil {
				h.opts.OnStdout(out)
			}
		}
		if len(e.Stderr) > 0 {
			errOut := string(e.Stderr)
			h.mu.Lock()
			h.stderr.WriteString(errOut)
			h.mu.Unlock()
			if h.opts != nil && h.opts.OnStderr != nil {
				h.opts.OnStderr(errOut)
			}
		}
		if len(e.Pty) > 0 {
			out := string(e.Pty)
			h.mu.Lock()
			h.stdout.WriteString(out)
			h.mu.Unlock()
			if h.opts != nil && h.opts.OnStdout != nil {
				h.opts.OnStdout(out)
			}
		}
		return nil
	}

	if e := wrapper.Event.End; e != nil {
		h.mu.Lock()
		h.exitCode = int(e.ExitCode)
		h.exited = true
		// A failed exit can also carry a diagnostic message. Let Wait return
		// CommandExitError for that result instead of treating it as a stream error.
		if e.Error != "" && e.ExitCode == 0 {
			h.err = fmt.Errorf("process failed: %s", e.Error)
		}
		cancel := h.streamCancel
		h.mu.Unlock()
		// Some envd versions keep the Connect stream alive after sending the
		// terminal End event. Stop it here so Wait is released deterministically.
		if cancel != nil {
			cancel()
		}
		return nil
	}

	return nil
}

// finish closes the done channel exactly once.
func (h *CommandHandle) finish() {
	h.closeOnce.Do(func() { close(h.done) })
}

// setError records a stream error.
func (h *CommandHandle) setError(err error) {
	h.mu.Lock()
	if h.err == nil {
		h.err = err
	}
	h.mu.Unlock()
}

// Wait blocks until the command exits and returns its result.
func (h *CommandHandle) Wait(ctx context.Context) (*CommandResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-h.done:
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	result := &CommandResult{
		Stdout:   h.stdout.String(),
		Stderr:   h.stderr.String(),
		ExitCode: h.exitCode,
	}

	if h.err != nil {
		return result, handleProcessRPCError(h.err)
	}
	if result.ExitCode != 0 {
		return result, &CommandExitError{
			Command:  h.command,
			ExitCode: result.ExitCode,
			Stderr:   result.Stderr,
		}
	}
	return result, nil
}

// Kill terminates the running command's process.
func (h *CommandHandle) Kill(ctx context.Context) error {
	pid := h.PID()
	if pid == 0 {
		return &InvalidArgumentError{Argument: "pid", Reason: "process has not started yet"}
	}

	commands := newCommands(h.sandbox)
	_, err := commands.Kill(ctx, pid, &CommandRequestOpts{RequestTimeoutMs: h.opts.RequestTimeoutMs})
	return err
}

// SendStdin writes data to the command's standard input.
//
// The command must have been started with Stdin enabled.
func (h *CommandHandle) SendStdin(ctx context.Context, data string) error {
	pid := h.PID()
	if pid == 0 {
		return &InvalidArgumentError{Argument: "pid", Reason: "process has not started yet"}
	}

	commands := newCommands(h.sandbox)
	return commands.sendInput(ctx, pid, data, false, h.opts.RequestTimeoutMs)
}

// CloseStdin closes the command's standard input.
func (h *CommandHandle) CloseStdin(ctx context.Context) error {
	pid := h.PID()
	if pid == 0 {
		return &InvalidArgumentError{Argument: "pid", Reason: "process has not started yet"}
	}

	commands := newCommands(h.sandbox)
	return commands.closeStdin(ctx, pid, h.opts.RequestTimeoutMs)
}

// sendInput writes to a process's stdin; when close is true it signals EOF.
func (c *Commands) sendInput(ctx context.Context, pid int, data string, _ bool, requestTimeoutMs *int) error {
	ctx, cancel := c.sandbox.connection.contextWithTimeout(ctx, requestTimeoutMs)
	defer cancel()

	caller := newServiceCaller(c.sandbox, processService, c.headersFor(c.resolveUser(Username{})))

	request := map[string]interface{}{
		"process": map[string]interface{}{"pid": pid},
		"input":   map[string]interface{}{"stdin": []byte(data)},
	}

	return caller.call(ctx, "SendInput", request, nil)
}

func (c *Commands) closeStdin(ctx context.Context, pid int, requestTimeoutMs *int) error {
	if compareVersions(c.sandbox.envdVersion, EnvdEnvdClose) < 0 {
		return &UnsupportedError{
			Operation: "Commands.CloseStdin",
			Reason:    fmt.Sprintf("requires envd >= %s, found %s", EnvdEnvdClose, c.sandbox.envdVersion),
		}
	}
	ctx, cancel := c.sandbox.connection.contextWithTimeout(ctx, requestTimeoutMs)
	defer cancel()
	caller := newServiceCaller(c.sandbox, processService, c.headersFor(c.resolveUser(Username{})))
	return handleProcessRPCError(caller.call(ctx, "CloseStdin", map[string]interface{}{
		"process": map[string]interface{}{"pid": pid},
	}, nil))
}

// sendPtyInput writes input to a PTY-backed process.
func (c *Commands) sendPtyInput(ctx context.Context, pid int, data string, requestTimeoutMs *int) error {
	ctx, cancel := c.sandbox.connection.contextWithTimeout(ctx, requestTimeoutMs)
	defer cancel()

	caller := newServiceCaller(c.sandbox, processService, c.headersFor(c.resolveUser(Username{})))

	request := map[string]interface{}{
		"process": map[string]interface{}{"pid": pid},
		"input":   map[string]interface{}{"pty": []byte(data)},
	}

	return caller.call(ctx, "SendInput", request, nil)
}

// SendSignal sends an operating-system signal to a process.
func (c *Commands) SendSignal(ctx context.Context, pid int, signal Signal, opts *CommandRequestOpts) error {
	if opts == nil {
		opts = &CommandRequestOpts{}
	}
	ctx, cancel := c.sandbox.connection.contextWithTimeout(ctx, opts.RequestTimeoutMs)
	defer cancel()
	caller := newServiceCaller(c.sandbox, processService, c.headersFor(c.resolveUser(opts.User)))
	return handleProcessRPCError(caller.call(ctx, "SendSignal", map[string]interface{}{"process": map[string]interface{}{"pid": pid}, "signal": signal}, nil))
}

// Signal sends a signal to this process.
func (h *CommandHandle) Signal(ctx context.Context, signal Signal) error {
	return newCommands(h.sandbox).SendSignal(ctx, h.PID(), signal, &CommandRequestOpts{RequestTimeoutMs: h.opts.RequestTimeoutMs})
}

// handleProcessRPCError maps process-service errors onto SDK error types.
func handleProcessRPCError(err error) error {
	rpc, ok := err.(*rpcError)
	if !ok {
		return err
	}

	switch rpc.Code {
	case CodeNotFound:
		return &NotFoundError{Resource: "process", ID: rpc.Message}
	case CodeInvalidArgument:
		return &InvalidArgumentError{Argument: "", Reason: rpc.Message}
	case CodeUnauthenticated:
		return &AuthenticationError{Message: rpc.Message}
	case CodeUnavailable:
		return &SandboxNotFoundError{SandboxID: ""}
	case CodeCanceled:
		return &TimeoutError{Operation: "request", Duration: "unknown"}
	case CodeDeadlineExceeded:
		return &TimeoutError{Operation: "command", Duration: "unknown"}
	default:
		return &SandboxError{Message: fmt.Sprintf("%s: %s", rpc.Code, rpc.Message)}
	}
}

// ensure http stays referenced for future streaming helpers.
var _ = http.MethodPost

// completeStream keeps transport failures distinct from a reported process exit.
func (h *CommandHandle) completeStream(ctx context.Context, err error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.exited {
		return
	}
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	if err == nil {
		err = fmt.Errorf("process stream ended without a result")
	}
	if h.err == nil {
		h.err = handleProcessRPCError(err)
	}
}

// Disconnect stops receiving output without killing the remote process.
// Wait subsequently returns context.Canceled if no terminal result arrived.
func (h *CommandHandle) Disconnect() {
	if h.streamCancel != nil {
		h.streamCancel()
	}
}

// Stdout returns the output captured so far.
func (h *CommandHandle) Stdout() string { h.mu.Lock(); defer h.mu.Unlock(); return h.stdout.String() }

// Stderr returns the error output captured so far.
func (h *CommandHandle) Stderr() string { h.mu.Lock(); defer h.mu.Unlock(); return h.stderr.String() }
