package core

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

func TestResourceTargetsToAPI(t *testing.T) {
	body, err := (SandboxResourceTargets{CPU: 4, MemoryGiB: 2, DiskGiB: 4}).toAPI()
	if err != nil {
		t.Fatalf("toAPI failed: %v", err)
	}
	want := map[string]int64{"vcpu": 4, "memory_mb": 2048, "disk_size_mb": 4096}
	for key, expected := range want {
		if body[key] != expected {
			t.Errorf("%s = %d, want %d", key, body[key], expected)
		}
	}

	// A template disk can be a non-integral number of GiB but a whole number
	// of MiB; the request still carries the exact MiB value.
	body, err = (SandboxResourceTargets{DiskGiB: 2669.0 / 1024.0}).toAPI()
	if err != nil {
		t.Fatalf("toAPI failed: %v", err)
	}
	if body["disk_size_mb"] != 2669 {
		t.Errorf("disk_size_mb = %d, want 2669", body["disk_size_mb"])
	}
}

func TestResourceTargetsRejectInvalidValues(t *testing.T) {
	cases := []struct {
		name    string
		targets SandboxResourceTargets
		match   string
	}{
		{"empty", SandboxResourceTargets{}, "at least one"},
		{"negative cpu", SandboxResourceTargets{CPU: -1}, "positive whole number"},
		{"negative memory", SandboxResourceTargets{MemoryGiB: -1}, "positive number"},
		{"infinite memory", SandboxResourceTargets{MemoryGiB: math.Inf(1)}, "positive number"},
		{"not exact memory", SandboxResourceTargets{MemoryGiB: 1.1}, "exact number of MiB"},
		{"not exact disk", SandboxResourceTargets{DiskGiB: 1.1}, "exact number of MiB"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tc.targets.toAPI()
			if err == nil {
				t.Fatal("toAPI succeeded, want an error")
			}
			if _, ok := err.(*InvalidArgumentError); !ok {
				t.Fatalf("error = %T, want *InvalidArgumentError", err)
			}
			if !strings.Contains(err.Error(), tc.match) {
				t.Fatalf("error = %q, want it to mention %q", err, tc.match)
			}
		})
	}
}

func TestCreateSandboxWithResourcesUsesCustomEndpoint(t *testing.T) {
	var path string
	var body struct {
		Resources map[string]float64 `json:"resources"`
	}
	server := newIPv4Server(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
		}
		writeJSON(t, w, map[string]interface{}{"sandboxID": "sb-res", "templateID": "base", "envdVersion": "0.5.7"})
	}))

	_, err := CreateSandbox(context.Background(), &SandboxOpts{
		APIKey:    "k",
		APIURL:    server.URL,
		Template:  "base",
		Resources: &SandboxResourceTargets{CPU: 8, MemoryGiB: 2, DiskGiB: 4},
	})
	if err != nil {
		t.Fatalf("CreateSandbox failed: %v", err)
	}
	if path != "/sandboxes/custom" {
		t.Fatalf("path = %q, want /sandboxes/custom", path)
	}
	if body.Resources["vcpu"] != 8 || body.Resources["memory_mb"] != 2048 || body.Resources["disk_size_mb"] != 4096 {
		t.Fatalf("resources = %#v, want vcpu 8, memory_mb 2048, disk_size_mb 4096", body.Resources)
	}
}

func TestResourceCreateTimeoutUsesSixMinutesUnlessExplicit(t *testing.T) {
	t.Setenv("NOVITA_REQUEST_TIMEOUT", "")
	client, err := NewClient(&SandboxOpts{APIKey: "k", APIURL: "http://127.0.0.1:1"})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	timeout := client.resourceCreateTimeout()
	if timeout == nil || *timeout != ResourceCreateTimeoutMS {
		t.Fatalf("default resource create timeout = %v, want %d", timeout, ResourceCreateTimeoutMS)
	}

	explicit := 12000
	client, err = NewClient(&SandboxOpts{APIKey: "k", APIURL: "http://127.0.0.1:1", RequestTimeoutMs: &explicit})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	if timeout := client.resourceCreateTimeout(); timeout != nil {
		t.Fatalf("explicit resource create timeout = %v, want no override", *timeout)
	}
}

func TestCreateSandboxWithResourcesHonorsExplicitTimeout(t *testing.T) {
	server := newIPv4Server(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Slightly outlast the explicit timeout: the create request must fail
		// instead of silently using the six-minute resource default.
		time.Sleep(1200 * time.Millisecond)
		writeJSON(t, w, map[string]interface{}{"sandboxID": "sb-res", "envdVersion": "0.5.7"})
	}))

	timeout := 400
	_, err := CreateSandbox(context.Background(), &SandboxOpts{
		APIKey:           "k",
		APIURL:           server.URL,
		Template:         "base",
		RequestTimeoutMs: &timeout,
		Resources:        &SandboxResourceTargets{MemoryGiB: 2},
	})
	if err == nil {
		t.Fatal("CreateSandbox succeeded, want the explicit timeout to apply")
	}
}

func TestCreateSandboxWithEmptyResourcesRejectsBeforeRequest(t *testing.T) {
	client, err := NewClient(&SandboxOpts{APIKey: "k", APIURL: "http://127.0.0.1:1"})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	_, err = client.CreateSandbox(context.Background(), &SandboxOpts{
		Template:  "base",
		Resources: &SandboxResourceTargets{},
	})
	if err == nil {
		t.Fatal("CreateSandbox succeeded, want an error")
	}
	if !strings.Contains(err.Error(), "at least one") {
		t.Fatalf("error = %q, want it to mention at least one resource", err)
	}
}

func TestCreateSandboxWithResourcesRejectsLegacyDomain(t *testing.T) {
	t.Setenv("NOVITA_IS_LEGACY", "true")
	server := newIPv4Server(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
	}))
	_, err := CreateSandbox(context.Background(), &SandboxOpts{
		APIKey:    "k",
		APIURL:    server.URL,
		Template:  "base",
		Resources: &SandboxResourceTargets{CPU: 2},
	})
	if err == nil {
		t.Fatal("CreateSandbox succeeded, want an error")
	}
	if _, ok := err.(*UnsupportedError); !ok {
		t.Fatalf("error = %T, want *UnsupportedError", err)
	}
}

func TestResizeSandboxConvertsMiBAndReturnsGiB(t *testing.T) {
	var path, method string
	var body map[string]float64
	server := newIPv4Server(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		method = r.Method
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
		}
		writeJSON(t, w, map[string]interface{}{
			"cpu": map[string]interface{}{
				"status": "applied", "requested": 4, "previous": 2, "current": 4, "code": 200,
			},
			"memory": map[string]interface{}{
				"status": "applied", "requested": 2048, "previous": 1024, "current": 2048, "code": 200,
			},
			"disk": map[string]interface{}{
				"status": "applied", "requested": 4096, "previous": 2669, "current": 4096, "code": 200,
			},
		})
	}))

	client, err := NewClient(&SandboxOpts{APIKey: "k", APIURL: server.URL})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	result, err := client.ResizeSandbox(context.Background(), "sbx", SandboxResourceTargets{CPU: 4, MemoryGiB: 2, DiskGiB: 4})
	if err != nil {
		t.Fatalf("ResizeSandbox failed: %v", err)
	}
	if method != http.MethodPost || path != "/sandboxes/sbx/resize" {
		t.Fatalf("request = %s %s, want POST /sandboxes/sbx/resize", method, path)
	}
	if body["vcpu"] != 4 || body["memory_mb"] != 2048 || body["disk_size_mb"] != 4096 {
		t.Fatalf("body = %#v, want MiB values", body)
	}
	if result.CPU == nil || result.CPU.Unit != "cores" {
		t.Fatalf("cpu item = %#v, want cores", result.CPU)
	}
	assertOptionalFloat(t, "cpu current", result.CPU.Current, 4)
	if result.Memory == nil || result.Memory.Unit != "GiB" {
		t.Fatalf("memory item = %#v, want GiB", result.Memory)
	}
	assertOptionalFloat(t, "memory current", result.Memory.Current, 2)
	assertOptionalFloat(t, "memory previous", result.Memory.Previous, 1)
	if result.Disk == nil || result.Disk.Unit != "GiB" {
		t.Fatalf("disk item = %#v, want GiB", result.Disk)
	}
	assertOptionalFloat(t, "disk previous", result.Disk.Previous, 2669.0/1024.0)
}

func assertOptionalFloat(t *testing.T, label string, got *float64, want float64) {
	t.Helper()
	if got == nil {
		t.Fatalf("%s = nil, want %v", label, want)
	}
	if math.Abs(*got-want) > 1e-9 {
		t.Fatalf("%s = %v, want %v", label, *got, want)
	}
}

func TestResizeSandboxKeepsAbsentTotalsNil(t *testing.T) {
	server := newIPv4Server(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, map[string]interface{}{
			"memory": map[string]interface{}{
				"status": "unknown", "requested": 2048, "code": 207, "reason": "record_pending",
			},
			"disk": map[string]interface{}{
				"status": "unknown", "requested": 4096, "previous": nil, "current": nil, "code": 207,
			},
		})
	}))
	client, err := NewClient(&SandboxOpts{APIKey: "k", APIURL: server.URL})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	result, err := client.ResizeSandbox(context.Background(), "sbx", SandboxResourceTargets{MemoryGiB: 2, DiskGiB: 4})
	if err != nil {
		t.Fatalf("ResizeSandbox failed: %v", err)
	}
	assertOptionalFloat(t, "memory requested", result.Memory.Requested, 2)
	if result.Memory.Previous != nil || result.Memory.Current != nil {
		t.Fatalf("memory previous/current = %v/%v, want nil", result.Memory.Previous, result.Memory.Current)
	}
	if result.Disk.Previous != nil || result.Disk.Current != nil {
		t.Fatalf("disk previous/current = %v/%v, want nil", result.Disk.Previous, result.Disk.Current)
	}
}

func TestClientCapturesTimeoutProvenanceAtConstruction(t *testing.T) {
	t.Setenv("NOVITA_REQUEST_TIMEOUT", "1")
	client, err := NewClient(&SandboxOpts{APIKey: "k", APIURL: "http://127.0.0.1:1"})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	// Changing the environment after construction must not change this client.
	os.Unsetenv("NOVITA_REQUEST_TIMEOUT")
	if timeout := client.resourceCreateTimeout(); timeout != nil {
		t.Fatalf("resource create timeout = %v, want the captured one-second timeout", *timeout)
	}
	if client.Connection().RequestTimeoutMs != 1000 {
		t.Fatalf("connection timeout = %d, want the captured 1000ms", client.Connection().RequestTimeoutMs)
	}
	inherited := client.inheritOptions(&SandboxOpts{Template: "base"})
	if inherited.RequestTimeoutMs == nil || *inherited.RequestTimeoutMs != 1000 {
		t.Fatalf("inherited timeout = %v, want the captured 1000ms", inherited.RequestTimeoutMs)
	}
}

func TestInheritedOptionsKeepTimeoutSnapshotAcrossEnvironmentChanges(t *testing.T) {
	t.Setenv("NOVITA_REQUEST_TIMEOUT", "")
	base, err := NewClient(&SandboxOpts{APIKey: "k", APIURL: "http://127.0.0.1:1"})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	os.Setenv("NOVITA_REQUEST_TIMEOUT", "1")
	defer os.Unsetenv("NOVITA_REQUEST_TIMEOUT")

	inherited := base.inheritOptions(&SandboxOpts{Template: "base"})
	if inherited.RequestTimeoutMs == nil || *inherited.RequestTimeoutMs != RequestTimeoutMS {
		t.Fatalf("inherited timeout = %v, want the captured %dms", inherited.RequestTimeoutMs, RequestTimeoutMS)
	}
	if inherited.requestTimeoutExplicit == nil || *inherited.requestTimeoutExplicit {
		t.Fatalf("inherited provenance = %v, want the captured default", inherited.requestTimeoutExplicit)
	}
	derived, err := NewClient(inherited)
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	if derived.Connection().RequestTimeoutMs != RequestTimeoutMS {
		t.Fatalf("derived connection timeout = %d, want %d", derived.Connection().RequestTimeoutMs, RequestTimeoutMS)
	}
	if timeout := derived.resourceCreateTimeout(); timeout == nil || *timeout != ResourceCreateTimeoutMS {
		t.Fatalf("derived resource create timeout = %v, want %d", timeout, ResourceCreateTimeoutMS)
	}

	explicit := 2000
	base, err = NewClient(&SandboxOpts{APIKey: "k", APIURL: "http://127.0.0.1:1", RequestTimeoutMs: &explicit})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	inherited = base.inheritOptions(&SandboxOpts{Template: "base"})
	if inherited.RequestTimeoutMs == nil || *inherited.RequestTimeoutMs != explicit {
		t.Fatalf("inherited explicit timeout = %v, want %d", inherited.RequestTimeoutMs, explicit)
	}
}

func TestResizeSandboxRequiresAnOutcomeForEachRequestedResource(t *testing.T) {
	server := newIPv4Server(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, map[string]interface{}{})
	}))
	client, err := NewClient(&SandboxOpts{APIKey: "k", APIURL: server.URL})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	_, err = client.ResizeSandbox(context.Background(), "sbx", SandboxResourceTargets{CPU: 2, MemoryGiB: 2})
	if err == nil || !strings.Contains(err.Error(), "missing the cpu result") {
		t.Fatalf("error = %v, want a missing cpu result error", err)
	}

	server2 := newIPv4Server(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("null\n"))
	}))
	client, err = NewClient(&SandboxOpts{APIKey: "k", APIURL: server2.URL})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	_, err = client.ResizeSandbox(context.Background(), "sbx", SandboxResourceTargets{DiskGiB: 4})
	if err == nil || !strings.Contains(err.Error(), "missing the disk result") {
		t.Fatalf("error = %v, want a missing disk result error", err)
	}
}

func TestResizeSandboxWrapsMalformedResponses(t *testing.T) {
	server := newIPv4Server(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("not json\n"))
	}))
	client, err := NewClient(&SandboxOpts{APIKey: "k", APIURL: server.URL})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	_, err = client.ResizeSandbox(context.Background(), "sbx", SandboxResourceTargets{CPU: 2})
	if err == nil || !strings.Contains(err.Error(), "invalid resize response") {
		t.Fatalf("error = %v, want an invalid resize response error", err)
	}
}

func TestNovitaFacadeResourceCreateUsesCustomEndpoint(t *testing.T) {
	var path string
	server := newIPv4Server(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		writeJSON(t, w, map[string]interface{}{"sandboxID": "sb-facade", "templateID": "base", "envdVersion": "0.5.7"})
	}))
	novita, err := NewNovita(&SandboxOpts{APIKey: "k", APIURL: server.URL})
	if err != nil {
		t.Fatalf("NewNovita failed: %v", err)
	}
	sandbox, err := novita.Create(context.Background(), &SandboxOpts{
		Template:  "base",
		Resources: &SandboxResourceTargets{CPU: 2},
	})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	if sandbox.ID() != "sb-facade" {
		t.Fatalf("sandbox ID = %q, want sb-facade", sandbox.ID())
	}
	if path != "/sandboxes/custom" {
		t.Fatalf("path = %q, want /sandboxes/custom", path)
	}
}

func TestInheritedOptionsPreserveResourceCreateDefault(t *testing.T) {
	t.Setenv("NOVITA_REQUEST_TIMEOUT", "")
	client, err := NewClient(&SandboxOpts{APIKey: "k", APIURL: "http://127.0.0.1:1"})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	inherited := client.inheritOptions(&SandboxOpts{Template: "base", Resources: &SandboxResourceTargets{CPU: 2}})
	if inherited.RequestTimeoutMs == nil || *inherited.RequestTimeoutMs != RequestTimeoutMS {
		t.Fatalf("inherited timeout = %v, want the parent default %dms", inherited.RequestTimeoutMs, RequestTimeoutMS)
	}
	if inherited.requestTimeoutExplicit == nil || *inherited.requestTimeoutExplicit {
		t.Fatalf("inherited provenance = %v, want default", inherited.requestTimeoutExplicit)
	}
	if inherited.APIKey != "k" || inherited.Resources == nil || inherited.Resources.CPU != 2 {
		t.Fatalf("inherited options = %#v, want credentials and resources preserved", inherited)
	}
	inheritedClient, err := NewClient(inherited)
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	if timeout := inheritedClient.resourceCreateTimeout(); timeout == nil || *timeout != ResourceCreateTimeoutMS {
		t.Fatalf("inherited resource create timeout = %v, want %d", timeout, ResourceCreateTimeoutMS)
	}

	explicit := 12000
	client, err = NewClient(&SandboxOpts{APIKey: "k", APIURL: "http://127.0.0.1:1", RequestTimeoutMs: &explicit})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	inherited = client.inheritOptions(&SandboxOpts{Template: "base"})
	if inherited.RequestTimeoutMs == nil || *inherited.RequestTimeoutMs != explicit {
		t.Fatalf("inherited timeout = %v, want the explicit %d", inherited.RequestTimeoutMs, explicit)
	}
}

func TestResizeSandboxKeepsPartialResult(t *testing.T) {
	server := newIPv4Server(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMultiStatus)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"cpu": map[string]interface{}{
				"status": "applied", "requested": 6, "previous": 4, "current": 6, "code": 200,
			},
			"memory": map[string]interface{}{
				"status": "failed", "requested": 9217, "previous": 1024, "current": 1024,
				"code": 400, "reason": "exceeds_sandbox_limit",
				"message": "requested 9217 MiB memory exceeds the maximum",
			},
		})
	}))

	client, err := NewClient(&SandboxOpts{APIKey: "k", APIURL: server.URL})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	result, err := client.ResizeSandbox(context.Background(), "sbx", SandboxResourceTargets{CPU: 6, MemoryGiB: 9217.0 / 1024.0})
	if err != nil {
		t.Fatalf("ResizeSandbox failed: %v", err)
	}
	if result.CPU == nil || result.CPU.Status != "applied" {
		t.Fatalf("cpu item = %#v, want applied", result.CPU)
	}
	if result.Memory == nil || result.Memory.Status != "failed" || result.Memory.Code != 400 || result.Memory.Reason != "exceeds_sandbox_limit" {
		t.Fatalf("memory item = %#v, want failed with reason", result.Memory)
	}
	assertOptionalFloat(t, "memory current", result.Memory.Current, 1)
	if result.Disk != nil {
		t.Fatalf("disk item = %#v, want nil", result.Disk)
	}
}

func TestResizeSandboxRejectsInvalidTargetsBeforeRequest(t *testing.T) {
	requests := 0
	server := newIPv4Server(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
	}))
	client, err := NewClient(&SandboxOpts{APIKey: "k", APIURL: server.URL})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	if _, err := client.ResizeSandbox(context.Background(), "sbx", SandboxResourceTargets{}); err == nil {
		t.Fatal("ResizeSandbox succeeded, want an error")
	}
	if _, err := client.ResizeSandbox(context.Background(), "sbx", SandboxResourceTargets{MemoryGiB: 1.1}); err == nil {
		t.Fatal("ResizeSandbox succeeded, want an error")
	}
	if requests != 0 {
		t.Fatalf("requests = %d, want 0", requests)
	}
}

func TestResizeSandboxMapsErrors(t *testing.T) {
	cases := []struct {
		status  int
		check   func(error) bool
		name    string
		payload string
	}{
		{http.StatusNotFound, func(err error) bool { _, ok := err.(*SandboxNotFoundError); return ok }, "not found", `{"code":404,"message":"sandbox not found"}`},
		{http.StatusUnauthorized, func(err error) bool { _, ok := err.(*AuthenticationError); return ok }, "unauthorized", `{"code":401,"message":"unauthorized"}`},
		{http.StatusConflict, func(err error) bool {
			e, ok := err.(*SandboxError)
			return ok && e.StatusCode == http.StatusConflict
		}, "conflict", `{"code":409,"message":"another resize is in progress"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := newIPv4Server(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.payload))
			}))
			client, err := NewClient(&SandboxOpts{APIKey: "k", APIURL: server.URL})
			if err != nil {
				t.Fatalf("NewClient failed: %v", err)
			}
			_, err = client.ResizeSandbox(context.Background(), "sbx", SandboxResourceTargets{CPU: 2})
			if err == nil {
				t.Fatal("ResizeSandbox succeeded, want an error")
			}
			if !tc.check(err) {
				t.Fatalf("error = %T %v, want the mapped status", err, err)
			}
		})
	}
}

func TestResizeSandboxRejectsLegacyDomain(t *testing.T) {
	t.Setenv("NOVITA_IS_LEGACY", "true")
	server := newIPv4Server(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
	}))
	client, err := NewClient(&SandboxOpts{APIKey: "k", APIURL: server.URL})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	_, err = client.ResizeSandbox(context.Background(), "sbx", SandboxResourceTargets{CPU: 2})
	if err == nil {
		t.Fatal("ResizeSandbox succeeded, want an error")
	}
	if _, ok := err.(*UnsupportedError); !ok {
		t.Fatalf("error = %T, want *UnsupportedError", err)
	}
}
