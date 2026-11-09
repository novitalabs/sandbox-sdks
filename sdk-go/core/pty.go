package core

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

// Pty starts a command attached to a pseudo-terminal.
type Pty struct {
	sandbox *Sandbox
}

// newPty wires up the pty module for a sandbox.
func newPty(sandbox *Sandbox) *Pty {
	return &Pty{sandbox: sandbox}
}

// PtyCreateOpts are options for creating a PTY session.
type PtyCreateOpts struct {
	// Cols is the terminal width in columns.
	Cols uint32
	// Rows is the terminal height in rows.
	Rows uint32
	// Cwd is the working directory.
	Cwd string
	// User runs the command as this user.
	User Username
	// Envs are extra environment variables.
	Envs map[string]string
	// OnData receives terminal output as it arrives.
	OnData func(string)
	// TimeoutMs is the total session lifetime in milliseconds.
	TimeoutMs *int
	// RequestTimeoutMs overrides the per-request timeout.
	RequestTimeoutMs *int
}

// PtyConnectOpts are options for attaching to an existing PTY session.
type PtyConnectOpts struct {
	// OnData receives terminal output as it arrives.
	OnData func(string)
	// RequestTimeoutMs overrides the per-request timeout.
	RequestTimeoutMs *int
}

// PtySize is a terminal window size.
type PtySize struct {
	Cols uint32
	Rows uint32
}

// Create starts a new command in a PTY and returns a handle to it.
func (p *Pty) Create(ctx context.Context, cmd string, opts *PtyCreateOpts) (*PtyHandle, error) {
	if opts == nil {
		opts = &PtyCreateOpts{}
	}
	if cmd == "" {
		return nil, &InvalidArgumentError{Argument: "cmd", Reason: "command cannot be empty"}
	}

	cols, rows := opts.Cols, opts.Rows
	if cols == 0 {
		cols = 80
	}
	if rows == 0 {
		rows = 24
	}

	commands := newCommands(p.sandbox)
	user := commands.resolveUser(opts.User)

	request := map[string]interface{}{
		"process": map[string]interface{}{
			"cmd":  "/bin/bash",
			"args": []string{"-l", "-c", cmd},
			"envs": opts.Envs,
			"cwd":  opts.Cwd,
		},
		"pty": map[string]interface{}{
			"size": map[string]interface{}{"cols": cols, "rows": rows},
		},
	}

	handle := newPtyHandle(p.sandbox, cmd, opts.OnData, opts.RequestTimeoutMs)

	if err := handle.start(ctx, user, request, opts.TimeoutMs); err != nil {
		return nil, err
	}
	return handle, nil
}

// Connect attaches to an existing PTY session by PID.
func (p *Pty) Connect(ctx context.Context, pid int, opts *PtyConnectOpts) (*PtyHandle, error) {
	if opts == nil {
		opts = &PtyConnectOpts{}
	}

	handle := newPtyHandle(p.sandbox, "", opts.OnData, opts.RequestTimeoutMs)
	handle.pid = pid

	request := map[string]interface{}{
		"process": map[string]interface{}{"pid": pid},
	}

	commands := newCommands(p.sandbox)
	user := commands.resolveUser(Username{})

	if err := handle.connect(ctx, user, request); err != nil {
		return nil, err
	}
	return handle, nil
}

// PtyHandle represents an attached pseudo-terminal session.
type PtyHandle struct {
	sandbox          *Sandbox
	command          string
	onData           func(string)
	requestTimeoutMs *int

	pid int

	mu           sync.Mutex
	output       strings.Builder
	exitCode     int
	exited       bool
	done         chan struct{}
	started      chan struct{}
	startOnce    sync.Once
	closeOnce    sync.Once
	err          error
	streamCancel context.CancelFunc
}

// newPtyHandle creates a handle with an initialised done channel.
func newPtyHandle(sandbox *Sandbox, command string, onData func(string), requestTimeoutMs *int) *PtyHandle {
	return &PtyHandle{
		sandbox:          sandbox,
		command:          command,
		onData:           onData,
		requestTimeoutMs: requestTimeoutMs,
		done:             make(chan struct{}),
		started:          make(chan struct{}),
	}
}

// PID returns the terminal's process ID.
func (h *PtyHandle) PID() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.pid
}

// start opens a PTY-backed process.start stream.
func (h *PtyHandle) start(ctx context.Context, user string, request map[string]interface{}, timeoutMs *int) error {
	headers := h.headersFor(user)
	if ctx == nil {
		ctx = context.Background()
	}
	streamCtx, cancel := context.WithCancel(ctx)
	if timeoutMs != nil && *timeoutMs > 0 {
		cancel()
		streamCtx, cancel = context.WithTimeout(ctx, msToDuration(*timeoutMs))
	}
	h.streamCancel = cancel

	caller := newServiceCaller(h.sandbox, processService, headers)

	go func() {
		defer h.finish()
		err := caller.stream(streamCtx, "Start", request, h.handleEvent)
		h.completeStream(streamCtx, err)
		cancel()
	}()

	if err := h.waitForStart(ctx); err != nil {
		cancel()
		return err
	}
	return nil
}

// connect opens a process.connect stream for an existing PTY.
func (h *PtyHandle) connect(ctx context.Context, user string, request map[string]interface{}) error {
	headers := h.headersFor(user)
	if ctx == nil {
		ctx = context.Background()
	}
	streamCtx, cancel := context.WithCancel(ctx)
	h.streamCancel = cancel

	caller := newServiceCaller(h.sandbox, processService, headers)

	go func() {
		defer h.finish()
		err := caller.stream(streamCtx, "Connect", request, h.handleEvent)
		h.completeStream(streamCtx, err)
		cancel()
	}()

	// A connect stream may not emit a start event (the PID is known already).
	return nil
}

// headersFor builds the auth headers for a PTY request.
func (h *PtyHandle) headersFor(user string) map[string]string {
	headers := map[string]string{}
	for k, v := range h.sandbox.baseHeaders {
		headers[k] = v
	}
	for k, v := range authenticationHeader(h.sandbox.envdVersion, user) {
		headers[k] = v
	}
	headers[KeepAlivePingHeader] = fmt.Sprint(KeepAlivePingIntervalSec)
	return headers
}

// waitForStart blocks until the PID is known or the stream ends.
func (h *PtyHandle) waitForStart(ctx context.Context) error {
	waitCtx, cancel := h.sandbox.connection.contextWithTimeout(ctx, h.requestTimeoutMs)
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
		return fmt.Errorf("PTY stream ended before the start event")
	case <-waitCtx.Done():
		return waitCtx.Err()
	}
}

// handleEvent consumes one streamed PTY event.
func (h *PtyHandle) handleEvent(payload []byte) error {
	var wrapper struct {
		Event *ptyEvents `json:"event"`
	}
	if err := json.Unmarshal(payload, &wrapper); err != nil {
		return fmt.Errorf("invalid PTY event: %w", err)
	}
	h.applyPtyEvents(wrapper.Event)
	return nil
}

// ptyEvents is the subset of the process event shape PTY cares about.
type ptyEvents struct {
	Start *struct {
		PID uint32 `json:"pid"`
	} `json:"start"`
	Data *struct {
		Pty []byte `json:"pty"`
	} `json:"data"`
	End *struct {
		ExitCode int32  `json:"exitCode"`
		Error    string `json:"error"`
	} `json:"end"`
}

// applyPtyEvents merges a decoded event into the handle.
func (h *PtyHandle) applyPtyEvents(events *ptyEvents) {
	if events == nil {
		return
	}

	if events.Start != nil {
		h.mu.Lock()
		h.pid = int(events.Start.PID)
		h.mu.Unlock()
		h.startOnce.Do(func() { close(h.started) })
	}

	if events.Data != nil && events.Data.Pty != nil {
		out := string(events.Data.Pty)
		h.mu.Lock()
		h.output.WriteString(out)
		h.mu.Unlock()
		if h.onData != nil {
			h.onData(out)
		}
	}

	if events.End != nil {
		h.mu.Lock()
		h.exitCode = int(events.End.ExitCode)
		h.exited = true
		if events.End.Error != "" {
			h.err = fmt.Errorf("PTY failed: %s", events.End.Error)
		}
		cancel := h.streamCancel
		h.mu.Unlock()
		if cancel != nil {
			cancel()
		}
	}
}

// finish closes the done channel exactly once.
func (h *PtyHandle) finish() {
	h.closeOnce.Do(func() { close(h.done) })
}

// setError records a stream error.
func (h *PtyHandle) setError(err error) {
	h.mu.Lock()
	if h.err == nil {
		h.err = err
	}
	h.mu.Unlock()
}

// SendData writes input to the terminal.
func (h *PtyHandle) SendData(ctx context.Context, data string) error {
	pid := h.PID()
	if pid == 0 {
		return &InvalidArgumentError{Argument: "pid", Reason: "pty has not started yet"}
	}

	commands := newCommands(h.sandbox)
	return commands.sendPtyInput(ctx, pid, data, h.requestTimeoutMs)
}

// Resize changes the terminal window size.
func (h *PtyHandle) Resize(ctx context.Context, cols, rows uint32) error {
	pid := h.PID()
	if pid == 0 {
		return &InvalidArgumentError{Argument: "pid", Reason: "pty has not started yet"}
	}

	caller := newServiceCaller(h.sandbox, processService, h.headersFor(""))

	request := map[string]interface{}{
		"process": map[string]interface{}{"pid": pid},
		"pty": map[string]interface{}{
			"size": map[string]interface{}{"cols": cols, "rows": rows},
		},
	}

	ctx, cancel := h.sandbox.connection.contextWithTimeout(ctx, h.requestTimeoutMs)
	defer cancel()

	return caller.call(ctx, "Update", request, nil)
}

// Kill terminates the terminal's process.
func (h *PtyHandle) Kill(ctx context.Context) error {
	pid := h.PID()
	if pid == 0 {
		return &InvalidArgumentError{Argument: "pid", Reason: "pty has not started yet"}
	}

	commands := newCommands(h.sandbox)
	_, err := commands.Kill(ctx, pid, &CommandRequestOpts{RequestTimeoutMs: h.requestTimeoutMs})
	return err
}

// Wait blocks until the terminal exits.
func (h *PtyHandle) Wait(ctx context.Context) (int, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-h.done:
	case <-ctx.Done():
		return 0, ctx.Err()
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	return h.exitCode, h.err
}

// Output returns everything captured so far.
func (h *PtyHandle) Output() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.output.String()
}

func (h *PtyHandle) completeStream(ctx context.Context, err error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.exited {
		return
	}
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	if err == nil {
		err = fmt.Errorf("PTY stream ended without a result")
	}
	if h.err == nil {
		h.err = handleProcessRPCError(err)
	}
}

// Disconnect detaches without killing the remote terminal process.
func (h *PtyHandle) Disconnect() {
	if h.streamCancel != nil {
		h.streamCancel()
	}
}
