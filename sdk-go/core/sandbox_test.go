package core

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestClientRequestContextLivesUntilResponseBodyClose(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		time.Sleep(25 * time.Millisecond)
		_, _ = io.WriteString(w, `{"sandboxID":"sb-delayed"}`)
	}))
	defer server.Close()

	timeout := 500
	client, err := NewClient(&SandboxOpts{APIKey: "k", APIURL: server.URL, RequestTimeoutMs: &timeout})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	info, err := client.GetSandboxInfo(context.Background(), "sb-delayed")
	if err != nil {
		t.Fatalf("reading delayed response body failed: %v", err)
	}
	if info.SandboxID != "sb-delayed" {
		t.Fatalf("SandboxID = %q, want sb-delayed", info.SandboxID)
	}
}

func TestCreateSandboxDebugBypassesControlPlane(t *testing.T) {
	debug := true
	sandbox, err := CreateSandbox(context.Background(), &SandboxOpts{Debug: &debug})
	if err != nil {
		t.Fatalf("CreateSandbox debug failed: %v", err)
	}
	if sandbox.ID() != "debug_sandbox_id" {
		t.Fatalf("ID = %q", sandbox.ID())
	}
	if sandbox.EnvdVersion() != EnvdDebugFallback {
		t.Fatalf("EnvdVersion = %q", sandbox.EnvdVersion())
	}
	if sandbox.EnvdURL() != "http://localhost:49983" {
		t.Fatalf("EnvdURL = %q", sandbox.EnvdURL())
	}
	if _, err := sandbox.Kill(context.Background()); err != nil {
		t.Fatalf("debug Kill = %v", err)
	}
	if err := sandbox.client.SetSandboxTimeout(context.Background(), sandbox.ID(), 1000); err != nil {
		t.Fatalf("debug SetTimeout = %v", err)
	}
	metrics, err := sandbox.client.GetSandboxMetrics(context.Background(), sandbox.ID(), "")
	if err != nil || len(metrics) != 0 {
		t.Fatalf("debug metrics = %#v, %v", metrics, err)
	}
}

func TestCreateSandboxUsesDefaultTemplateAndTimeout(t *testing.T) {
	var payload map[string]interface{}
	server := newIPv4Server(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		writeJSON(t, w, map[string]interface{}{"sandboxID": "sb", "templateID": "base", "envdVersion": "0.5.7"})
	}))
	_, err := CreateSandbox(context.Background(), &SandboxOpts{APIKey: "k", APIURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	if payload["templateID"] != "base" || payload["timeout"] != float64(300) {
		t.Fatalf("payload = %#v", payload)
	}
}

func TestClientCreateSandbox(t *testing.T) {
	var gotPayload map[string]interface{}
	var gotAPIKey, gotSDKName string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/sandboxes" {
			t.Errorf("path = %q, want /sandboxes", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("method = %q, want POST", r.Method)
		}

		gotAPIKey = r.Header.Get("X-API-Key")
		gotSDKName = r.Header.Get("X-Novita-SDK-Name")

		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotPayload)

		writeJSON(t, w, map[string]interface{}{
			"sandboxID":   "sb-1",
			"templateID":  "base",
			"state":       "running",
			"envdVersion": "0.5.7",
			"startedAt":   "2026-01-01T00:00:00Z",
			"endAt":       "2026-01-01T00:05:00Z",
			"cpuCount":    2,
			"memoryMB":    512,
			"diskSizeMB":  1024,
			"clientID":    "c-1",
		})
	}))
	defer server.Close()

	client, err := NewClient(&SandboxOpts{APIKey: "secret", APIURL: server.URL})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}

	allowInternet := false
	info, err := client.CreateSandbox(context.Background(), &SandboxOpts{
		Template:            "base",
		Timeout:             300,
		EnvVars:             map[string]string{"FOO": "bar"},
		AllowInternetAccess: &allowInternet,
	})
	if err != nil {
		t.Fatalf("CreateSandbox failed: %v", err)
	}

	if info.SandboxID != "sb-1" {
		t.Errorf("SandboxID = %q, want sb-1", info.SandboxID)
	}
	if info.EnvdVersion != "0.5.7" {
		t.Errorf("EnvdVersion = %q, want 0.5.7", info.EnvdVersion)
	}
	if info.CPUCount != 2 || info.MemoryMB != 512 {
		t.Errorf("resources = %d cpu / %d MB, want 2 / 512", info.CPUCount, info.MemoryMB)
	}

	if gotAPIKey != "secret" {
		t.Errorf("X-API-Key = %q, want secret", gotAPIKey)
	}
	if gotSDKName != SDKName {
		t.Errorf("X-Novita-SDK-Name = %q, want %q", gotSDKName, SDKName)
	}
	if gotPayload["templateID"] != "base" {
		t.Errorf("payload templateID = %v, want base", gotPayload["templateID"])
	}
	if gotPayload["timeout"] != float64(300) {
		t.Errorf("payload timeout = %v, want 300", gotPayload["timeout"])
	}
	if gotPayload["allow_internet_access"] != false {
		t.Errorf("payload allow_internet_access = %v, want false", gotPayload["allow_internet_access"])
	}
}

func TestClientCreateSandboxRequiresTemplateOrImage(t *testing.T) {
	client, err := NewClient(&SandboxOpts{APIKey: "k", APIURL: "https://example.invalid"})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}

	if _, err := client.CreateSandbox(context.Background(), &SandboxOpts{}); err == nil {
		t.Fatal("expected an error when neither template nor image is set")
	}

	// Both set is also an error.
	_, err = client.CreateSandbox(context.Background(), &SandboxOpts{Template: "t", Image: "i"})
	if err == nil {
		t.Fatal("expected an error when both template and image are set")
	}
}

func TestClientCreateSandboxMapsLifecycle(t *testing.T) {
	var got map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decode body: %v", err)
		}
		writeJSON(t, w, map[string]interface{}{"sandboxID": "sb", "envdVersion": "0.5.7"})
	}))
	defer server.Close()
	client, err := NewClient(&SandboxOpts{APIKey: "k", APIURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.CreateSandbox(context.Background(), &SandboxOpts{
		Template:  "base",
		Lifecycle: &SandboxLifecycleConfig{OnTimeout: OnTimeoutPause, AutoResume: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got["autoPause"] != true {
		t.Errorf("autoPause = %v, want true", got["autoPause"])
	}
	autoResume, ok := got["autoResume"].(map[string]interface{})
	if !ok || autoResume["enabled"] != true {
		t.Errorf("autoResume = %#v, want enabled", got["autoResume"])
	}
}

func TestClientLegacyCreateCapabilityGates(t *testing.T) {
	client, err := NewClient(&SandboxOpts{APIKey: "k", Domain: "sandbox.novita.ai"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.CreateSandbox(context.Background(), &SandboxOpts{Template: "base", Network: &SandboxNetworkConfig{AllowOut: []string{"1.1.1.1"}}})
	if _, ok := err.(*UnsupportedError); !ok {
		t.Fatalf("error = %T, want *UnsupportedError", err)
	}
}

func TestCreateSandboxRejectsOutdatedEnvdAndKillsSandbox(t *testing.T) {
	killed := ""
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			killed = r.URL.Path
			w.WriteHeader(http.StatusNoContent)
			return
		}
		writeJSON(t, w, map[string]interface{}{
			"sandboxID": "old-sandbox", "templateID": "old-template", "envdVersion": "0.0.9",
		})
	}))
	defer server.Close()
	_, err := CreateSandbox(context.Background(), &SandboxOpts{APIKey: "k", APIURL: server.URL, Template: "old-template"})
	if _, ok := err.(*TemplateError); !ok {
		t.Fatalf("error = %T, want *TemplateError", err)
	}
	if killed != "/sandboxes/old-sandbox" {
		t.Fatalf("killed path = %q", killed)
	}
}

func TestLegacySandboxUsesCompositeRuntimeID(t *testing.T) {
	cfg, err := NewConnectionConfig(&SandboxOpts{APIKey: "k", Domain: "sandbox.novita.ai"})
	if err != nil {
		t.Fatal(err)
	}
	sandbox, err := NewSandboxFromConfig(cfg, &SandboxInfo{SandboxID: "sandbox", ClientID: "client", EnvdVersion: "0.5.7"})
	if err != nil {
		t.Fatal(err)
	}
	if sandbox.ID() != "sandbox-client" {
		t.Fatalf("ID = %q, want sandbox-client", sandbox.ID())
	}
	if sandbox.SandboxDomain() != "sandbox.novita.ai" {
		t.Fatalf("SandboxDomain = %q", sandbox.SandboxDomain())
	}
}

func TestSandboxMetricsDecodeModernAndLegacy(t *testing.T) {
	var modern SandboxMetrics
	if err := json.Unmarshal([]byte(`{"timestamp":"2026-09-14T00:00:00Z","timestampUnix":1789344000,"cpuUsedPct":12.5,"cpuCount":2,"memUsed":10,"memTotal":20,"diskUsed":30,"diskTotal":40}`), &modern); err != nil {
		t.Fatal(err)
	}
	if modern.CPUUsedPct != 12.5 || modern.MemUsed != 10 || modern.DiskTotal != 40 || modern.Timestamp.IsZero() {
		t.Fatalf("modern = %#v", modern)
	}
	var legacy SandboxMetrics
	if err := json.Unmarshal([]byte(`{"timestamp":1789344000000,"cpuUsedPct":7,"cpuCount":1,"memUsedMiB":128,"memTotalMiB":512}`), &legacy); err != nil {
		t.Fatal(err)
	}
	if legacy.MemUsed != 128*1024*1024 || legacy.MemTotal != 512*1024*1024 || legacy.DiskUsed != -1 || legacy.DiskTotal != -1 {
		t.Fatalf("legacy = %#v", legacy)
	}
}

func TestLegacyMetricsForceDiskMinusOneAndNoQueryParams(t *testing.T) {
	var gotQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		// A legacy response that unexpectedly carries disk figures: JS still
		// reports -1 for both (sandboxApi.ts getMetrics legacy branch).
		_ = json.NewEncoder(w).Encode([]map[string]any{{
			"timestamp": 1789344000000, "cpuUsedPct": 7, "cpuCount": 1,
			"memUsedMiB": 128, "memTotalMiB": 512, "diskUsed": 30, "diskTotal": 40,
		}})
	}))
	defer server.Close()

	client, err := NewClient(&SandboxOpts{APIKey: "k", APIURL: server.URL, Domain: "sandbox.novita.ai"})
	if err != nil {
		t.Fatal(err)
	}
	ns := &SandboxNamespace{client: client}
	start := time.Unix(1789344000, 0)
	metrics, err := ns.MetricsWithOptions(context.Background(), "sb-1", SandboxMetricsOptions{Start: &start})
	if err != nil {
		t.Fatal(err)
	}
	// JS legacy metrics sends no start/end query parameters at all.
	if gotQuery != "" {
		t.Errorf("legacy metrics query = %q, want empty", gotQuery)
	}
	if len(metrics) != 1 || metrics[0].DiskUsed != -1 || metrics[0].DiskTotal != -1 {
		t.Fatalf("legacy metrics = %#v, want disk -1/-1", metrics)
	}
	if metrics[0].MemUsed != 128*1024*1024 {
		t.Fatalf("legacy memUsed = %d, want %d", metrics[0].MemUsed, 128*1024*1024)
	}
}

func TestSandboxEventsDecodeProtocolShape(t *testing.T) {
	server := newIPv4Server(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(t, w, map[string]interface{}{
			"items": []map[string]interface{}{{"eventID": "evt", "recordAt": 123, "templateID": "tpl", "templateName": "base", "sandboxID": "sb", "eventName": "create", "state": "success", "errorMsg": "", "statusCode": 200}},
			"total": 1, "hasMore": true,
		})
	}))
	client, _ := NewClient(&SandboxOpts{APIKey: "k", APIURL: server.URL})
	result, err := client.GetSandboxEvents(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Items) != 1 || result.Items[0].EventID != "evt" || result.Total != 1 || !result.HasMore {
		t.Fatalf("result = %#v", result)
	}
}

func TestClientKillSandbox(t *testing.T) {
	var gotMethod, gotPath string
	var calls int

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		// Second kill: the sandbox is already gone.
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client, err := NewClient(&SandboxOpts{APIKey: "k", APIURL: server.URL})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}

	// 2xx reports true; 404 reports false like JS/Python kill().
	killed, err := client.KillSandbox(context.Background(), "sb-1")
	if err != nil {
		t.Fatalf("KillSandbox failed: %v", err)
	}
	if !killed {
		t.Error("KillSandbox = false, want true on 2xx")
	}
	if gotMethod != http.MethodDelete {
		t.Errorf("method = %q, want DELETE", gotMethod)
	}
	if gotPath != "/sandboxes/sb-1" {
		t.Errorf("path = %q, want /sandboxes/sb-1", gotPath)
	}

	missing, err := client.KillSandbox(context.Background(), "sb-gone")
	if err != nil {
		t.Fatalf("KillSandbox(404) failed: %v", err)
	}
	if missing {
		t.Error("KillSandbox(404) = true, want false")
	}
}

func TestClientPauseSandboxStatusSemantics(t *testing.T) {
	var calls int
	var gotBody []byte

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		calls++
		if calls == 1 {
			// Already paused.
			w.WriteHeader(http.StatusConflict)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client, err := NewClient(&SandboxOpts{APIKey: "k", APIURL: server.URL})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}

	// 409 reports false (already paused), matching JS/Python pause().
	paused, err := client.PauseSandbox(context.Background(), "sb-1")
	if err != nil {
		t.Fatalf("PauseSandbox failed: %v", err)
	}
	if paused {
		t.Error("PauseSandbox(409) = true, want false")
	}
	// Modern pause POSTs with no request body, matching JS.
	if len(gotBody) != 0 {
		t.Errorf("pause body = %q, want empty on modern clusters", gotBody)
	}

	ok, err := client.PauseSandbox(context.Background(), "sb-1")
	if err != nil || !ok {
		t.Fatalf("PauseSandbox = %v, %v; want true, nil", ok, err)
	}
}

func TestClientLegacyPauseSendsSyncFalse(t *testing.T) {
	var gotBody []byte

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client, err := NewClient(&SandboxOpts{APIKey: "k", APIURL: server.URL, Domain: "sandbox.novita.ai"})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}

	if _, err := client.PauseSandbox(context.Background(), "sb-1"); err != nil {
		t.Fatalf("PauseSandbox failed: %v", err)
	}
	// Legacy clusters send {"sync": false} (JS sandboxApi.ts pause legacy branch).
	if string(gotBody) != `{"sync":false}` {
		t.Errorf("legacy pause body = %q, want {\"sync\":false}", gotBody)
	}
}

func TestClientResetSandboxStatusSemantics(t *testing.T) {
	var gotBody []byte

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		if r.URL.Path == "/sandboxes/sb-busy/reset" {
			w.WriteHeader(http.StatusConflict)
			return
		}
		// Success with an empty body: JS/Python report true for any 2xx.
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client, err := NewClient(&SandboxOpts{APIKey: "k", APIURL: server.URL})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}

	// 409 reports false (cannot reset now); the body always carries resume
	// (defaulting to true), matching JS/Python resetSandbox.
	reset, err := client.ResetSandbox(context.Background(), "sb-busy", map[string]interface{}{})
	if err != nil || reset {
		t.Fatalf("ResetSandbox(409) = %v, %v; want false, nil", reset, err)
	}
	var firstBody map[string]any
	if err := json.Unmarshal(gotBody, &firstBody); err != nil {
		t.Fatal(err)
	}
	if resume, ok := firstBody["resume"].(bool); !ok || !resume {
		t.Errorf("reset body resume = %v, want true (defaulted)", firstBody["resume"])
	}

	reset, err = client.ResetSandbox(context.Background(), "sb-1", map[string]interface{}{"resume": true})
	if err != nil || !reset {
		t.Fatalf("ResetSandbox = %v, %v; want true, nil", reset, err)
	}
	var payload map[string]any
	if err := json.Unmarshal(gotBody, &payload); err != nil {
		t.Fatalf("reset body not JSON: %v", err)
	}
	if resume, ok := payload["resume"].(bool); !ok || !resume {
		t.Errorf("reset body resume = %v, want true", payload["resume"])
	}
}

func TestClientSetSandboxNetworkPutsAllowAndDenyOnly(t *testing.T) {
	var gotMethod string
	var gotBody []byte

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client, err := NewClient(&SandboxOpts{APIKey: "k", APIURL: server.URL})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}

	err = client.SetSandboxNetwork(context.Background(), "sb-1", &SandboxNetworkConfig{
		AllowPublicTraffic: true,
		AllowOut:           []string{"api.example.com"},
		DenyOut:            []string{"evil.example.com"},
		MaskRequestHost:    "masked",
	})
	if err != nil {
		t.Fatalf("SetSandboxNetwork failed: %v", err)
	}
	// JS/Python PUT /sandboxes/{id}/network with only allowOut/denyOut in the
	// body (SandboxNetworkUpdateOpts); the other fields are create-only.
	if gotMethod != http.MethodPut {
		t.Errorf("method = %q, want PUT", gotMethod)
	}
	var body map[string]any
	if err := json.Unmarshal(gotBody, &body); err != nil {
		t.Fatalf("network body not JSON: %v", err)
	}
	if _, ok := body["allowPublicTraffic"]; ok {
		t.Error("network body must not carry allowPublicTraffic")
	}
	if _, ok := body["maskRequestHost"]; ok {
		t.Error("network body must not carry maskRequestHost")
	}
	if allow, _ := body["allowOut"].([]any); len(allow) != 1 || allow[0] != "api.example.com" {
		t.Errorf("allowOut = %v, want [api.example.com]", body["allowOut"])
	}
	if deny, _ := body["denyOut"].([]any); len(deny) != 1 || deny[0] != "evil.example.com" {
		t.Errorf("denyOut = %v, want [evil.example.com]", body["denyOut"])
	}

	// An explicitly set empty list is sent as [] (JS [] is truthy; Python
	// checks is not None), while an unset field is omitted.
	err = client.SetSandboxNetwork(context.Background(), "sb-1", &SandboxNetworkConfig{
		AllowOut: []string{},
	})
	if err != nil {
		t.Fatalf("SetSandboxNetwork failed: %v", err)
	}
	body = map[string]any{}
	if err := json.Unmarshal(gotBody, &body); err != nil {
		t.Fatal(err)
	}
	if allow, ok := body["allowOut"].([]any); !ok || len(allow) != 0 {
		t.Errorf("allowOut = %#v, want empty array", body["allowOut"])
	}
	if _, ok := body["denyOut"]; ok {
		t.Error("unset denyOut must be omitted")
	}
}

func TestSandboxGetMcpURL(t *testing.T) {
	sandbox := &Sandbox{
		sandboxID:     "sb-1",
		sandboxDomain: "us-phx-1.sandbox.novita.ai",
		connection:    &ConnectionConfig{Domain: "us-phx-1.sandbox.novita.ai"},
	}

	// JS getMcpUrl(): https://{50005}-{id}.{domain}/mcp.
	url, err := sandbox.GetMcpURL()
	if err != nil {
		t.Fatalf("GetMcpURL failed: %v", err)
	}
	want := "https://50005-sb-1.us-phx-1.sandbox.novita.ai/mcp"
	if url != want {
		t.Errorf("GetMcpURL = %q, want %q", url, want)
	}

	// Legacy domains reject the call, matching JS raiseIfLegacy.
	sandbox.connection = &ConnectionConfig{Domain: "sandbox.novita.ai", IsLegacyDomain: true}
	if _, err := sandbox.GetMcpURL(); err == nil {
		t.Error("GetMcpURL on legacy domain should fail")
	}
}

func TestSandboxIsRunning(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/health" {
			t.Errorf("path = %q, want /health", r.URL.Path)
		}
		if calls == 1 {
			// The gateway answers 502 while the sandbox is down.
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	sandbox := &Sandbox{
		rest: newRESTClient(server.URL, nil),
	}

	running, err := sandbox.IsRunning(context.Background())
	if err != nil || running {
		t.Fatalf("IsRunning(502) = %v, %v; want false, nil", running, err)
	}
	running, err = sandbox.IsRunning(context.Background())
	if err != nil || !running {
		t.Fatalf("IsRunning(200) = %v, %v; want true, nil", running, err)
	}
}

func TestSandboxUploadDownloadURLSignature(t *testing.T) {
	expiration := 300
	// envdVersion 0.3.0 < 0.4.0, so the default user "user" applies (JS
	// uploadUrl() resolves the username this way for old envd builds).
	sandbox := &Sandbox{
		sandboxID:       "sb-1",
		sandboxDomain:   "us-phx-1.sandbox.novita.ai",
		envdVersion:     "0.3.0",
		envdAccessToken: "token-1",
		connection:      &ConnectionConfig{Domain: "us-phx-1.sandbox.novita.ai"},
		rest:            newRESTClient("https://49983-sb-1.us-phx-1.sandbox.novita.ai", nil),
	}

	// Secured sandbox: the URL carries username, path, signature and its
	// expiration, matching JS uploadUrl().
	upload, err := sandbox.UploadURL("/tmp/data.bin", &SandboxURLOpts{UseSignatureExpiration: &expiration})
	if err != nil {
		t.Fatalf("UploadURL failed: %v", err)
	}
	u, err := url.Parse(upload)
	if err != nil {
		t.Fatalf("UploadURL not a URL: %v", err)
	}
	if u.Path != "/files" {
		t.Errorf("path = %q, want /files", u.Path)
	}
	query := u.Query()
	if query.Get("username") != "user" {
		t.Errorf("username = %q, want user (envd < 0.4.0 default)", query.Get("username"))
	}
	if query.Get("path") != "/tmp/data.bin" {
		t.Errorf("file path = %q, want /tmp/data.bin", query.Get("path"))
	}
	if !strings.HasPrefix(query.Get("signature"), "v1_") {
		t.Errorf("signature = %q, want v1_ prefix", query.Get("signature"))
	}
	if strings.HasSuffix(query.Get("signature"), "=") {
		t.Error("signature must have trailing '=' stripped")
	}
	if query.Get("signature_expiration") == "" {
		t.Error("signature_expiration missing")
	}

	// An explicit user overrides the default and changes the signature.
	root := "root"
	explicit, err := sandbox.UploadURL("/tmp/data.bin", &SandboxURLOpts{User: &root})
	if err != nil {
		t.Fatalf("UploadURL failed: %v", err)
	}
	explicitQuery := urlMustParse(t, explicit).Query()
	if explicitQuery.Get("username") != "root" {
		t.Errorf("username = %q, want root", explicitQuery.Get("username"))
	}
	if explicitQuery.Get("signature") == query.Get("signature") {
		t.Error("explicit user must produce a different signature")
	}

	// An explicit empty user is NOT replaced by the envd<0.4.0 default
	// (JS checks user == undefined, not falsiness); the signature uses ''.
	empty := ""
	emptyUser, err := sandbox.UploadURL("/tmp/data.bin", &SandboxURLOpts{User: &empty})
	if err != nil {
		t.Fatalf("UploadURL failed: %v", err)
	}
	emptyQuery := urlMustParse(t, emptyUser).Query()
	if emptyQuery.Get("username") != "" {
		t.Errorf("username = %q, want empty (explicit)", emptyQuery.Get("username"))
	}
	if emptyQuery.Get("signature") == query.Get("signature") {
		t.Error("explicit empty user must produce a different signature than the default")
	}

	// A zero expiration is treated as unset (JS truthiness), producing no
	// signature_expiration parameter.
	zeroExpiration := 0
	zeroExp, err := sandbox.UploadURL("/tmp/data.bin", &SandboxURLOpts{UseSignatureExpiration: &zeroExpiration})
	if err != nil {
		t.Fatalf("UploadURL failed: %v", err)
	}
	if zq := urlMustParse(t, zeroExp).Query(); zq.Get("signature_expiration") != "" {
		t.Errorf("signature_expiration = %q, want empty for zero expiration", zq.Get("signature_expiration"))
	}

	// Download uses the read operation; the signature must differ.
	download, err := sandbox.DownloadURL("/tmp/data.bin", nil)
	if err != nil {
		t.Fatalf("DownloadURL failed: %v", err)
	}
	dlQuery := urlMustParse(t, download).Query()
	if dlQuery.Get("signature") == query.Get("signature") {
		t.Error("download signature must use the read operation and differ from upload")
	}

	// Plain sandbox (no envd access token, modern envd): no signature and no
	// default username — JS only assumes "user" on envd < 0.4.0.
	plain := &Sandbox{
		sandboxID:     "sb-2",
		sandboxDomain: "us-phx-1.sandbox.novita.ai",
		envdVersion:   "0.5.7",
		connection:    &ConnectionConfig{Domain: "us-phx-1.sandbox.novita.ai"},
		rest:          newRESTClient("https://49983-sb-2.us-phx-1.sandbox.novita.ai", nil),
	}
	plainURL, err := plain.DownloadURL("/tmp/a.txt", nil)
	if err != nil {
		t.Fatalf("DownloadURL failed: %v", err)
	}
	pq := urlMustParse(t, plainURL).Query()
	if pq.Get("signature") != "" {
		t.Error("unsecured sandbox URL must not carry a signature")
	}
	if pq.Get("username") != "" {
		t.Errorf("username = %q, want empty on envd >= 0.4.0", pq.Get("username"))
	}

	// Signature expiration requires a secured sandbox, matching JS.
	if _, err := plain.UploadURL("/tmp/a.txt", &SandboxURLOpts{UseSignatureExpiration: &expiration}); err == nil {
		t.Error("signature expiration on unsecured sandbox should fail")
	}

	// Zero counts as defined for the secured check (JS != undefined), even
	// though it is ignored later as an expiration value.
	zero := 0
	if _, err := plain.UploadURL("/tmp/a.txt", &SandboxURLOpts{UseSignatureExpiration: &zero}); err == nil {
		t.Error("zero signature expiration on unsecured sandbox should fail")
	}
}

func urlMustParse(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("invalid URL %q: %v", raw, err)
	}
	return u
}

func TestClientErrorMapping(t *testing.T) {
	cases := []struct {
		status  int
		wantErr interface{}
	}{
		{http.StatusNotFound, &SandboxNotFoundError{}},
		{http.StatusUnauthorized, &AuthenticationError{}},
		{http.StatusBadRequest, &InvalidArgumentError{}},
		{http.StatusTooManyRequests, &RateLimitError{}},
		{http.StatusGatewayTimeout, &TimeoutError{}},
		{http.StatusInsufficientStorage, &NotEnoughSpaceError{}},
	}

	for _, tc := range cases {
		t.Run(http.StatusText(tc.status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_ = json.NewEncoder(w).Encode(map[string]string{"message": "boom"})
			}))
			defer server.Close()

			client, err := NewClient(&SandboxOpts{APIKey: "k", APIURL: server.URL})
			if err != nil {
				t.Fatalf("NewClient failed: %v", err)
			}

			_, err = client.GetSandboxInfo(context.Background(), "sb-1")
			if err == nil {
				t.Fatal("expected an error")
			}

			switch tc.wantErr.(type) {
			case *SandboxNotFoundError:
				if _, ok := err.(*SandboxNotFoundError); !ok {
					t.Errorf("error = %T, want *SandboxNotFoundError", err)
				}
			case *AuthenticationError:
				if _, ok := err.(*AuthenticationError); !ok {
					t.Errorf("error = %T, want *AuthenticationError", err)
				}
			case *InvalidArgumentError:
				if _, ok := err.(*InvalidArgumentError); !ok {
					t.Errorf("error = %T, want *InvalidArgumentError", err)
				}
			case *RateLimitError:
				if _, ok := err.(*RateLimitError); !ok {
					t.Errorf("error = %T, want *RateLimitError", err)
				}
			case *TimeoutError:
				if _, ok := err.(*TimeoutError); !ok {
					t.Errorf("error = %T, want *TimeoutError", err)
				}
			case *NotEnoughSpaceError:
				if _, ok := err.(*NotEnoughSpaceError); !ok {
					t.Errorf("error = %T, want *NotEnoughSpaceError", err)
				}
			}
		})
	}
}

func TestClientSendsAccessTokenWhenSet(t *testing.T) {
	var gotAuth string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		writeJSON(t, w, map[string]interface{}{"sandboxID": "sb"})
	}))
	defer server.Close()

	client, err := NewClient(&SandboxOpts{
		APIKey:      "k",
		AccessToken: "tok",
		APIURL:      server.URL,
	})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}

	if _, err := client.GetSandboxInfo(context.Background(), "sb"); err != nil {
		t.Fatalf("GetSandboxInfo failed: %v", err)
	}
	if gotAuth != "Bearer tok" {
		t.Errorf("Authorization = %q, want Bearer tok", gotAuth)
	}
}

func TestEnvelopeFrameRoundTrip(t *testing.T) {
	payload := []byte(`{"hello":"world"}`)
	frame := envelopeFrame(payload)

	if frame[0] != 0 {
		t.Errorf("flag byte = %d, want 0", frame[0])
	}
	if got := binary.BigEndian.Uint32(frame[1:5]); got != uint32(len(payload)) {
		t.Errorf("length = %d, want %d", got, len(payload))
	}

	reader := bytes.NewReader(frame)
	got, err := readEnvelope(bufio.NewReader(reader))
	if err != nil {
		t.Fatalf("readEnvelope failed: %v", err)
	}
	if string(got) != string(payload) {
		t.Errorf("payload = %q, want %q", string(got), string(payload))
	}
}

func TestReadEnvelopeEndOfStream(t *testing.T) {
	// End-of-stream envelope: flag 0x02 with no payload.
	frame := []byte{0x02, 0, 0, 0, 0}

	_, err := readEnvelope(bufio.NewReader(bytes.NewReader(frame)))
	if err != io.EOF {
		t.Errorf("error = %v, want io.EOF", err)
	}
}

func TestReadEnvelopeEndOfStreamWithError(t *testing.T) {
	payload, _ := json.Marshal(map[string]string{"code": "not_found", "message": "gone"})

	frame := make([]byte, 5+len(payload))
	frame[0] = 0x02
	binary.BigEndian.PutUint32(frame[1:5], uint32(len(payload)))
	copy(frame[5:], payload)

	_, err := readEnvelope(bufio.NewReader(bytes.NewReader(frame)))
	rpc, ok := err.(*rpcError)
	if !ok {
		t.Fatalf("error = %T, want *rpcError", err)
	}
	if rpc.Code != CodeNotFound {
		t.Errorf("code = %q, want not_found", rpc.Code)
	}
}

func TestStatusToCode(t *testing.T) {
	cases := map[int]string{
		http.StatusBadRequest:          CodeInvalidArgument,
		http.StatusUnauthorized:        CodeUnauthenticated,
		http.StatusForbidden:           CodePermissionDenied,
		http.StatusNotFound:            CodeNotFound,
		http.StatusConflict:            CodeAlreadyExists,
		http.StatusTooManyRequests:     CodeResourceExhausted,
		http.StatusServiceUnavailable:  CodeUnavailable,
		http.StatusInternalServerError: CodeUnknown,
	}

	for status, want := range cases {
		if got := statusToCode(status); got != want {
			t.Errorf("statusToCode(%d) = %q, want %q", status, got, want)
		}
	}
}

func TestConnectSandboxPayload(t *testing.T) {
	var gotPayload map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/sandboxes/sb-1/connect" {
			t.Errorf("path = %q, want /sandboxes/sb-1/connect", r.URL.Path)
		}

		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotPayload)

		writeJSON(t, w, map[string]interface{}{"sandboxID": "sb-1", "envdVersion": "0.5.7"})
	}))
	defer server.Close()

	client, err := NewClient(&SandboxOpts{APIKey: "k", APIURL: server.URL})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}

	if _, err := client.ConnectSandbox(context.Background(), "sb-1", &SandboxOpts{Timeout: 120}); err != nil {
		t.Fatalf("ConnectSandbox failed: %v", err)
	}

	if gotPayload["timeout"] != float64(120) {
		t.Errorf("timeout = %v, want 120", gotPayload["timeout"])
	}
}
