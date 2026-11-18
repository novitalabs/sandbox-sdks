package core

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
)

func TestPauseSurfacesResizeConflict(t *testing.T) {
	server := newIPv4Server(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSONStatus(t, w, http.StatusConflict, map[string]interface{}{
			"code":    409,
			"message": `Sandbox "sb-1" is being resized`,
		})
	}))
	client, err := NewClient(&SandboxOpts{APIKey: "k", APIURL: server.URL})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}

	paused, err := client.PauseSandbox(context.Background(), "sb-1")
	if paused {
		t.Fatal("PauseSandbox = true, want false")
	}
	if err == nil || !strings.Contains(err.Error(), "409") || !strings.Contains(err.Error(), "being resized") {
		t.Fatalf("error = %v, want a 409 resize conflict", err)
	}
	var sandboxErr *SandboxError
	if !errors.As(err, &sandboxErr) || sandboxErr.StatusCode != http.StatusConflict {
		t.Fatalf("error type = %T, want SandboxError with status 409", err)
	}
}

func writeJSONStatus(t *testing.T, w http.ResponseWriter, status int, body interface{}) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		t.Fatalf("encode response: %v", err)
	}
}

func pauseClient(t *testing.T, body string) *Client {
	t.Helper()
	server := newIPv4Server(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(body))
	}))
	client, err := NewClient(&SandboxOpts{APIKey: "k", APIURL: server.URL})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	return client
}

func TestPauseTreatsEscapedAlreadyPausedAsNoOp(t *testing.T) {
	client := pauseClient(t, `{"code":409,"message":"Sandbox sb-1 is already\u0020paused"}`)
	paused, err := client.PauseSandbox(context.Background(), "sb-1")
	if err != nil || paused {
		t.Fatalf("PauseSandbox = (%v, %v), want (false, nil)", paused, err)
	}
}

func TestPauseIgnoresUnrelatedAlreadyPausedText(t *testing.T) {
	client := pauseClient(t, `{"code":409,"message":"Sandbox sb-1 is being resized","previousError":"already paused"}`)
	_, err := client.PauseSandbox(context.Background(), "sb-1")
	if err == nil || !strings.Contains(err.Error(), "being resized") {
		t.Fatalf("error = %v, want the resize conflict", err)
	}
}

func TestPauseRaisesOnUnusableConflictMessages(t *testing.T) {
	bodies := []string{
		`{"reason":"being resized","previousError":"already paused"}`,
		`{"message":"","previousError":"already paused"}`,
		`{"message":null,"previousError":"already paused"}`,
		`{"message":123,"previousError":"already paused"}`,
	}
	for _, body := range bodies {
		t.Run(body, func(t *testing.T) {
			client := pauseClient(t, body)
			_, err := client.PauseSandbox(context.Background(), "sb-1")
			if err == nil || !strings.Contains(err.Error(), "409") {
				t.Fatalf("error = %v, want a 409 conflict", err)
			}
		})
	}
}

func TestPausePropagatesConflictBodyReadErrors(t *testing.T) {
	server := newIPv4Server(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "100")
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"message":"already paused"}`))
	}))
	client, err := NewClient(&SandboxOpts{APIKey: "k", APIURL: server.URL})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	_, err = client.PauseSandbox(context.Background(), "sb-1")
	if err == nil || !strings.Contains(err.Error(), "failed to read pause response") {
		t.Fatalf("error = %v, want a body read failure", err)
	}
}

func TestPauseRejectsMalformedConflictJSON(t *testing.T) {
	bodies := []string{
		`{"message":"Sandbox sb-1 is being resized","previousError":"already paused"`,
		`{'message':'already paused'}`,
		`{message:"already paused"}`,
		`{"message":"already paused"/*comment*/}`,
		`{"message":"already paused","extra":TRUE}`,
		`{"message":"already paused
"}`,
	}
	for _, body := range bodies {
		client := pauseClient(t, body)
		_, err := client.PauseSandbox(context.Background(), "sb-1")
		if err == nil || !strings.Contains(err.Error(), "409") {
			t.Fatalf("error = %v, want a 409 conflict for %s", err, body)
		}
	}
}
