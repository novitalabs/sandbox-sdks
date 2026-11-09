package novita

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/novitalabs/sandbox-sdks/sdk-go/v2/core"
)

func TestNewExposesAllNamespaces(t *testing.T) {
	client, err := New(&core.SandboxOpts{APIKey: "test-key"})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if client.Client == nil || client.Sandbox == nil || client.Template == nil ||
		client.Volume == nil || client.Secret == nil || client.CodeInterpreter == nil ||
		client.Desktop == nil || client.Job == nil {
		t.Fatal("New() did not initialize every namespace")
	}
}

func TestJobInheritsFacadeConnection(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/migrations" || r.URL.Query().Get("job_id") != "job" ||
			r.Header.Get("Authorization") != "Bearer access-token" || r.Header.Get("X-Custom") != "custom" {
			t.Errorf("unexpected job request: %s %v", r.URL, r.Header)
		}
		fmt.Fprint(w, `{"items":[{"jobId":"job","status":"completed"}]}`)
	}))
	defer server.Close()
	client, err := New(&core.SandboxOpts{AccessToken: "access-token", APIURL: server.URL,
		Domain: "modern.example", Headers: map[string]string{"X-Custom": "custom"}})
	if err != nil {
		t.Fatal(err)
	}
	status, err := client.Job.Get(context.Background(), "job")
	if err != nil || status.Status != core.JobCompleted {
		t.Fatalf("%+v %v", status, err)
	}
}

func TestMergeSandboxOpts(t *testing.T) {
	baseTimeout := 60_000
	callTimeout := 10_000
	debug := true
	base := &core.SandboxOpts{
		APIKey:           "base-key",
		AccessToken:      "base-token",
		Domain:           "base.example",
		APIURL:           "https://api.base.example",
		SandboxURL:       "https://sandbox.base.example",
		SandboxDomain:    "runtime.base.example",
		Debug:            &debug,
		RequestTimeoutMs: &baseTimeout,
		Headers:          map[string]string{"base": "yes", "shared": "base"},
		Template:         "must-not-be-inherited",
	}
	call := &core.SandboxOpts{
		Template:         "call-template",
		RequestTimeoutMs: &callTimeout,
		Headers:          map[string]string{"call": "yes", "shared": "call"},
	}

	got := mergeSandboxOpts(base, call)
	if got.APIKey != base.APIKey || got.AccessToken != base.AccessToken || got.Domain != base.Domain {
		t.Fatalf("connection options were not inherited: %#v", got)
	}
	if got.Template != "call-template" {
		t.Fatalf("Template = %q, want call-template", got.Template)
	}
	if got.RequestTimeoutMs != &callTimeout {
		t.Fatal("per-call timeout did not override the client timeout")
	}
	if got.Headers["base"] != "yes" || got.Headers["call"] != "yes" || got.Headers["shared"] != "call" {
		t.Fatalf("Headers = %#v", got.Headers)
	}

	got.Headers["base"] = "changed"
	if base.Headers["base"] != "yes" || call.Headers["base"] != "" {
		t.Fatal("merged headers alias an input map")
	}
}

func TestMergeSandboxOptsDoesNotInheritCreationFields(t *testing.T) {
	base := &core.SandboxOpts{
		APIKey:   "key",
		Template: "base-template",
		Timeout:  300,
		EnvVars:  map[string]string{"BASE": "value"},
	}

	got := mergeSandboxOpts(base, nil)
	if got.APIKey != "key" {
		t.Fatal("API key was not inherited")
	}
	if got.Template != "" || got.Timeout != 0 || got.EnvVars != nil {
		t.Fatalf("creation fields leaked from client options: %#v", got)
	}
}
