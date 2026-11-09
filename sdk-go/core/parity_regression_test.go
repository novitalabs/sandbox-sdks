package core

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"testing"
	"time"
)

func TestParityConnectEndStreamError(t *testing.T) {
	frame := envelopeFrame([]byte(`{"error":{"code":"permission_denied","message":"denied"},"metadata":{}}`))
	frame[0] = 2
	_, err := readEnvelope(bufio.NewReader(bytes.NewReader(frame)))
	if errors.Is(err, io.EOF) || err == nil {
		t.Fatalf("Connect terminal permission_denied was swallowed as %v", err)
	}
}
func TestParityCommandTimeoutCannotSucceed(t *testing.T) {
	s := newTestSandbox(t, "0.5.7", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeStreamFrames(t, w, map[string]any{"event": map[string]any{"start": map[string]any{"pid": 42}}})
		<-r.Context().Done()
	}))
	timeout := 30
	result, err := s.Commands.Run(context.Background(), "sleep 99", &CommandStartOpts{TimeoutMs: &timeout})
	if err == nil {
		t.Fatalf("timed out command falsely succeeded: %#v", result)
	}
}
func TestParityPtyTimeoutCannotSucceed(t *testing.T) {
	s := newTestSandbox(t, "0.5.7", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeStreamFrames(t, w, map[string]any{"event": map[string]any{"start": map[string]any{"pid": 42}}})
		<-r.Context().Done()
	}))
	timeout := 30
	h, err := s.Pty.Create(context.Background(), "bash", &PtyCreateOpts{TimeoutMs: &timeout})
	if err != nil {
		t.Fatal(err)
	}
	code, err := h.Wait(context.Background())
	if err == nil {
		t.Fatalf("timed out PTY falsely succeeded: exitCode=%d", code)
	}
}
func TestParityBackgroundPreservesCommand(t *testing.T) {
	seen := make(chan string, 1)
	s := newTestSandbox(t, "0.5.7", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := readStreamRequest(t, r)
		args := body["process"].(map[string]any)["args"].([]any)
		seen <- args[2].(string)
		writeStreamFrames(t, w, map[string]any{"event": map[string]any{"start": map[string]any{"pid": 42}}})
		<-r.Context().Done()
	}))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, err := s.Commands.Start(ctx, "echo hello; sleep 99", &CommandStartOpts{Background: true})
	if err != nil {
		t.Fatal(err)
	}
	if got := <-seen; got != "echo hello; sleep 99" {
		t.Fatalf("background rewrites command and discards streams: %q", got)
	}
}
func TestParityWatchDirUser(t *testing.T) {
	auth := make(chan string, 1)
	s := newTestSandbox(t, "0.5.7", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth <- r.Header.Get("Authorization")
		writeStreamFrames(t, w, map[string]any{"start": map[string]any{}})
		<-r.Context().Done()
	}))
	h, err := s.Files.WatchDir(context.Background(), "/root", &WatchOptions{FilesystemRequestOpts: FilesystemRequestOpts{User: Username{Name: "root"}}})
	if err != nil {
		t.Fatal(err)
	}
	defer h.Stop()
	select {
	case got := <-auth:
		if got == "" {
			t.Fatal("WatchDir silently dropped explicit root Authorization header")
		}
	case <-time.After(time.Second):
		t.Fatal("request missing")
	}
}
func TestParityFacadeOptionsInherited(t *testing.T) {
	t.Setenv("NOVITA_API_KEY", "")
	t.Setenv("NOVITA_ACCESS_TOKEN", "")
	t.Setenv("NOVITA_DEBUG", "")
	server := newIPv4Server(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-API-Key") != "dummy" {
			t.Error("constructor key lost")
		}
		io.WriteString(w, `{"sandboxID":"sb","envdVersion":"0.5.7"}`)
	}))
	n, err := NewNovita(&SandboxOpts{APIKey: "dummy", APIURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	for name, call := range map[string]func() error{
		"Create":          func() error { _, e := n.Create(context.Background(), nil); return e },
		"Connect":         func() error { _, e := n.Connect(context.Background(), "x", nil); return e },
		"Sandbox.Create":  func() error { _, e := n.Sandbox.Create(context.Background(), nil); return e },
		"Sandbox.Connect": func() error { _, e := n.Sandbox.Connect(context.Background(), "x", nil); return e },
	} {
		t.Run(name, func(t *testing.T) {
			err := call()
			if err != nil {
				t.Fatalf("configured dummy key lost: %v", err)
			}
		})
	}
}

func TestParityNetworkOmittedPublicTraffic(t *testing.T) {
	raw, err := json.Marshal(createSandboxRequest{Network: &SandboxNetworkConfig{AllowOut: []string{"1.1.1.1"}}})
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte(`"allowPublicTraffic":false`)) {
		t.Fatalf("egress-only network unexpectedly disables public URLs: %s", raw)
	}
}
func TestParityDockerfileExecCMD(t *testing.T) {
	b := NewTemplateBuilder().FromDockerfile("FROM python:3\nCMD [\"python\", \"app.py\"]\n")
	if b.Err() != nil {
		t.Fatal(b.Err())
	}
	if got := b.Spec()["startCmd"]; got != "python app.py" {
		t.Fatalf("exec-form CMD copied as literal shell command: %#v", got)
	}
}
func TestParityWriteTimeoutOverride(t *testing.T) {
	s := newTestSandbox(t, "0.5.7", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(60 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `[{"name":"a","path":"/a"}]`)
	}))
	s.connection.RequestTimeoutMs = 20
	override := 200
	_, err := s.Files.Write(context.Background(), "/a", []byte("a"), &FilesystemWriteOpts{FilesystemRequestOpts: FilesystemRequestOpts{RequestTimeoutMs: &override}})
	if err != nil {
		t.Fatalf("200ms per-call timeout overruled by 20ms connection default: %v", err)
	}
}
func TestParityAccessTokenOnly(t *testing.T) {
	clearNovitaEnv(t)
	_, err := NewClient(&SandboxOpts{AccessToken: "dummy"})
	if err != nil {
		t.Fatalf("Bearer-token client rejected before any request: %v", err)
	}
}

func TestParityConnectDefaultTimeout(t *testing.T) {
	got := make(chan map[string]any, 1)
	server := newIPv4Server(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		got <- body
		io.WriteString(w, `{"sandboxID":"sb","envdVersion":"0.5.7"}`)
	}))
	_, err := Connect(context.Background(), "sb", &SandboxOpts{APIKey: "dummy", APIURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	if b := <-got; b["timeout"] != float64(300) {
		t.Fatalf("omitted connect timeout did not use 300 seconds: %#v", b)
	}
}
func TestParityConnectExplicitSecureFalse(t *testing.T) {
	got := make(chan map[string]any, 1)
	server := newIPv4Server(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		got <- body
		io.WriteString(w, `{"sandboxID":"sb","envdVersion":"0.5.7"}`)
	}))
	_, err := Connect(context.Background(), "sb", &SandboxOpts{APIKey: "dummy", APIURL: server.URL, Timeout: 300, SecureOption: Bool(false)})
	if err != nil {
		t.Fatal(err)
	}
	if b := <-got; b["secure"] != false {
		t.Fatalf("explicit secure=false omitted on wire: %#v", b)
	}
}
