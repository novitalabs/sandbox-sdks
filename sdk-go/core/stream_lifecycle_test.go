package core

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestCommandAndPtyIncompleteStream(t *testing.T) {
	for _, pty := range []bool{false, true} {
		t.Run(map[bool]string{false: "command", true: "pty"}[pty], func(t *testing.T) {
			s := newTestSandbox(t, "0.5.7", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				writeStreamFrames(t, w, map[string]any{"event": map[string]any{"start": map[string]any{"pid": 7}}})
			}))
			var err error
			if pty {
				h, e := s.Pty.Create(context.Background(), "bash", nil)
				err = e
				if err == nil {
					_, err = h.Wait(context.Background())
				}
			} else {
				_, err = s.Commands.Run(context.Background(), "echo", nil)
			}
			if err == nil || !strings.Contains(err.Error(), "without a result") {
				t.Fatalf("incomplete stream = %v", err)
			}
		})
	}
}
func TestCommandDisconnectReconnectAndTerminalResult(t *testing.T) {
	s := newTestSandbox(t, "0.5.7", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		readStreamRequest(t, r)
		if strings.HasSuffix(r.URL.Path, "/Start") {
			writeStreamFrames(t, w, map[string]any{"event": map[string]any{"start": map[string]any{"pid": 7}}})
			<-r.Context().Done()
			return
		}
		if !strings.HasSuffix(r.URL.Path, "/Connect") {
			t.Errorf("disconnect sent unexpected RPC %s", r.URL.Path)
			return
		}
		writeStreamFrames(t, w, map[string]any{"event": map[string]any{"start": map[string]any{"pid": 7}}}, map[string]any{"event": map[string]any{"data": map[string]any{"stdout": []byte("continued")}}}, map[string]any{"event": map[string]any{"end": map[string]any{"exitCode": 0}}})
		<-r.Context().Done()
	}))
	h, err := s.Commands.Start(context.Background(), "sleep", nil)
	if err != nil {
		t.Fatal(err)
	}
	h.Disconnect()
	h.Disconnect()
	if _, err = h.Wait(context.Background()); !errors.Is(err, context.Canceled) {
		t.Fatalf("disconnect=%v", err)
	}
	reconnected, err := s.Commands.Connect(context.Background(), h.PID(), nil)
	if err != nil {
		t.Fatal(err)
	}
	result, err := reconnected.Wait(context.Background())
	if err != nil || result.Stdout != "continued" || reconnected.Stdout() != "continued" {
		t.Fatalf("result=%v err=%v", result, err)
	}
}
func TestPtyDisconnectAndSuccessfulEnd(t *testing.T) {
	for _, end := range []bool{false, true} {
		t.Run(map[bool]string{false: "disconnect", true: "end"}[end], func(t *testing.T) {
			s := newTestSandbox(t, "0.5.7", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				frames := []any{map[string]any{"event": map[string]any{"start": map[string]any{"pid": 7}}}}
				if end {
					frames = append(frames, map[string]any{"event": map[string]any{"end": map[string]any{"exitCode": 2}}})
				}
				writeStreamFrames(t, w, frames...)
				<-r.Context().Done()
			}))
			h, err := s.Pty.Create(context.Background(), "bash", nil)
			if err != nil {
				t.Fatal(err)
			}
			if !end {
				h.Disconnect()
			}
			code, err := h.Wait(context.Background())
			if end && (err != nil || code != 2) {
				t.Fatalf("end code=%d error=%v", code, err)
			}
			if !end && !errors.Is(err, context.Canceled) {
				t.Fatalf("disconnect=%v", err)
			}
		})
	}
}
func TestWatchRegistrationDeadlineAndCallbackStop(t *testing.T) {
	t.Run("deadline", func(t *testing.T) {
		s := newTestSandbox(t, "0.5.7", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(200)
			w.(http.Flusher).Flush()
			<-r.Context().Done()
		}))
		ms := 20
		h, err := s.Files.WatchDir(context.Background(), "/tmp", &WatchOptions{FilesystemRequestOpts: FilesystemRequestOpts{RequestTimeoutMs: &ms}})
		if h != nil || !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("watch=%v error=%v", h, err)
		}
	})
	t.Run("stop in callback", func(t *testing.T) {
		emit := make(chan struct{})
		s := newTestSandbox(t, "0.5.7", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get(KeepAlivePingHeader) != "50" {
				t.Error("keepalive header missing")
			}
			writeStreamFrames(t, w, map[string]any{"start": map[string]any{}})
			<-emit
			w.Write(envelopeFrame([]byte(`{"filesystem":{"name":"file","type":"CREATE"}}`)))
			w.(http.Flusher).Flush()
			<-r.Context().Done()
		}))
		var h *WatchHandle
		var err error
		h, err = s.Files.WatchDir(context.Background(), "/tmp", &WatchOptions{OnEvent: func(event FilesystemEvent) {
			if event.Name != "file" {
				t.Error(event)
			}
			h.Stop()
		}})
		if err != nil {
			t.Fatal(err)
		}
		close(emit)
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err = h.Wait(ctx); !errors.Is(err, context.Canceled) {
			t.Fatalf("watch stop=%v", err)
		}
	})
}

func TestCommandAndWatchSurfaceNestedTerminalError(t *testing.T) {
	for _, watch := range []bool{false, true} {
		t.Run(map[bool]string{false: "command", true: "watch"}[watch], func(t *testing.T) {
			release := make(chan struct{})
			s := newTestSandbox(t, "0.5.7", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				event := map[string]any{"event": map[string]any{"start": map[string]any{"pid": 9}}}
				if watch {
					event = map[string]any{"start": map[string]any{}}
				}
				writeStreamFrames(t, w, event)
				<-release
				frame := envelopeFrame([]byte(`{"error":{"code":"permission_denied","message":"fixture denied"},"metadata":{}}`))
				frame[0] = 2
				w.Write(frame)
				w.(http.Flusher).Flush()
			}))
			var err error
			if watch {
				h, e := s.Files.WatchDir(context.Background(), "/tmp", nil)
				if e != nil {
					t.Fatal(e)
				}
				close(release)
				err = h.Wait(context.Background())
			} else {
				h, e := s.Commands.Start(context.Background(), "command", nil)
				if e != nil {
					t.Fatal(e)
				}
				close(release)
				_, err = h.Wait(context.Background())
			}
			if err == nil || !strings.Contains(err.Error(), "fixture denied") {
				t.Fatalf("terminal error=%v", err)
			}
		})
	}
}
