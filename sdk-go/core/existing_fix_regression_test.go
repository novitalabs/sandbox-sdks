package core

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"testing"
)

func TestExistingConnectDefaultsAndSecure(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		for _, tc := range []struct {
			name    string
			opts    *SandboxOpts
			timeout float64
			secure  any
		}{
			{"nil", nil, 300, nil},
			{"empty", &SandboxOpts{}, 300, nil},
			{"explicit", &SandboxOpts{Timeout: 90, SecureOption: Bool(false)}, 90, false},
			{"secure", &SandboxOpts{Secure: true}, 300, true},
		} {
			t.Run(tc.name, func(t *testing.T) {
				c := reportClient(legacy, func(r *http.Request) (*http.Response, error) {
					var body map[string]any
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Fatal(err)
					}
					if r.URL.Path != "/sandboxes/demo/connect" || body["timeout"] != tc.timeout || body["secure"] != tc.secure {
						t.Errorf("request = %s, body = %v", r.URL, body)
					}
					if legacy && body["autoPause"] != false {
						t.Errorf("legacy autoPause = %v", body["autoPause"])
					}
					return reportResponse(200, `{"sandboxID":"demo","envdVersion":"0.5.7","trafficAccessToken":"token"}`), nil
				})
				info, err := c.ConnectSandbox(context.Background(), "demo", tc.opts)
				if err != nil {
					t.Fatal(err)
				}
				s, err := newSandboxFromInfo(c, info, &SandboxOpts{})
				if err != nil || s.TrafficAccessToken() != "token" {
					t.Fatalf("connect token missing: %v", err)
				}
			})
		}
	}
}

func TestExistingCreateSecureAndLongRunningPassthrough(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		for _, long := range []string{"true", "false"} {
			c := reportClient(legacy, func(r *http.Request) (*http.Response, error) {
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Fatal(err)
				}
				if body["secure"] != false || (long == "true" && body["timeout"] != nil || long != "true" && body["timeout"] != float64(60)) || !reflect.DeepEqual(body["metadata"], map[string]any{"long_running": long, "idle_timeout": "30"}) {
					t.Errorf("create payload = %v", body)
				}
				return reportResponse(200, `{"sandboxID":"demo","envdVersion":"0.5.7"}`), nil
			})
			_, err := c.CreateSandbox(context.Background(), &SandboxOpts{Template: "base", Timeout: 60, SecureOption: Bool(false), Metadata: map[string]string{"long_running": long, "idle_timeout": "30"}})
			if err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestExistingDockerfileCopyFlags(t *testing.T) {
	for _, input := range []string{
		"COPY --chmod=0644 input.txt /app/input.txt",
		`COPY --chmod=0644 ["input.txt", "/app/input.txt"]`,
	} {
		b := NewTemplateBuilder().FromDockerfile("FROM ubuntu:22.04\n" + input)
		if b.Err() != nil {
			t.Fatal(b.Err())
		}
		instructions, err := decodeInstructions(b.Spec()["instructions"])
		if err != nil {
			t.Fatal(err)
		}
		if len(instructions) != 1 || instructions[0].Type != "COPY" || !reflect.DeepEqual(instructions[0].Args, []string{"input.txt", "/app/input.txt", "", "0644"}) {
			t.Fatalf("COPY instructions = %+v", instructions)
		}
	}
}

type reportContextReader struct{ ctx context.Context }

func (r reportContextReader) Read([]byte) (int, error) { <-r.ctx.Done(); return 0, r.ctx.Err() }

func TestExistingCommandStreamOutcomes(t *testing.T) {
	for _, mode := range []string{"success", "nonzero", "deadline", "canceled", "no end"} {
		t.Run(mode, func(t *testing.T) {
			c := reportClient(false, nil)
			s, err := newSandboxFromInfo(c, &SandboxInfo{SandboxID: "demo", EnvdVersion: "0.5.7"}, &SandboxOpts{})
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			s.rpc.http.Transport = reportTransport(func(r *http.Request) (*http.Response, error) {
				frames := envelopeFrame([]byte(`{"event":{"start":{"pid":42}}}`))
				frames = append(frames, envelopeFrame([]byte(`{"event":{"data":{"stdout":"b2s="}}}`))...)
				if mode == "success" {
					frames = append(frames, envelopeFrame([]byte(`{"event":{"end":{"exitCode":0}}}`))...)
				}
				if mode == "nonzero" {
					frames = append(frames, envelopeFrame([]byte(`{"event":{"end":{"exitCode":7}}}`))...)
				}
				var reader io.Reader = bytes.NewReader(frames)
				if mode == "deadline" || mode == "canceled" {
					reader = io.MultiReader(reader, reportContextReader{r.Context()})
				}
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(reader)}, nil
			})
			timeout := 100
			opts := &CommandStartOpts{TimeoutMs: &timeout}
			if mode == "canceled" {
				opts.OnStdout = func(string) { cancel() }
			}
			result, err := s.Commands.Run(ctx, "echo ok", opts)
			switch mode {
			case "success":
				if err != nil || result.ExitCode != 0 || result.Stdout != "ok" {
					t.Fatalf("result = %+v %v", result, err)
				}
			case "nonzero":
				var exit *CommandExitError
				if !errors.As(err, &exit) || result.ExitCode != 7 {
					t.Fatalf("result = %+v %v", result, err)
				}
			case "deadline":
				if !errors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("deadline returned %v", err)
				}
			case "canceled":
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("cancellation returned %v", err)
				}
			case "no end":
				if err == nil {
					t.Fatalf("stream without End succeeded: %+v", result)
				}
			}
		})
	}
}
