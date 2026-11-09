package core

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

func newIPv4Server(t *testing.T, handler http.Handler) *httptest.Server {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	server := httptest.NewUnstartedServer(handler)
	server.Listener = listener
	server.Start()
	t.Cleanup(server.Close)
	return server
}

func TestFileHashMatchesPythonAlgorithm(t *testing.T) {
	contextPath := t.TempDir()
	path := filepath.Join(contextPath, "app.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	stat := info.Sys().(*syscall.Stat_t)
	h := sha256.New()
	io.WriteString(h, "COPY app.txt /app/app.txt")
	io.WriteString(h, "app.txt")
	io.WriteString(h, fmt.Sprint(stat.Mode))
	io.WriteString(h, fmt.Sprint(info.Size()))
	io.WriteString(h, "hello")
	want := fmt.Sprintf("%x", h.Sum(nil))

	got, err := fileHash("app.txt", "/app/app.txt", contextPath, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("hash = %s, want Python-compatible %s", got, want)
	}
}

func TestCollectTemplateFilesUsesDockerignoreAndStableOrder(t *testing.T) {
	contextPath := t.TempDir()
	for name, content := range map[string]string{
		"src/b.txt":       "b",
		"src/a.txt":       "a",
		"src/private.txt": "private",
		"src/keep.log":    "keep",
		".dockerignore":   "src/private.txt\n*.log\n!src/keep.log\n",
	} {
		path := filepath.Join(contextPath, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	patterns, err := templateIgnorePatterns(contextPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	paths, err := collectTemplateFiles("src", contextPath, patterns)
	if err != nil {
		t.Fatal(err)
	}
	var relative []string
	for _, path := range paths {
		rel, _ := filepath.Rel(contextPath, path)
		relative = append(relative, filepath.ToSlash(rel))
	}
	want := []string{"src", "src/a.txt", "src/b.txt", "src/keep.log"}
	if !reflect.DeepEqual(relative, want) {
		t.Fatalf("files = %#v, want %#v", relative, want)
	}
}

func TestTemplateBuildProtocol(t *testing.T) {
	contextPath := t.TempDir()
	if err := os.WriteFile(filepath.Join(contextPath, "main.py"), []byte("print('ok')"), 0o755); err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	var calls []string
	var trigger map[string]any
	var createBuild map[string]any
	var pollQueries []string
	var archiveNames []string
	polls := 0
	var server *httptest.Server
	server = newIPv4Server(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.EscapedPath()
		mu.Lock()
		calls = append(calls, r.Method+" "+path)
		mu.Unlock()
		switch {
		case r.Method == http.MethodPost && path == "/v3/templates":
			_ = json.NewDecoder(r.Body).Decode(&createBuild)
			_ = json.NewEncoder(w).Encode(map[string]any{"templateID": "template/1", "buildID": "build 1"})
		case r.Method == http.MethodGet && strings.HasPrefix(path, "/templates/template%2F1/files/"):
			_ = json.NewEncoder(w).Encode(map[string]any{"url": server.URL + "/upload", "present": false})
		case r.Method == http.MethodPut && path == "/upload":
			zr, err := gzip.NewReader(r.Body)
			if err != nil {
				t.Errorf("upload is not gzip: %v", err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			tr := tar.NewReader(zr)
			for {
				header, err := tr.Next()
				if err == io.EOF {
					break
				}
				if err != nil {
					t.Errorf("tar: %v", err)
					break
				}
				archiveNames = append(archiveNames, header.Name)
			}
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPost && path == "/v2/templates/template%2F1/builds/build%201":
			_ = json.NewDecoder(r.Body).Decode(&trigger)
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && path == "/templates/template%2F1/builds/build%201/status":
			pollQueries = append(pollQueries, r.URL.RawQuery)
			polls++
			if polls == 1 {
				_ = json.NewEncoder(w).Encode(map[string]any{"templateID": "template/1", "buildID": "build 1", "status": "building", "logEntries": []map[string]any{{"timestamp": "2026-09-14T00:00:00Z", "level": "info", "message": "building"}}})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"templateID": "template/1", "buildID": "build 1", "status": "ready"})
		default:
			http.Error(w, r.Method+" "+r.URL.String(), http.StatusNotFound)
		}
	}))

	client, err := NewClient(&SandboxOpts{APIKey: "key", APIURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	var logs []string
	builder := NewTemplateBuilder().FromPythonImage("3.12").Copy("main.py", "/app/main.py", CopyOptions{}).Run("python -m compileall /app")
	info, err := client.Templates().BuildTemplate(context.Background(), "python-app", builder, BuildOptions{InheritImageConfig: Bool(false),
		ContextPath:  contextPath,
		PollInterval: time.Millisecond,
		SkipCache:    true,
		OnLog:        func(entry BuildLog) { logs = append(logs, entry.Message) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if info.Status != "ready" || polls != 2 {
		t.Fatalf("build = %#v, polls = %d", info, polls)
	}
	if !reflect.DeepEqual(logs, []string{"building"}) {
		t.Fatalf("logs = %#v", logs)
	}
	if !reflect.DeepEqual(archiveNames, []string{"main.py"}) {
		t.Fatalf("archive names = %#v", archiveNames)
	}
	steps, ok := trigger["steps"].([]any)
	if !ok || len(steps) != 2 {
		t.Fatalf("trigger steps = %#v", trigger["steps"])
	}
	copyStep := steps[0].(map[string]any)
	if copyStep["filesHash"] == "" || trigger["fromImage"] != "python:3.12" {
		t.Fatalf("trigger = %#v", trigger)
	}
	if trigger["force"] != true {
		t.Fatalf("trigger force = %#v, want true", trigger["force"])
	}
	if _, exists := createBuild["skipCache"]; exists {
		t.Fatalf("create build must not contain skipCache: %#v", createBuild)
	}
	if len(pollQueries) != 2 || pollQueries[0] != "logsOffset=0" || pollQueries[1] != "logsOffset=1" {
		t.Fatalf("poll queries = %#v", pollQueries)
	}
	if calls[0] != "POST /v3/templates" || polls == 0 {
		t.Fatalf("calls = %#v", calls)
	}
}

func TestTemplateBackgroundBuildDoesNotPollAndSkipsCachedUpload(t *testing.T) {
	contextPath := t.TempDir()
	if err := os.WriteFile(filepath.Join(contextPath, "cached.txt"), []byte("cached"), 0o644); err != nil {
		t.Fatal(err)
	}
	polls, uploads := 0, 0
	server := newIPv4Server(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v3/templates":
			_ = json.NewEncoder(w).Encode(map[string]string{"templateID": "t", "buildID": "b"})
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/files/"):
			_ = json.NewEncoder(w).Encode(map[string]any{"present": true})
		case r.Method == http.MethodPost && r.URL.Path == "/v2/templates/t/builds/b":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPut:
			uploads++
		case strings.HasSuffix(r.URL.Path, "/status"):
			polls++
		default:
			http.Error(w, "unexpected", http.StatusNotFound)
		}
	}))
	client, _ := NewClient(&SandboxOpts{APIKey: "key", APIURL: server.URL})
	_, err := client.Templates().BuildTemplateInBackground(context.Background(), "cached", NewTemplateBuilder().FromDebianImage("").CopyFile("cached.txt", "/tmp/cached.txt"), BuildOptions{InheritImageConfig: Bool(false), ContextPath: contextPath})
	if err != nil {
		t.Fatal(err)
	}
	if polls != 0 || uploads != 0 {
		t.Fatalf("polls = %d, uploads = %d", polls, uploads)
	}
}

func TestTemplateBuilderSerialization(t *testing.T) {
	builder := NewTemplateBuilder().
		FromPythonImage("3.12").
		Workdir("/app").
		Env("APP_ENV", "prod", true).
		AptInstall([]string{"git", "curl"}, PackageInstallOptions{NoInstallRecommends: true}).
		PipInstall([]string{"requests"}, true).
		StartCommand("python /app/main.py", WaitForPort(8080))
	raw, err := builder.JSON()
	if err != nil {
		t.Fatal(err)
	}
	var spec map[string]any
	if err := json.Unmarshal(raw, &spec); err != nil {
		t.Fatal(err)
	}
	if spec["fromImage"] != "python:3.12" || spec["readyCmd"] != "ss -tuln | grep :8080" {
		t.Fatalf("spec = %#v", spec)
	}
	steps := spec["steps"].([]any)
	if len(steps) != 6 {
		t.Fatalf("steps = %#v", steps)
	}
	runtimeStep := steps[len(steps)-1].(map[string]any)
	runtimeArgs := runtimeStep["args"].([]any)
	if runtimeStep["type"] != "RUN" || runtimeArgs[1] != "root" || !strings.Contains(runtimeArgs[0].(string), "/tmp/.novita-image-env.sh") {
		t.Fatalf("runtime env step = %#v", runtimeStep)
	}
}

func TestTemplateBuilderSkipCacheForcesSubsequentLayers(t *testing.T) {
	builder := NewTemplateBuilder().FromPythonImage("3.12").Run("cached").SkipCache().Run("fresh").CopyFile("file", "/file")
	steps, err := decodeInstructions(builder.Spec()["instructions"])
	if err != nil {
		t.Fatal(err)
	}
	if steps[0].Force || !steps[1].Force || !steps[2].Force {
		t.Fatalf("force flags = %#v", []bool{steps[0].Force, steps[1].Force, steps[2].Force})
	}
}

func TestTemplateBuilderSerializationAlwaysCarriesStepsAndForce(t *testing.T) {
	// JS/Python always emit steps (empty array) and force (false) in the
	// serialized trigger body, even for a bare image-only template.
	raw, err := NewTemplateBuilder().FromImage("debian:12").JSON()
	if err != nil {
		t.Fatal(err)
	}
	var spec map[string]any
	if err := json.Unmarshal(raw, &spec); err != nil {
		t.Fatal(err)
	}
	steps, ok := spec["steps"].([]any)
	if !ok || len(steps) != 0 {
		t.Fatalf("steps = %#v, want empty array", spec["steps"])
	}
	if force, ok := spec["force"].(bool); !ok || force {
		t.Fatalf("force = %#v, want false", spec["force"])
	}

	// Caller-provided nil values are replaced like missing keys: the wire
	// body must always carry an array and a bool, never JSON null.
	serialized := serializeTemplateSpec(map[string]any{
		"fromImage": "debian:12",
		"steps":     nil,
		"force":     nil,
	})
	if steps, ok := serialized["steps"].([]any); !ok {
		t.Fatalf("steps = %#v, want array (nil replaced)", serialized["steps"])
	} else if len(steps) != 0 {
		t.Fatalf("steps = %#v, want empty array", steps)
	}
	if force, ok := serialized["force"].(bool); !ok || force {
		t.Fatalf("force = %#v, want false (nil replaced)", serialized["force"])
	}
}

func TestTemplateGetDerivesBuildFromBuildsArray(t *testing.T) {
	server := newIPv4Server(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/templates/tpl-1" {
			http.Error(w, "unexpected "+r.Method+" "+r.URL.Path, http.StatusNotFound)
			return
		}
		// Top-level buildID/buildStatus are stale; builds[0] wins, matching
		// JS Template.get (template/index.ts:137-142).
		_ = json.NewEncoder(w).Encode(map[string]any{
			"templateID":  "tpl-1",
			"buildID":     "stale",
			"buildStatus": "old",
			"builds":      []map[string]any{{"buildID": "b-9", "status": "ready"}},
		})
	}))
	client, err := NewClient(&SandboxOpts{APIKey: "key", APIURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	info, err := client.Templates().Get(context.Background(), "tpl-1")
	if err != nil {
		t.Fatal(err)
	}
	if info.BuildID != "b-9" || info.BuildStatus != "ready" {
		t.Fatalf("build = %q/%q, want b-9/ready", info.BuildID, info.BuildStatus)
	}
	if len(info.Builds) != 1 || info.Builds[0].BuildID != "b-9" {
		t.Fatalf("builds = %#v", info.Builds)
	}
}

func TestTemplateGetWithoutBuildsClearsTopLevelBuild(t *testing.T) {
	server := newIPv4Server(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"templateID":  "tpl-2",
			"buildID":     "stale",
			"buildStatus": "old",
		})
	}))
	client, err := NewClient(&SandboxOpts{APIKey: "key", APIURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	info, err := client.Templates().Get(context.Background(), "tpl-2")
	if err != nil {
		t.Fatal(err)
	}
	// JS: build?.buildID ?? '' — no builds entry leaves both empty.
	if info.BuildID != "" || info.BuildStatus != "" {
		t.Fatalf("build = %q/%q, want empty", info.BuildID, info.BuildStatus)
	}
}

func TestCommitSandboxDefaultsSnapshotTemplateType(t *testing.T) {
	server := newIPv4Server(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/sandboxes/sb-1/commit" {
			http.Error(w, "unexpected "+r.URL.Path, http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"templateID": "tpl-77"})
	}))
	// Commit is a legacy-only operation, matching the JS/Python SDKs.
	client, err := NewClient(&SandboxOpts{APIKey: "key", APIURL: server.URL, Domain: "sandbox.novita.ai"})
	if err != nil {
		t.Fatal(err)
	}
	info, err := client.CommitSandbox(context.Background(), "sb-1", "my-alias")
	if err != nil {
		t.Fatal(err)
	}
	// JS parses the commit response via toTemplateInfo(data, 'snapshot_template').
	if info.TemplateType != "snapshot_template" {
		t.Fatalf("templateType = %q, want snapshot_template", info.TemplateType)
	}
}
