package consumer_test

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	novita "github.com/novitalabs/sandbox-sdks/sdk-go/v2"
	interpreter "github.com/novitalabs/sandbox-sdks/sdk-go/v2/code_interpreter"
	"github.com/novitalabs/sandbox-sdks/sdk-go/v2/core"
)

// These tests consume the SDK's public facade from a separate module, like a customer.
func fixture(t *testing.T) *core.SandboxOpts {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/sandboxes", "/sandboxes/consumer-sandbox/connect":
			if r.Header.Get("X-API-Key") != "consumer-api-key" {
				t.Error("control-plane API key did not reach request")
			}
			if strings.HasSuffix(r.URL.Path, "/connect") {
				var payload map[string]any
				_ = json.NewDecoder(r.Body).Decode(&payload)
				if payload["timeout"] != float64(300) {
					t.Errorf("connect lifetime = %v, want 300", payload["timeout"])
				}
			}
			io.WriteString(w, `{"sandboxID":"consumer-sandbox","templateID":"test-template","envdVersion":"0.5.7","envdAccessToken":"consumer-envd","trafficAccessToken":"consumer-traffic","domain":"local.invalid"}`)
		case "/sandboxes/consumer-sandbox":
			w.WriteHeader(http.StatusNoContent)
		case "/files":
			if r.Header.Get("X-Access-Token") != "consumer-envd" {
				t.Error("file request lost access token")
			}
			if r.Method == http.MethodGet {
				io.WriteString(w, "streamed payload\x00\xff")
			} else {
				body, _ := io.ReadAll(r.Body)
				if !strings.Contains(string(body), "streamed payload\x00\xff") {
					t.Error("uploaded bytes were not preserved")
				}
				io.WriteString(w, `[{"path":"/tmp/consumer.bin","name":"consumer.bin","type":"file"}]`)
			}
		case "/filesystem.Filesystem/Stat":
			w.WriteHeader(http.StatusNotFound)
			io.WriteString(w, `{"code":"not_found","message":"missing"}`)
		case "/process.Process/Start":
			body, _ := io.ReadAll(r.Body)
			w.Header().Set("Content-Type", "application/connect+json")
			frame(w, 0, `{"event":{"start":{"pid":41}}}`)
			if strings.Contains(string(body), "consumer-stall") {
				w.(http.Flusher).Flush()
				<-r.Context().Done()
				return
			}
			if strings.Contains(string(body), "consumer-denied") {
				frame(w, 2, `{"error":{"code":"permission_denied","message":"denied"},"metadata":{}}`)
				return
			}
			frame(w, 0, `{"event":{"data":{"stdout":"Y29uc3VtZXItb2sK"}}}`)
			frame(w, 0, `{"event":{"end":{"exitCode":0,"exited":true}}}`)
			frame(w, 2, `{"metadata":{"x-fixture":["ok"]}}`)
		case "/execute":
			if r.Header.Get("X-Access-Token") != "consumer-envd" || r.Header.Get("E2B-Traffic-Access-Token") != "consumer-traffic" {
				t.Error("interpreter request lost security headers")
			}
			w.Header().Set("Content-Type", "application/x-ndjson")
			io.WriteString(w, "{\"type\":\"stdout\",\"text\":\"consumer-ok\\n\"}\n{\"type\":\"result\",\"text\":\"42\",\"is_main_result\":true}\n")
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	return &core.SandboxOpts{APIKey: "consumer-api-key", APIURL: server.URL,
		SandboxURL: server.URL, Domain: "local.invalid"}
}

func frame(w io.Writer, flag byte, payload string) {
	header := make([]byte, 5)
	header[0] = flag
	binary.BigEndian.PutUint32(header[1:], uint32(len(payload)))
	w.Write(header)
	io.WriteString(w, payload)
}

func cleanupSandbox(t *testing.T, sandbox *core.Sandbox) {
	t.Helper()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if _, err := sandbox.Kill(ctx); err != nil {
			t.Errorf("sandbox cleanup failed: %v", err)
		}
	})
}

func TestPackageVersion(t *testing.T) {
	if expected := os.Getenv("NOVITA_EXPECTED_VERSION"); expected != "" && core.SDKVersion != expected {
		t.Fatalf("imported version %s, expected %s", core.SDKVersion, expected)
	}
}

func TestConsumerLifecycleFilesAndCommands(t *testing.T) {
	opts := fixture(t)
	ctx := context.Background()
	client, err := novita.New(opts)
	if err != nil {
		t.Fatal(err)
	}
	sandbox, err := client.Sandbox.Create(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	cleanupSandbox(t, sandbox)
	if _, err = sandbox.Files.WriteStream(ctx, "/tmp/consumer.bin", strings.NewReader("streamed payload\x00\xff"), nil); err != nil {
		t.Fatal(err)
	}
	stream, err := sandbox.Files.ReadStream(ctx, "/tmp/consumer.bin", nil)
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(stream)
	stream.Close()
	if err != nil || string(data) != "streamed payload\x00\xff" {
		t.Fatalf("streamed file content mismatch: %v", err)
	}
	exists, err := sandbox.Files.Exists(ctx, "/absent", nil)
	if err != nil || exists {
		t.Fatalf("missing-file exists = %v, %v", exists, err)
	}
	result, err := sandbox.Commands.Run(ctx, "printf consumer-ok", nil)
	if err != nil || result.Stdout != "consumer-ok\n" || result.ExitCode != 0 {
		t.Fatalf("command result = %+v, %v", result, err)
	}
	if _, err = client.Sandbox.Connect(ctx, sandbox.ID(), &core.SandboxOpts{Timeout: 300}); err != nil {
		t.Fatal(err)
	}
}

func TestConsumerCannotMistakeCommandFailureForSuccess(t *testing.T) {
	opts := fixture(t)
	ctx := context.Background()
	client, err := novita.New(opts)
	if err != nil {
		t.Fatal(err)
	}
	sandbox, err := client.Sandbox.Create(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	cleanupSandbox(t, sandbox)
	if _, err = sandbox.Commands.Run(ctx, "consumer-denied", nil); err == nil {
		t.Fatal("Connect permission_denied was reported as success")
	}
	timeout := 30
	start := time.Now()
	if _, err = sandbox.Commands.Run(ctx, "consumer-stall", &core.CommandStartOpts{TimeoutMs: &timeout}); err == nil {
		t.Fatal("unfinished command timeout was reported as success")
	}
	if time.Since(start) > time.Second {
		t.Fatal("command timeout did not cancel the body read")
	}
}

func TestConsumerInterpreterOutputAndReconnect(t *testing.T) {
	opts := fixture(t)
	ctx := context.Background()
	client, err := novita.New(opts)
	if err != nil {
		t.Fatal(err)
	}
	sandbox, err := client.CodeInterpreter.Create(ctx, &core.SandboxOpts{Timeout: 300})
	if err != nil {
		t.Fatal(err)
	}
	cleanupSandbox(t, sandbox.Sandbox)
	var callbackOutput strings.Builder
	result, err := sandbox.RunCode(ctx, "print('consumer-ok'); 6 * 7", &interpreter.RunCodeOpts{
		OnStdout: func(message interpreter.OutputMessage) { callbackOutput.WriteString(message.Line) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(result.Logs.Stdout, "") != "consumer-ok\n" || callbackOutput.String() != "consumer-ok\n" {
		t.Fatalf("NDJSON stdout/callback was not preserved: %+v", result)
	}
	if len(result.Results) != 1 || result.Results[0].Text != "42" {
		t.Fatalf("rich result lost: %+v", result.Results)
	}
	reconnected, err := client.CodeInterpreter.Connect(ctx, sandbox.ID(), &core.SandboxOpts{Timeout: 300})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = reconnected.RunCode(ctx, "6 * 7", nil); err != nil {
		t.Fatal(err)
	}
}

func TestConsumerDesktopEntrypoint(t *testing.T) {
	opts := fixture(t)
	client, err := novita.New(opts)
	if err != nil {
		t.Fatal(err)
	}
	sandbox, err := client.Desktop.Connect(context.Background(), "consumer-sandbox", &core.SandboxOpts{Timeout: 300})
	if err != nil {
		t.Fatal(err)
	}
	cleanupSandbox(t, sandbox.Sandbox)
	if sandbox.ID() != "consumer-sandbox" {
		t.Fatal("desktop connect returned another sandbox")
	}
}
