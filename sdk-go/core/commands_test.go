package core

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// readStreamRequest decodes the JSON payload from a Connect streaming request.
func readStreamRequest(t *testing.T, r *http.Request) map[string]interface{} {
	t.Helper()

	raw, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatalf("failed to read body: %v", err)
	}
	if len(raw) < 5 {
		t.Fatalf("streaming request body too short: %d bytes", len(raw))
	}

	length := binary.BigEndian.Uint32(raw[1:5])
	if int(length) > len(raw)-5 {
		t.Fatalf("frame length %d exceeds body %d", length, len(raw))
	}

	var decoded map[string]interface{}
	if err := json.Unmarshal(raw[5:5+length], &decoded); err != nil {
		t.Fatalf("failed to decode streaming request: %v", err)
	}
	return decoded
}

// writeStreamFrames writes Connect streaming frames and flushes them.
func writeStreamFrames(t *testing.T, w http.ResponseWriter, payloads ...interface{}) {
	t.Helper()

	w.Header().Set("Content-Type", "application/connect+json")
	w.WriteHeader(http.StatusOK)

	flusher, _ := w.(http.Flusher)

	for _, payload := range payloads {
		body, err := json.Marshal(payload)
		if err != nil {
			t.Errorf("failed to encode frame: %v", err)
			return
		}
		if _, err := w.Write(envelopeFrame(body)); err != nil {
			t.Errorf("failed to write frame: %v", err)
			return
		}
		if flusher != nil {
			flusher.Flush()
		}
	}
}

func TestCommandsRunCapturesOutput(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/process.Process/Start") {
			t.Errorf("path = %q, want the process Start RPC", r.URL.Path)
		}

		writeStreamFrames(t, w,
			map[string]interface{}{
				"event": map[string]interface{}{"start": map[string]interface{}{"pid": 42}},
			},
			map[string]interface{}{
				"event": map[string]interface{}{
					"data": map[string]interface{}{"stdout": []byte("hello ")},
				},
			},
			map[string]interface{}{
				"event": map[string]interface{}{
					"data": map[string]interface{}{"stdout": []byte("world")},
				},
			},
			map[string]interface{}{
				"event": map[string]interface{}{
					"data": map[string]interface{}{"stderr": []byte("warn")},
				},
			},
			map[string]interface{}{
				"event": map[string]interface{}{
					"end": map[string]interface{}{"exitCode": 0, "exited": true},
				},
			},
		)
	})

	sandbox := newTestSandbox(t, "0.5.7", handler)

	result, err := sandbox.Commands.Run(context.Background(), "echo hello", nil)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	if result.Stdout != "hello world" {
		t.Errorf("stdout = %q, want %q", result.Stdout, "hello world")
	}
	if result.Stderr != "warn" {
		t.Errorf("stderr = %q, want warn", result.Stderr)
	}
	if result.ExitCode != 0 {
		t.Errorf("exitCode = %d, want 0", result.ExitCode)
	}
}

func TestCommandsRunNonZeroExitCode(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeStreamFrames(t, w,
			map[string]interface{}{
				"event": map[string]interface{}{"start": map[string]interface{}{"pid": 7}},
			},
			map[string]interface{}{
				"event": map[string]interface{}{
					"data": map[string]interface{}{"stderr": []byte("boom")},
				},
			},
			map[string]interface{}{
				"event": map[string]interface{}{
					"end": map[string]interface{}{"exitCode": 1, "exited": true},
				},
			},
		)
	})

	sandbox := newTestSandbox(t, "0.5.7", handler)

	result, err := sandbox.Commands.Run(context.Background(), "false", nil)
	if err == nil {
		t.Fatal("expected a CommandExitError")
	}

	exitErr, ok := err.(*CommandExitError)
	if !ok {
		t.Fatalf("error = %T, want *CommandExitError", err)
	}
	if exitErr.ExitCode != 1 {
		t.Errorf("ExitCode = %d, want 1", exitErr.ExitCode)
	}
	if exitErr.Stderr != "boom" {
		t.Errorf("Stderr = %q, want boom", exitErr.Stderr)
	}
	// The result is still returned alongside the error.
	if result == nil || result.Stderr != "boom" {
		t.Errorf("result = %+v, want captured stderr", result)
	}
}

func TestCommandsNonZeroExitWithErrorMessage(t *testing.T) {
	for _, code := range []int{2, 3, 124} {
		for _, wait := range []bool{false, true} {
			t.Run(fmt.Sprintf("exit_%d/wait_%t", code, wait), func(t *testing.T) {
				sandbox := newTestSandbox(t, "0.5.7", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					writeStreamFrames(t, w,
						map[string]any{"event": map[string]any{"start": map[string]any{"pid": 7}}},
						map[string]any{"event": map[string]any{"data": map[string]any{"stdout": []byte("output"), "stderr": []byte("denied")}}},
						map[string]any{"event": map[string]any{"end": map[string]any{"exitCode": code, "exited": true, "error": fmt.Sprintf("exit status %d", code)}}},
					)
				}))
				var result *CommandResult
				var err error
				if wait {
					var handle *CommandHandle
					handle, err = sandbox.Commands.Start(context.Background(), "synthetic-command", nil)
					if err != nil {
						t.Fatalf("Start failed: %v", err)
					}
					result, err = handle.Wait(context.Background())
				} else {
					result, err = sandbox.Commands.Run(context.Background(), "synthetic-command", nil)
				}
				var exitErr *CommandExitError
				if !errors.As(err, &exitErr) {
					t.Fatalf("error = %T %v, want *CommandExitError", err, err)
				}
				if exitErr.ExitCode != code || exitErr.Command != "synthetic-command" || exitErr.Stderr != "denied" {
					t.Errorf("exit error = %+v", exitErr)
				}
				if result == nil || result.ExitCode != code || result.Stdout != "output" || result.Stderr != "denied" {
					t.Errorf("result = %+v, want captured output and exit code %d", result, code)
				}
			})
		}
	}
}

func TestCommandsEndErrorWithoutNonZeroExit(t *testing.T) {
	sandbox := newTestSandbox(t, "0.5.7", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeStreamFrames(t, w,
			map[string]any{"event": map[string]any{"start": map[string]any{"pid": 7}}},
			map[string]any{"event": map[string]any{"end": map[string]any{"exitCode": 0, "error": "process execution failed"}}},
		)
	}))
	_, err := sandbox.Commands.Run(context.Background(), "synthetic-command", nil)
	var exitErr *CommandExitError
	if err == nil || errors.As(err, &exitErr) || !strings.Contains(err.Error(), "process execution failed") {
		t.Fatalf("error = %T %v, want the process error", err, err)
	}
}

func TestCommandsRunInvokesCallbacks(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeStreamFrames(t, w,
			map[string]interface{}{
				"event": map[string]interface{}{"start": map[string]interface{}{"pid": 1}},
			},
			map[string]interface{}{
				"event": map[string]interface{}{
					"data": map[string]interface{}{"stdout": []byte("a")},
				},
			},
			map[string]interface{}{
				"event": map[string]interface{}{
					"data": map[string]interface{}{"stderr": []byte("b")},
				},
			},
			map[string]interface{}{
				"event": map[string]interface{}{
					"end": map[string]interface{}{"exitCode": 0, "exited": true},
				},
			},
		)
	})

	sandbox := newTestSandbox(t, "0.5.7", handler)

	var stdout, stderr strings.Builder
	_, err := sandbox.Commands.Run(context.Background(), "cmd", &CommandStartOpts{
		OnStdout: func(s string) { stdout.WriteString(s) },
		OnStderr: func(s string) { stderr.WriteString(s) },
	})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	if stdout.String() != "a" {
		t.Errorf("OnStdout collected %q, want a", stdout.String())
	}
	if stderr.String() != "b" {
		t.Errorf("OnStderr collected %q, want b", stderr.String())
	}
}

func TestCommandsRunSendsProcessConfig(t *testing.T) {
	var gotBody map[string]interface{}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody = readStreamRequest(t, r)
		writeStreamFrames(t, w,
			map[string]interface{}{
				"event": map[string]interface{}{"start": map[string]interface{}{"pid": 1}},
			},
			map[string]interface{}{
				"event": map[string]interface{}{
					"end": map[string]interface{}{"exitCode": 0, "exited": true},
				},
			},
		)
	})

	sandbox := newTestSandbox(t, "0.5.7", handler)

	_, err := sandbox.Commands.Run(context.Background(), "ls -la", &CommandStartOpts{
		Cwd:  "/workspace",
		Envs: map[string]string{"FOO": "bar"},
	})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	process, ok := gotBody["process"].(map[string]interface{})
	if !ok {
		t.Fatalf("process payload missing: %v", gotBody)
	}
	if process["cmd"] != "/bin/bash" {
		t.Errorf("cmd = %v, want /bin/bash", process["cmd"])
	}
	args, _ := process["args"].([]interface{})
	if len(args) != 3 || args[0] != "-l" || args[1] != "-c" || args[2] != "ls -la" {
		t.Errorf("args = %v, want [-l -c 'ls -la']", args)
	}
	if process["cwd"] != "/workspace" {
		t.Errorf("cwd = %v, want /workspace", process["cwd"])
	}
}

func TestCommandsRunRejectsEmptyCommand(t *testing.T) {
	sandbox := newTestSandbox(t, "0.5.7", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

	_, err := sandbox.Commands.Run(context.Background(), "", nil)
	if err == nil {
		t.Fatal("expected an error for an empty command")
	}
	if _, ok := err.(*InvalidArgumentError); !ok {
		t.Errorf("error = %T, want *InvalidArgumentError", err)
	}
}

func TestCommandsStdinRequiresNewEnvd(t *testing.T) {
	sandbox := newTestSandbox(t, "0.2.0", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

	_, err := sandbox.Commands.Run(context.Background(), "cat", &CommandStartOpts{Stdin: true})
	if err == nil {
		t.Fatal("expected an error for stdin on an old envd")
	}
	if _, ok := err.(*TemplateError); !ok {
		t.Errorf("error = %T, want *TemplateError", err)
	}
}

func TestCommandsList(t *testing.T) {
	handler := newRPCServer(t, map[string]func(w http.ResponseWriter, r *http.Request){
		"List": func(w http.ResponseWriter, r *http.Request) {
			writeJSON(t, w, map[string]interface{}{
				"processes": []map[string]interface{}{
					{
						"config": map[string]interface{}{
							"cmd":  "/bin/bash",
							"args": []string{"-l", "-c", "sleep 100"},
						},
						"pid": 99,
						"tag": "worker",
					},
				},
			})
		},
	})

	sandbox := newTestSandbox(t, "0.5.7", handler)

	infos, err := sandbox.Commands.List(context.Background(), nil)
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}

	if len(infos) != 1 {
		t.Fatalf("got %d processes, want 1", len(infos))
	}
	if infos[0].PID != 99 {
		t.Errorf("PID = %d, want 99", infos[0].PID)
	}
	if infos[0].Tag != "worker" {
		t.Errorf("Tag = %q, want worker", infos[0].Tag)
	}
	if !strings.Contains(infos[0].Command, "sleep 100") {
		t.Errorf("Command = %q, want it to contain 'sleep 100'", infos[0].Command)
	}
}

func TestCommandsKill(t *testing.T) {
	var gotBody map[string]interface{}

	handler := newRPCServer(t, map[string]func(w http.ResponseWriter, r *http.Request){
		"SendSignal": func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewDecoder(r.Body).Decode(&gotBody)
			writeJSON(t, w, map[string]interface{}{})
		},
	})

	sandbox := newTestSandbox(t, "0.5.7", handler)

	killed, err := sandbox.Commands.Kill(context.Background(), 55, nil)
	if err != nil {
		t.Fatalf("Kill failed: %v", err)
	}
	if !killed {
		t.Error("expected killed=true")
	}

	process, _ := gotBody["process"].(map[string]interface{})
	if process["pid"] != float64(55) {
		t.Errorf("pid = %v, want 55", process["pid"])
	}
	if gotBody["signal"] != "SIGNAL_SIGKILL" {
		t.Errorf("signal = %v, want SIGNAL_SIGKILL", gotBody["signal"])
	}
}

func TestCommandsKillMissingProcess(t *testing.T) {
	handler := newRPCServer(t, map[string]func(w http.ResponseWriter, r *http.Request){
		"SendSignal": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
			writeJSON(t, w, map[string]string{"code": CodeNotFound, "message": "no such process"})
		},
	})

	sandbox := newTestSandbox(t, "0.5.7", handler)

	killed, err := sandbox.Commands.Kill(context.Background(), 1, nil)
	if err != nil {
		t.Fatalf("Kill failed: %v", err)
	}
	if killed {
		t.Error("expected killed=false for a missing process")
	}
}

func TestCommandsErrorMapping(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"code": CodeNotFound, "message": "gone"})
	})

	sandbox := newTestSandbox(t, "0.5.7", handler)

	_, err := sandbox.Commands.Run(context.Background(), "x", nil)
	if err == nil {
		t.Fatal("expected an error")
	}
	if _, ok := err.(*NotFoundError); !ok {
		t.Errorf("error = %T, want *NotFoundError", err)
	}
}

func TestCommandHandleKillAndSendStdin(t *testing.T) {
	var signalSent, inputSent, stdinClosed bool

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/Start"):
			writeStreamFrames(t, w,
				map[string]interface{}{
					"event": map[string]interface{}{"start": map[string]interface{}{"pid": 5}},
				},
			)
			// Keep the stream open long enough for the handle to be usable.
			select {
			case <-r.Context().Done():
			case <-time.After(2 * time.Second):
			}
		case strings.HasSuffix(r.URL.Path, "/SendSignal"):
			signalSent = true
			writeJSON(t, w, map[string]interface{}{})
		case strings.HasSuffix(r.URL.Path, "/SendInput"):
			inputSent = true
			writeJSON(t, w, map[string]interface{}{})
		case strings.HasSuffix(r.URL.Path, "/CloseStdin"):
			stdinClosed = true
			writeJSON(t, w, map[string]interface{}{})
		default:
			http.NotFound(w, r)
		}
	})

	sandbox := newTestSandbox(t, "0.5.7", handler)

	handle, err := sandbox.Commands.Start(context.Background(), "cat", &CommandStartOpts{Stdin: true})
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	if handle.PID() != 5 {
		t.Errorf("PID = %d, want 5", handle.PID())
	}

	if err := handle.SendStdin(context.Background(), "hello\n"); err != nil {
		t.Fatalf("SendStdin failed: %v", err)
	}
	if !inputSent {
		t.Error("expected SendInput to be called")
	}
	if err := handle.CloseStdin(context.Background()); err != nil {
		t.Fatalf("CloseStdin failed: %v", err)
	}
	if !stdinClosed {
		t.Error("expected CloseStdin to be called")
	}

	if err := handle.Kill(context.Background()); err != nil {
		t.Fatalf("Kill failed: %v", err)
	}
	if !signalSent {
		t.Error("expected SendSignal to be called")
	}
}

func TestCommandsStartReturnsAfterStartFrameWhileStreamRemainsOpen(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get(KeepAlivePingHeader); got != "50" {
			t.Errorf("%s = %q, want 50", KeepAlivePingHeader, got)
		}
		writeStreamFrames(t, w, map[string]interface{}{
			"event": map[string]interface{}{"start": map[string]interface{}{"pid": 17}},
		})
		<-r.Context().Done()
	})

	sandbox := newTestSandbox(t, "0.5.7", handler)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	startedAt := time.Now()
	handle, err := sandbox.Commands.Start(ctx, "cat", &CommandStartOpts{Stdin: true})
	if err != nil {
		t.Fatalf("Start failed before the process stream ended: %v", err)
	}
	if elapsed := time.Since(startedAt); elapsed > 500*time.Millisecond {
		t.Fatalf("Start waited %s after receiving the start frame", elapsed)
	}
	if handle.PID() != 17 {
		t.Fatalf("PID = %d, want 17", handle.PID())
	}
	cancel()
}

func TestCloseStdinRequiresSupportedEnvd(t *testing.T) {
	sandbox := newTestSandbox(t, "0.5.1", http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	commands := newCommands(sandbox)
	if err := commands.closeStdin(context.Background(), 1, nil); err == nil {
		t.Fatal("expected unsupported error")
	} else if _, ok := err.(*UnsupportedError); !ok {
		t.Fatalf("error = %T", err)
	}
}

func TestCommandEndEventCancelsStream(t *testing.T) {
	handle := newCommandHandle(nil, "true", &CommandStartOpts{})
	cancelled := make(chan struct{})
	handle.streamCancel = func() { close(cancelled) }
	if err := handle.handleEvent([]byte(`{"event":{"end":{"exitCode":0,"exited":true}}}`)); err != nil {
		t.Fatal(err)
	}
	select {
	case <-cancelled:
	default:
		t.Fatal("end event did not cancel the command stream")
	}
}

func TestCommandHandleKillBeforeStart(t *testing.T) {
	sandbox := newTestSandbox(t, "0.5.7", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

	handle := newCommandHandle(sandbox, "x", &CommandStartOpts{})

	if err := handle.Kill(context.Background()); err == nil {
		t.Fatal("expected an error when killing a handle that never started")
	}
	if err := handle.SendStdin(context.Background(), "x"); err == nil {
		t.Fatal("expected an error when writing to a handle that never started")
	}
}

func TestPtyCreateAndResize(t *testing.T) {
	var gotBody map[string]interface{}
	var resizeBody map[string]interface{}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/Start"):
			gotBody = readStreamRequest(t, r)
			writeStreamFrames(t, w,
				map[string]interface{}{
					"event": map[string]interface{}{"start": map[string]interface{}{"pid": 11}},
				},
				map[string]interface{}{
					"event": map[string]interface{}{
						"data": map[string]interface{}{"pty": []byte("prompt$ ")},
					},
				},
			)
		case strings.HasSuffix(r.URL.Path, "/Update"):
			_ = json.NewDecoder(r.Body).Decode(&resizeBody)
			writeJSON(t, w, map[string]interface{}{})
		default:
			http.NotFound(w, r)
		}
	})

	sandbox := newTestSandbox(t, "0.5.7", handler)

	handle, err := sandbox.Pty.Create(context.Background(), "bash", &PtyCreateOpts{
		Cols: 120,
		Rows: 40,
	})
	if err != nil {
		t.Fatalf("Pty.Create failed: %v", err)
	}

	if handle.PID() != 11 {
		t.Errorf("PID = %d, want 11", handle.PID())
	}

	pty, ok := gotBody["pty"].(map[string]interface{})
	if !ok {
		t.Fatalf("pty payload missing: %v", gotBody)
	}
	size, _ := pty["size"].(map[string]interface{})
	if size["cols"] != float64(120) || size["rows"] != float64(40) {
		t.Errorf("size = %v, want 120x40", size)
	}

	if err := handle.Resize(context.Background(), 80, 24); err != nil {
		t.Fatalf("Resize failed: %v", err)
	}
	resizePty, _ := resizeBody["pty"].(map[string]interface{})
	resizeSize, _ := resizePty["size"].(map[string]interface{})
	if resizeSize["cols"] != float64(80) || resizeSize["rows"] != float64(24) {
		t.Errorf("resize size = %v, want 80x24", resizeSize)
	}
}

func TestPtyCreateReturnsAfterStartFrameWhileStreamRemainsOpen(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get(KeepAlivePingHeader); got != "50" {
			t.Errorf("%s = %q, want 50", KeepAlivePingHeader, got)
		}
		writeStreamFrames(t, w, map[string]interface{}{
			"event": map[string]interface{}{"start": map[string]interface{}{"pid": 23}},
		})
		<-r.Context().Done()
	})

	sandbox := newTestSandbox(t, "0.5.7", handler)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	startedAt := time.Now()
	handle, err := sandbox.Pty.Create(ctx, "bash", nil)
	if err != nil {
		t.Fatalf("Pty.Create failed before the process stream ended: %v", err)
	}
	if elapsed := time.Since(startedAt); elapsed > 500*time.Millisecond {
		t.Fatalf("Pty.Create waited %s after receiving the start frame", elapsed)
	}
	if handle.PID() != 23 {
		t.Fatalf("PID = %d, want 23", handle.PID())
	}
	cancel()
}

func TestPtyDefaultSize(t *testing.T) {
	var gotBody map[string]interface{}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody = readStreamRequest(t, r)
		writeStreamFrames(t, w,
			map[string]interface{}{
				"event": map[string]interface{}{"start": map[string]interface{}{"pid": 1}},
			},
		)
	})

	sandbox := newTestSandbox(t, "0.5.7", handler)

	if _, err := sandbox.Pty.Create(context.Background(), "bash", nil); err != nil {
		t.Fatalf("Pty.Create failed: %v", err)
	}

	pty, _ := gotBody["pty"].(map[string]interface{})
	size, _ := pty["size"].(map[string]interface{})
	if size["cols"] != float64(80) || size["rows"] != float64(24) {
		t.Errorf("default size = %v, want 80x24", size)
	}
}

func TestPtyCreateRejectsEmptyCommand(t *testing.T) {
	sandbox := newTestSandbox(t, "0.5.7", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

	if _, err := sandbox.Pty.Create(context.Background(), "", nil); err == nil {
		t.Fatal("expected an error for an empty command")
	}
}
