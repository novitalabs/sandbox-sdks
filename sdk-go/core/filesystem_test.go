package core

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// newTestSandbox wires up a Sandbox pointing at a fake envd server.
func newTestSandbox(t *testing.T, envdVersion string, handler http.Handler) *Sandbox {
	t.Helper()

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	connection := &ConnectionConfig{
		APIKey:           "test-key",
		Domain:           DefaultNovitaDomain,
		SandboxURL:       server.URL,
		RequestTimeoutMs: RequestTimeoutMS,
		Headers: map[string]string{
			"User-Agent":           "novita-sandbox-go-sdk/test",
			"X-Novita-SDK-Name":    SDKName,
			"X-Novita-SDK-Version": SDKVersion,
		},
	}

	// Mirror what newSandboxFromInfo builds: sandbox headers plus SDK headers.
	baseHeaders := map[string]string{
		"Novita-Sandbox-Id":   "test-sandbox",
		"Novita-Sandbox-Port": "49983",
	}
	for k, v := range connection.Headers {
		baseHeaders[k] = v
	}

	sandbox := &Sandbox{
		sandboxID:   "test-sandbox",
		connection:  connection,
		envdVersion: envdVersion,
		envdPort:    EnvdPort,
		baseHeaders: baseHeaders,
	}

	sandbox.rpc = newRPCClient(server.URL, sandbox.baseHeaders)
	sandbox.rest = newRESTClient(server.URL, sandbox.baseHeaders)
	sandbox.Files = newFilesystem(sandbox)
	sandbox.Commands = newCommands(sandbox)
	sandbox.Pty = newPty(sandbox)
	sandbox.Git = newGit(sandbox)

	return sandbox
}

// rpcRoute describes one expected Connect call.
type rpcRoute struct {
	method  string
	request interface{}
	respond func(w http.ResponseWriter)
}

// newRPCServer serves Connect JSON responses for the given methods.
func newRPCServer(t *testing.T, routes map[string]func(w http.ResponseWriter, r *http.Request)) http.Handler {
	t.Helper()

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Path looks like /filesystem.Filesystem/Stat
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/"), "/")
		if len(parts) != 2 {
			http.NotFound(w, r)
			return
		}
		if parts[0] != filesystemService && parts[0] != processService {
			t.Errorf("unexpected RPC service: %s", parts[0])
			http.NotFound(w, r)
			return
		}
		method := parts[1]

		handler, ok := routes[method]
		if !ok {
			t.Errorf("unexpected RPC method: %s", method)
			http.NotFound(w, r)
			return
		}

		handler(w, r)
	})
}

// writeJSON writes a JSON response body.
func writeJSON(t *testing.T, w http.ResponseWriter, payload interface{}) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		t.Errorf("failed to encode response: %v", err)
	}
}

func TestFilesystemPollingWatcherLifecycle(t *testing.T) {
	var methods []string
	sandbox := newTestSandbox(t, EnvdVersionRecursiveWatch, newRPCServer(t, map[string]func(http.ResponseWriter, *http.Request){
		"CreateWatcher": func(w http.ResponseWriter, r *http.Request) {
			methods = append(methods, "create")
			var request struct {
				Path      string `json:"path"`
				Recursive bool   `json:"recursive"`
			}
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Error(err)
			}
			if request.Path != "/work" || !request.Recursive {
				t.Errorf("request = %#v", request)
			}
			writeJSON(t, w, map[string]any{"watcherId": "watcher-1"})
		},
		"GetWatcherEvents": func(w http.ResponseWriter, r *http.Request) {
			methods = append(methods, "get")
			writeJSON(t, w, map[string]any{"events": []map[string]any{{"name": "a.txt", "type": 1}, {"name": "a.txt", "type": "EVENT_TYPE_WRITE"}}})
		},
		"RemoveWatcher": func(w http.ResponseWriter, r *http.Request) {
			methods = append(methods, "remove")
			writeJSON(t, w, map[string]any{})
		},
	}))
	watcher, err := sandbox.Files.CreateWatcher(context.Background(), "/work", &WatchOptions{Recursive: true})
	if err != nil {
		t.Fatal(err)
	}
	if watcher.ID != "watcher-1" {
		t.Fatalf("watcher = %#v", watcher)
	}
	events, err := sandbox.Files.GetWatcherEvents(context.Background(), watcher, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0].Type != FilesystemEventCreate || events[1].Type != FilesystemEventWrite {
		t.Fatalf("events = %#v", events)
	}
	if err := sandbox.Files.RemoveWatcher(context.Background(), watcher, nil); err != nil {
		t.Fatal(err)
	}
	if strings.Join(methods, ",") != "create,get,remove" {
		t.Fatalf("methods = %#v", methods)
	}
}

func TestFilesystemRecursiveWatcherVersionGate(t *testing.T) {
	sandbox := newTestSandbox(t, "0.1.3", http.NotFoundHandler())
	_, err := sandbox.Files.CreateWatcher(context.Background(), "/work", &WatchOptions{Recursive: true})
	var unsupported *UnsupportedError
	if !errors.As(err, &unsupported) {
		t.Fatalf("error = %T %v", err, err)
	}
}

func TestFilesystemRead(t *testing.T) {
	var gotPath, gotUser string

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != envdFilesRoute {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		gotPath = r.URL.Query().Get("path")
		gotUser = r.URL.Query().Get("username")

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("hello world"))
	})

	sandbox := newTestSandbox(t, "0.5.7", handler)

	data, err := sandbox.Files.Read(nil, "/tmp/hello.txt", nil)
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}

	if string(data) != "hello world" {
		t.Errorf("data = %q, want %q", string(data), "hello world")
	}
	if gotPath != "/tmp/hello.txt" {
		t.Errorf("path query = %q, want /tmp/hello.txt", gotPath)
	}
	// envd >= 0.4.0 with no explicit user sends no username.
	if gotUser != "" {
		t.Errorf("username = %q, want empty", gotUser)
	}
}

func TestFilesystemReadAppliesDefaultUserOnOldEnvd(t *testing.T) {
	var gotUser string

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUser = r.URL.Query().Get("username")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("x"))
	})

	sandbox := newTestSandbox(t, "0.3.0", handler)

	if _, err := sandbox.Files.Read(nil, "/tmp/x", nil); err != nil {
		t.Fatalf("Read failed: %v", err)
	}
	if gotUser != DefaultUsername {
		t.Errorf("username = %q, want %q", gotUser, DefaultUsername)
	}
}

func TestFilesystemReadNotFound(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"message": "/tmp/missing"})
	})

	sandbox := newTestSandbox(t, "0.5.7", handler)

	_, err := sandbox.Files.Read(nil, "/tmp/missing", nil)
	if err == nil {
		t.Fatal("expected an error")
	}
	if _, ok := err.(*FileNotFoundError); !ok {
		t.Errorf("error type = %T, want *FileNotFoundError", err)
	}
}

func TestFilesystemWriteMultipart(t *testing.T) {
	var gotPath string

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, "multipart/form-data") {
			t.Errorf("Content-Type = %q, want multipart/form-data", ct)
		}

		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatalf("failed to parse multipart form: %v", err)
		}

		file, header, err := r.FormFile("file")
		if err != nil {
			t.Fatalf("failed to read form file: %v", err)
		}
		defer file.Close()

		// Go's multipart reader strips the directory part from Filename, so
		// assert against the raw header to check the full path was sent.
		gotPath = header.Header.Get("Content-Disposition")
		body, _ := io.ReadAll(file)
		if string(body) != "content" {
			t.Errorf("body = %q, want content", string(body))
		}

		writeJSON(t, w, []WriteInfo{{Name: "a.txt", Path: "/tmp/a.txt"}})
	})

	sandbox := newTestSandbox(t, "0.5.0", handler)

	info, err := sandbox.Files.WriteText(nil, "/tmp/a.txt", "content", nil)
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	if info == nil || info.Path != "/tmp/a.txt" {
		t.Errorf("info = %+v, want path /tmp/a.txt", info)
	}
	if !strings.Contains(gotPath, "/tmp/a.txt") {
		t.Errorf("Content-Disposition = %q, want it to contain /tmp/a.txt", gotPath)
	}
}

func TestFilesystemWriteOctetStream(t *testing.T) {
	var gotPath string

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Content-Type"); got != "application/octet-stream" {
			t.Errorf("Content-Type = %q, want application/octet-stream", got)
		}
		gotPath = r.URL.Query().Get("path")

		body, _ := io.ReadAll(r.Body)
		if string(body) != "raw-bytes" {
			t.Errorf("body = %q, want raw-bytes", string(body))
		}

		writeJSON(t, w, []WriteInfo{{Name: "b.txt", Path: "/tmp/b.txt"}})
	})

	sandbox := newTestSandbox(t, "0.5.7", handler)

	_, err := sandbox.Files.Write(nil, "/tmp/b.txt", []byte("raw-bytes"), &FilesystemWriteOpts{
		UseOctetStream: true,
	})
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	if gotPath != "/tmp/b.txt" {
		t.Errorf("path query = %q, want /tmp/b.txt", gotPath)
	}
}

func TestFilesystemWriteFallsBackFromOctetStreamOnOldEnvd(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, "multipart/form-data") {
			t.Errorf("Content-Type = %q, want multipart fallback", ct)
		}
		writeJSON(t, w, []WriteInfo{{Name: "c.txt", Path: "/tmp/c.txt"}})
	})

	// envd below the octet-stream gate must fall back to multipart.
	sandbox := newTestSandbox(t, "0.5.6", handler)

	_, err := sandbox.Files.Write(nil, "/tmp/c.txt", []byte("x"), &FilesystemWriteOpts{
		UseOctetStream: true,
	})
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}
}

func TestFilesystemWriteFilesMultiple(t *testing.T) {
	var count int

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatalf("failed to parse multipart form: %v", err)
		}
		count = len(r.MultipartForm.File["file"])

		writeJSON(t, w, []WriteInfo{
			{Name: "a.txt", Path: "/a.txt"},
			{Name: "b.txt", Path: "/b.txt"},
		})
	})

	sandbox := newTestSandbox(t, "0.5.0", handler)

	files := []WriteEntry{
		{Path: "/a.txt", Data: []byte("a")},
		{Path: "/b.txt", Data: []byte("b")},
	}
	infos, err := sandbox.Files.WriteFiles(nil, files, nil)
	if err != nil {
		t.Fatalf("WriteFiles failed: %v", err)
	}
	if len(infos) != 2 {
		t.Errorf("got %d infos, want 2", len(infos))
	}
	if count != 2 {
		t.Errorf("multipart file count = %d, want 2", count)
	}
}

func TestFilesystemList(t *testing.T) {
	handler := newRPCServer(t, map[string]func(w http.ResponseWriter, r *http.Request){
		"ListDir": func(w http.ResponseWriter, r *http.Request) {
			var req map[string]interface{}
			_ = json.NewDecoder(r.Body).Decode(&req)

			if req["path"] != "/tmp" {
				t.Errorf("path = %v, want /tmp", req["path"])
			}
			if req["depth"] != float64(2) {
				t.Errorf("depth = %v, want 2", req["depth"])
			}

			writeJSON(t, w, map[string]interface{}{
				"entries": []map[string]interface{}{
					{
						"name":         "a.txt",
						"type":         "FILE_TYPE_FILE",
						"path":         "/tmp/a.txt",
						"size":         "12",
						"mode":         33188,
						"permissions":  "rw-r--r--",
						"owner":        "user",
						"group":        "user",
						"modifiedTime": "2026-09-17T02:46:59.123Z",
					},
					{
						"name": "sub",
						"type": 2, // DIRECTORY
						"path": "/tmp/sub",
						"size": "0",
					},
					{
						// Unspecified types must be dropped, matching JS/Python.
						"name": "weird",
						"type": 0,
						"path": "/tmp/weird",
						"size": "1",
					},
				},
			})
		},
	})

	sandbox := newTestSandbox(t, "0.5.7", handler)

	entries, err := sandbox.Files.List(nil, "/tmp", &FilesystemListOpts{Depth: 2})
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}

	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2 (unspecified type must be dropped)", len(entries))
	}
	if entries[0].Name != "a.txt" || entries[0].Type != FileTypeFile {
		t.Errorf("entry 0 = %+v, want file a.txt", entries[0])
	}
	if entries[0].Size != 12 {
		t.Errorf("size = %d, want 12", entries[0].Size)
	}
	if entries[0].Permissions != "rw-r--r--" {
		t.Errorf("permissions = %q, want rw-r--r--", entries[0].Permissions)
	}
	if entries[0].ModifiedTime == nil || entries[0].ModifiedTime.Format(time.RFC3339Nano) != "2026-09-17T02:46:59.123Z" {
		t.Errorf("modified time = %v", entries[0].ModifiedTime)
	}
	if entries[1].Type != FileTypeDir {
		t.Errorf("entry 1 type = %v, want dir", entries[1].Type)
	}
}

func TestFilesystemListDepthDefaultsToOne(t *testing.T) {
	var gotDepth float64

	handler := newRPCServer(t, map[string]func(w http.ResponseWriter, r *http.Request){
		"ListDir": func(w http.ResponseWriter, r *http.Request) {
			var req map[string]interface{}
			_ = json.NewDecoder(r.Body).Decode(&req)
			if d, ok := req["depth"].(float64); ok {
				gotDepth = d
			}
			writeJSON(t, w, map[string]interface{}{"entries": []interface{}{}})
		},
	})

	sandbox := newTestSandbox(t, "0.5.7", handler)

	if _, err := sandbox.Files.List(nil, "/tmp", &FilesystemListOpts{Depth: 0}); err != nil {
		t.Fatalf("depth 0 should default to 1, got error: %v", err)
	}
	if gotDepth != 1 {
		t.Errorf("depth = %v, want 1", gotDepth)
	}
}

func TestFilesystemListRejectsNegativeDepth(t *testing.T) {
	handler := newRPCServer(t, map[string]func(w http.ResponseWriter, r *http.Request){})
	sandbox := newTestSandbox(t, "0.5.7", handler)

	_, err := sandbox.Files.List(nil, "/tmp", &FilesystemListOpts{Depth: -1})
	if err == nil {
		t.Fatal("expected an error for a negative depth")
	}
	if _, ok := err.(*InvalidArgumentError); !ok {
		t.Errorf("error type = %T, want *InvalidArgumentError", err)
	}
}

func TestFilesystemMakeDirAlreadyExists(t *testing.T) {
	handler := newRPCServer(t, map[string]func(w http.ResponseWriter, r *http.Request){
		"MakeDir": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusConflict)
			writeJSON(t, w, map[string]string{
				"code":    CodeAlreadyExists,
				"message": "already exists",
			})
		},
	})

	sandbox := newTestSandbox(t, "0.5.7", handler)

	created, err := sandbox.Files.MakeDir(nil, "/tmp/existing", nil)
	if err != nil {
		t.Fatalf("MakeDir failed: %v", err)
	}
	if created {
		t.Error("expected created=false for an existing directory")
	}
}

func TestFilesystemMakeDirCreated(t *testing.T) {
	handler := newRPCServer(t, map[string]func(w http.ResponseWriter, r *http.Request){
		"MakeDir": func(w http.ResponseWriter, r *http.Request) {
			writeJSON(t, w, map[string]interface{}{
				"entry": map[string]interface{}{
					"name": "/tmp/new",
					"type": 2,
					"path": "/tmp/new",
				},
			})
		},
	})

	sandbox := newTestSandbox(t, "0.5.7", handler)

	created, err := sandbox.Files.MakeDir(nil, "/tmp/new", nil)
	if err != nil {
		t.Fatalf("MakeDir failed: %v", err)
	}
	if !created {
		t.Error("expected created=true")
	}
}

func TestFilesystemExists(t *testing.T) {
	handler := newRPCServer(t, map[string]func(w http.ResponseWriter, r *http.Request){
		"Stat": func(w http.ResponseWriter, r *http.Request) {
			var req map[string]interface{}
			_ = json.NewDecoder(r.Body).Decode(&req)

			if req["path"] == "/missing" {
				w.WriteHeader(http.StatusNotFound)
				writeJSON(t, w, map[string]string{"code": CodeNotFound, "message": "missing"})
				return
			}
			writeJSON(t, w, map[string]interface{}{
				"entry": map[string]interface{}{"name": "a", "type": 1, "path": "/a", "size": "1"},
			})
		},
	})

	sandbox := newTestSandbox(t, "0.5.7", handler)

	exists, err := sandbox.Files.Exists(nil, "/a", nil)
	if err != nil {
		t.Fatalf("Exists failed: %v", err)
	}
	if !exists {
		t.Error("expected /a to exist")
	}

	exists, err = sandbox.Files.Exists(nil, "/missing", nil)
	if err != nil {
		t.Fatalf("Exists failed: %v", err)
	}
	if exists {
		t.Error("expected /missing not to exist")
	}
}

func TestFilesystemRename(t *testing.T) {
	handler := newRPCServer(t, map[string]func(w http.ResponseWriter, r *http.Request){
		"Move": func(w http.ResponseWriter, r *http.Request) {
			var req map[string]interface{}
			_ = json.NewDecoder(r.Body).Decode(&req)

			if req["source"] != "/a.txt" || req["destination"] != "/b.txt" {
				t.Errorf("unexpected move request: %v", req)
			}

			writeJSON(t, w, map[string]interface{}{
				"entry": map[string]interface{}{"name": "b.txt", "type": 1, "path": "/b.txt", "size": "3"},
			})
		},
	})

	sandbox := newTestSandbox(t, "0.5.7", handler)

	info, err := sandbox.Files.Rename(nil, "/a.txt", "/b.txt", nil)
	if err != nil {
		t.Fatalf("Rename failed: %v", err)
	}
	if info.Path != "/b.txt" {
		t.Errorf("path = %q, want /b.txt", info.Path)
	}
}

func TestFilesystemRemove(t *testing.T) {
	called := false

	handler := newRPCServer(t, map[string]func(w http.ResponseWriter, r *http.Request){
		"Remove": func(w http.ResponseWriter, r *http.Request) {
			called = true
			writeJSON(t, w, map[string]interface{}{})
		},
	})

	sandbox := newTestSandbox(t, "0.5.7", handler)

	if err := sandbox.Files.Remove(nil, "/tmp/gone", nil); err != nil {
		t.Fatalf("Remove failed: %v", err)
	}
	if !called {
		t.Error("expected the Remove RPC to be called")
	}
}

func TestFilesystemRequestHeaders(t *testing.T) {
	var gotHeaders http.Header

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeaders = r.Header.Clone()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	sandbox := newTestSandbox(t, "0.5.7", handler)

	if _, err := sandbox.Files.Read(nil, "/tmp/x", nil); err != nil {
		t.Fatalf("Read failed: %v", err)
	}

	if gotHeaders.Get("Novita-Sandbox-Id") != "test-sandbox" {
		t.Errorf("Novita-Sandbox-Id = %q", gotHeaders.Get("Novita-Sandbox-Id"))
	}
	if gotHeaders.Get("Novita-Sandbox-Port") != "49983" {
		t.Errorf("Novita-Sandbox-Port = %q", gotHeaders.Get("Novita-Sandbox-Port"))
	}
	if gotHeaders.Get("X-Novita-SDK-Name") != SDKName {
		t.Errorf("X-Novita-SDK-Name = %q", gotHeaders.Get("X-Novita-SDK-Name"))
	}
}
