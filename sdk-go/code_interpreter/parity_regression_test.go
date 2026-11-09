package code_interpreter

import (
	"context"
	"errors"
	"github.com/novitalabs/sandbox-sdks/sdk-go/v2/core"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestParityRunCodeBodyTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		w.WriteHeader(200)
		w.(http.Flusher).Flush()
		select {
		case <-r.Context().Done():
		case <-time.After(200 * time.Millisecond):
		}
	}))
	defer srv.Close()
	cfg, err := core.NewConnectionConfig(&core.SandboxOpts{SandboxURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	inner, err := core.NewSandboxFromConfig(cfg, &core.SandboxInfo{SandboxID: "test", EnvdVersion: "0.5.7"})
	if err != nil {
		t.Fatal(err)
	}
	s := &Sandbox{Sandbox: inner, http: &http.Client{}}
	timeout := 20 * time.Millisecond
	start := time.Now()
	out, err := s.RunCode(context.Background(), "while True: pass", &RunCodeOpts{Timeout: &timeout})
	elapsed := time.Since(start)
	if elapsed > 120*time.Millisecond || err == nil {
		t.Fatalf("Timeout=20ms did not abort idle body: elapsed=%s error=%v result=%#v", elapsed, err, out)
	}
}

func newParityInterpreter(t *testing.T, handler http.Handler, requestMS int) *Sandbox {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	cfg, err := core.NewConnectionConfig(&core.SandboxOpts{SandboxURL: srv.URL, RequestTimeoutMs: &requestMS})
	if err != nil {
		t.Fatal(err)
	}
	inner, err := core.NewSandboxFromConfig(cfg, &core.SandboxInfo{SandboxID: "test", EnvdVersion: "0.5.7"})
	if err != nil {
		t.Fatal(err)
	}
	return &Sandbox{Sandbox: inner, http: &http.Client{}}
}
func TestRunCodeRequestTimeoutAndOverride(t *testing.T) {
	for _, tc := range []struct {
		name     string
		override *time.Duration
		fail     bool
	}{{"configured default", nil, true}, {"disabled", durationPointer(0), false}, {"extended", durationPointer(time.Second), false}} {
		t.Run(tc.name, func(t *testing.T) {
			s := newParityInterpreter(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				select {
				case <-r.Context().Done():
					return
				case <-time.After(60 * time.Millisecond):
				}
				io.WriteString(w, "{\"type\":\"stdout\",\"text\":\"ok\"}\n")
			}), 20)
			result, err := s.RunCode(context.Background(), "print('ok')", &RunCodeOpts{RequestTimeout: tc.override})
			if tc.fail {
				var timeout *core.TimeoutError
				if !errors.As(err, &timeout) || timeout.Operation != "request" {
					t.Fatalf("request timeout=%v", err)
				}
			} else if err != nil || result == nil {
				t.Fatalf("result=%v err=%v", result, err)
			}
		})
	}
}
func durationPointer(value time.Duration) *time.Duration { return &value }
func TestRunCodeParentCancellation(t *testing.T) {
	started := make(chan struct{})
	s := newParityInterpreter(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.(http.Flusher).Flush()
		close(started)
		<-r.Context().Done()
	}), 1000)
	ctx, cancel := context.WithCancel(context.Background())
	go func() { <-started; cancel() }()
	if _, err := s.RunCode(ctx, "pass", nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("parent cancellation=%v", err)
	}
}
func TestContextRequestUsesConfiguredTimeout(t *testing.T) {
	s := newParityInterpreter(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }), 20)
	_, err := s.ListCodeContexts(context.Background(), nil)
	var timeout *core.TimeoutError
	if !errors.As(err, &timeout) || timeout.Operation != "request" {
		t.Fatalf("context request timeout=%v", err)
	}
}

func TestRunCodeCancellationStopsBufferedEvents(t *testing.T) {
	s := newParityInterpreter(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "{\"type\":\"stdout\",\"text\":\"first\"}\n{\"type\":\"stdout\",\"text\":\"second\"}\n{\"type\":\"stdout\",\"text\":\"third\"}\n")
	}), 1000)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	callbacks := 0
	result, err := s.RunCode(ctx, "pass", &RunCodeOpts{OnStdout: func(OutputMessage) { callbacks++; cancel() }})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation=%v", err)
	}
	if callbacks != 1 {
		t.Fatalf("received %d callbacks after cancellation; want exactly one", callbacks)
	}
	if len(result.Logs.Stdout) != 1 || result.Logs.Stdout[0] != "first" {
		t.Fatalf("buffered events accumulated after cancellation: %+v", result.Logs)
	}
}

func TestRunCodeErrorBodyRemainsBounded(t *testing.T) {
	for _, tc := range []struct {
		name               string
		request, execution time.Duration
		operation          string
	}{{"request deadline", 20 * time.Millisecond, 0, "request"}, {"execution deadline", 0, 20 * time.Millisecond, "execution"}} {
		t.Run(tc.name, func(t *testing.T) {
			s := newParityInterpreter(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(500)
				w.(http.Flusher).Flush()
				<-r.Context().Done()
			}), 1000)
			// Keep the independent hang guard well above the SDK deadlines.
			// Assert which timer canceled the body, not scheduler wall-clock latency.
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			begin := time.Now()
			_, err := s.RunCode(ctx, "pass", &RunCodeOpts{Timeout: &tc.execution, RequestTimeout: &tc.request})
			elapsed := time.Since(begin)
			var timeout *core.TimeoutError
			if !errors.As(err, &timeout) || timeout.Operation != tc.operation {
				t.Fatalf("error body elapsed=%v err=%v; want %s timeout", elapsed, err, tc.operation)
			}
		})
	}
}

func TestReadErrorBodyStopsAtSizeLimit(t *testing.T) {
	const limit = 1 << 20
	got, err := readAllLimited(strings.NewReader(strings.Repeat("x", limit+1024)), limit)
	if err != nil || len(got) != limit {
		t.Fatalf("limited body length=%d err=%v", len(got), err)
	}
}
