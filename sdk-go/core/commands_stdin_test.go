package core

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"
)

func TestCommandsStdinRequest(t *testing.T) {
	tests := []struct {
		name              string
		envdVersion       string
		opts              *CommandStartOpts
		wantStdin         interface{}
		wantTemplateError bool
	}{
		{name: "default", envdVersion: "0.5.12", wantStdin: false},
		{name: "zero options", envdVersion: "0.5.12", opts: &CommandStartOpts{}, wantStdin: false},
		{name: "explicit false", envdVersion: "0.5.12", opts: &CommandStartOpts{Stdin: false}, wantStdin: false},
		{name: "enabled", envdVersion: "0.5.12", opts: &CommandStartOpts{Stdin: true}, wantStdin: true},
		{name: "minimum version default", envdVersion: "0.3.0", wantStdin: false},
		{name: "minimum version false", envdVersion: "0.3.0", opts: &CommandStartOpts{Stdin: false}, wantStdin: false},
		{name: "minimum version enabled", envdVersion: "0.3.0", opts: &CommandStartOpts{Stdin: true}, wantStdin: true},
		{name: "legacy default", envdVersion: "0.2.9"},
		{name: "legacy zero options", envdVersion: "0.2.9", opts: &CommandStartOpts{}},
		{name: "legacy false", envdVersion: "0.2.9", opts: &CommandStartOpts{Stdin: false}},
		{name: "legacy enabled", envdVersion: "0.2.9", opts: &CommandStartOpts{Stdin: true}, wantTemplateError: true},
	}
	for _, method := range []string{"Start", "Run"} {
		for _, tt := range tests {
			t.Run(method+"/"+tt.name, func(t *testing.T) {
				requests := make(chan map[string]interface{}, 1)
				sandbox := newTestSandbox(t, tt.envdVersion, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path != "/process.Process/Start" {
						t.Errorf("path = %q, want the process Start RPC", r.URL.Path)
					}
					requests <- readStreamRequest(t, r)
					writeStreamFrames(t, w,
						map[string]interface{}{"event": map[string]interface{}{"start": map[string]interface{}{"pid": 7}}},
						map[string]interface{}{"event": map[string]interface{}{"end": map[string]interface{}{"exitCode": 0, "exited": true}}},
					)
				}))
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()

				var err error
				if method == "Start" {
					var handle *CommandHandle
					handle, err = sandbox.Commands.Start(ctx, "cat", tt.opts)
					if err == nil {
						_, err = handle.Wait(ctx)
					}
				} else {
					_, err = sandbox.Commands.Run(ctx, "cat", tt.opts)
				}
				if tt.wantTemplateError {
					var templateErr *TemplateError
					if !errors.As(err, &templateErr) {
						t.Fatalf("error = %T %v, want *TemplateError", err, err)
					}
					select {
					case <-requests:
						t.Error("unsupported stdin sent a request")
					default:
					}
					return
				}
				if err != nil {
					t.Fatalf("%s failed: %v", method, err)
				}

				select {
				case request := <-requests:
					stdin, present := request["stdin"]
					if present != (tt.wantStdin != nil) || stdin != tt.wantStdin {
						t.Errorf("stdin = %v (present=%t), want %v (present=%t)", stdin, present, tt.wantStdin, tt.wantStdin != nil)
					}
				default:
					t.Fatal("missing process Start request")
				}
			})
		}
	}
}
