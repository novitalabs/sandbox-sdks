package core

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestGitStatusAheadBehind(t *testing.T) {
	s := newTestSandbox(t, "0.5.7", gitStreamServer(t, "## feature...origin/feature [ahead 12, behind 3]\n", nil))
	got, err := s.Git.Status(context.Background(), nil)
	if err != nil || got.Ahead != 12 || got.Behind != 3 {
		t.Fatalf("status=%+v err=%v", got, err)
	}
}
func TestDockerfileExecQuotingAndValidation(t *testing.T) {
	b := NewTemplateBuilder().FromDockerfile("FROM python:3\nENV GREETING=\"hello world\" EMPTY=\"\"\nCMD [\"python\", \"a b.py\", \"$(echo unsafe)\"]")
	if b.Err() != nil {
		t.Fatal(b.Err())
	}
	if got := b.Spec()["startCmd"]; got != "python 'a b.py' '$(echo unsafe)'" {
		t.Fatalf("startCmd=%v", got)
	}
	if env := b.Spec()["envVars"].(map[string]string); env["GREETING"] != "hello world" || env["EMPTY"] != "" {
		t.Fatal(env)
	}
	for _, src := range []string{"FROM one\nFROM two", "FROM one\nCMD [bad]"} {
		if err := NewTemplateBuilder().FromDockerfile(src).Err(); err == nil {
			t.Fatalf("accepted invalid Dockerfile %q", src)
		}
	}
}
func TestFileReadStreamReturnsBeforeEOF(t *testing.T) {
	s := newTestSandbox(t, "0.5.7", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "hello")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	reader, err := s.Files.ReadStream(ctx, "/a", nil)
	if err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 5)
	if _, err = io.ReadFull(reader, buf); err != nil || string(buf) != "hello" {
		t.Fatalf("read=%q %v", buf, err)
	}
	if err = reader.Close(); err != nil {
		t.Fatal(err)
	}
}
func TestFileWriteStreamFormats(t *testing.T) {
	for _, tc := range []struct {
		name, version string
		octet         bool
	}{{"octet", "0.5.7", true}, {"multipart", "0.5.7", false}, {"legacy fallback", "0.5.6", true}} {
		t.Run(tc.name, func(t *testing.T) {
			data := strings.Repeat("payload", 30000)
			s := newTestSandbox(t, tc.version, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var reader io.Reader = r.Body
				if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/") {
					_, params, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
					part, err := multipart.NewReader(r.Body, params["boundary"]).NextPart()
					if err != nil {
						t.Error(err)
						return
					}
					reader = part
					if !strings.Contains(part.Header.Get("Content-Disposition"), "/tmp/a") {
						t.Error("upload path lost")
					}
				} else if r.URL.Query().Get("path") != "/tmp/a" {
					t.Error("upload path lost")
				}
				got, err := io.ReadAll(reader)
				if err != nil || string(got) != data {
					t.Errorf("received=%d err=%v", len(got), err)
				}
				io.WriteString(w, `[{"path":"/tmp/a","name":"a"}]`)
			}))
			got, err := s.Files.WriteStream(context.Background(), "/tmp/a", strings.NewReader(data), &FilesystemWriteOpts{UseOctetStream: tc.octet})
			if err != nil || got.Path != "/tmp/a" {
				t.Fatalf("write=%+v err=%v", got, err)
			}
		})
	}
}

type brokenFileReader struct{}

func (brokenFileReader) Read([]byte) (int, error) { return 0, errors.New("source read failed") }
func TestFileWriteStreamPropagatesReaderError(t *testing.T) {
	s := newTestSandbox(t, "0.5.7", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.Copy(io.Discard, r.Body) }))
	if _, err := s.Files.WriteStream(context.Background(), "/a", brokenFileReader{}, nil); err == nil {
		t.Fatal("source failure swallowed")
	}
}
func TestBuildStatusLogOffset(t *testing.T) {
	srv := newIPv4Server(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("logsOffset") != "8" {
			t.Errorf("query=%s", r.URL.RawQuery)
		}
		io.WriteString(w, `{"status":"ready"}`)
	}))
	c, err := NewClient(&SandboxOpts{APIKey: "dummy", APIURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = c.Templates().BuildStatusWithOptions(context.Background(), "t", "b", &BuildStatusOptions{LogsOffset: 8}); err != nil {
		t.Fatal(err)
	}
}
func TestImageInheritanceExplicitValuesWin(t *testing.T) {
	b := NewTemplateBuilder().FromImage("example/image").Env("PATH", "/explicit", true).Workdir("/explicit").Command("custom").ReadyCommand("ready")
	got, err := prepareImageSpec(context.Background(), b.Spec(), BuildOptions{ImageConfigResolver: func(context.Context, string, *RegistryConfig) (*ImageConfig, error) {
		return &ImageConfig{Env: map[string]string{"PATH": "/image", "LANG": "C", "HOME": "/unsafe"}, Workdir: "/image", Entrypoint: []string{"python"}, Cmd: []string{"a b.py"}}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	env := got["envVars"].(map[string]string)
	if env["PATH"] != "/explicit" || env["LANG"] != "C" || env["HOME"] != "" {
		t.Fatal(env)
	}
	if got["startCmd"] != "custom" || got["readyCmd"] != "ready" {
		t.Fatal(got)
	}
	steps, _ := decodeInstructions(got["instructions"])
	workdir := ""
	for _, step := range steps {
		if step.Type == "WORKDIR" {
			workdir = step.Args[0]
		}
	}
	if workdir != "/explicit" {
		t.Fatalf("effective workdir: %s", workdir)
	}
	if _, exists := b.Spec()["envVars"].(map[string]string)["LANG"]; exists {
		t.Fatal("builder mutated by inheritance")
	}
}
func TestImageInheritanceDefaultsOptOutAndFailure(t *testing.T) {
	calls := 0
	resolver := func(context.Context, string, *RegistryConfig) (*ImageConfig, error) {
		calls++
		return &ImageConfig{Workdir: "/work", Entrypoint: []string{"python"}, Cmd: []string{"a b.py"}}, nil
	}
	b := NewTemplateBuilder().FromImage("example/image")
	got, err := prepareImageSpec(context.Background(), b.Spec(), BuildOptions{ImageConfigResolver: resolver})
	if err != nil || got["startCmd"] != "python 'a b.py'" || got["readyCmd"] != "sleep 20" {
		t.Fatalf("got=%v err=%v", got, err)
	}
	_, err = prepareImageSpec(context.Background(), b.InheritImageConfig(false).Spec(), BuildOptions{ImageConfigResolver: resolver})
	if err != nil || calls != 1 {
		t.Fatalf("opt-out called resolver %d times: %v", calls, err)
	}
	warned := false
	_, err = prepareImageSpec(context.Background(), NewTemplateBuilder().FromImage("missing").Spec(), BuildOptions{OnLog: func(BuildLog) { warned = true }, ImageConfigResolver: func(context.Context, string, *RegistryConfig) (*ImageConfig, error) {
		return nil, errors.New("unavailable")
	}})
	if err != nil || !warned {
		t.Fatalf("lookup failure err=%v warned=%v", err, warned)
	}
}

func TestNetworkPublicTrafficRoundTrip(t *testing.T) {
	for _, value := range []bool{false, true} {
		config := SandboxNetworkConfig{AllowPublicTrafficOption: Bool(value), AllowOut: []string{}}
		raw, err := json.Marshal(config)
		if err != nil {
			t.Fatal(err)
		}
		var roundtrip SandboxNetworkConfig
		if err = json.Unmarshal(raw, &roundtrip); err != nil {
			t.Fatal(err)
		}
		if roundtrip.AllowPublicTraffic != value || roundtrip.AllowPublicTrafficOption == nil || *roundtrip.AllowPublicTrafficOption != value {
			t.Fatalf("roundtrip=%+v wire=%s", roundtrip, raw)
		}
		if !bytes.Contains(raw, []byte(`"allowOut":[]`)) {
			t.Fatalf("explicit empty egress lost: %s", raw)
		}
	}
}
func TestFileWriteStreamEmptyAndGzip(t *testing.T) {
	for _, payload := range []string{"", strings.Repeat("bytes", 20000)} {
		s := newTestSandbox(t, "0.5.7", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			decoder, err := gzip.NewReader(r.Body)
			if err != nil {
				t.Error(err)
				return
			}
			defer decoder.Close()
			data, err := io.ReadAll(decoder)
			if err != nil || string(data) != payload {
				t.Errorf("gzip upload=%d %v", len(data), err)
			}
			io.WriteString(w, `[{"path":"/a"}]`)
		}))
		if _, err := s.Files.WriteStream(context.Background(), "/a", strings.NewReader(payload), &FilesystemWriteOpts{UseOctetStream: true, Gzip: true}); err != nil {
			t.Fatal(err)
		}
	}
}
func TestFileWriteStreamSendsBeforeSourceEOF(t *testing.T) {
	received := make(chan struct{})
	s := newTestSandbox(t, "0.5.7", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		prefix := make([]byte, 4)
		if _, err := io.ReadFull(r.Body, prefix); err != nil {
			t.Error(err)
			return
		}
		close(received)
		if _, err := io.Copy(io.Discard, r.Body); err != nil {
			t.Error(err)
		}
		io.WriteString(w, `[{"path":"/a"}]`)
	}))
	reader, writer := io.Pipe()
	defer reader.Close()
	go func() {
		writer.Write([]byte("data"))
		select {
		case <-received:
			writer.Close()
		case <-time.After(time.Second):
			writer.CloseWithError(errors.New("upload buffered until EOF"))
		}
	}()
	if _, err := s.Files.WriteStream(context.Background(), "/a", reader, &FilesystemWriteOpts{UseOctetStream: true}); err != nil {
		t.Fatal(err)
	}
}
func TestFileReadStreamTruncatedBodyAndUser(t *testing.T) {
	s := newTestSandbox(t, "0.5.7", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("username") != "root" || r.Header.Get("Authorization") == "" {
			t.Error("file read user lost")
		}
		w.Header().Set("Content-Length", "10")
		io.WriteString(w, "short")
	}))
	reader, err := s.Files.ReadStream(context.Background(), "/a", &FilesystemReadOpts{FilesystemRequestOpts: FilesystemRequestOpts{User: Username{Name: "root"}}})
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	if _, err = io.ReadAll(reader); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("truncated body=%v", err)
	}
}
func TestLegacyCreateAllowsPublicTrafficOption(t *testing.T) {
	srv := newIPv4Server(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if body["network"].(map[string]any)["allowPublicTraffic"] != false {
			t.Error(body)
		}
		io.WriteString(w, `{"sandboxID":"legacy","envdVersion":"0.5.7"}`)
	}))
	c, err := NewClient(&SandboxOpts{APIURL: srv.URL, APIKey: "dummy", Domain: "sandbox.novita.ai"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = c.CreateSandbox(context.Background(), &SandboxOpts{Template: "base", Network: &SandboxNetworkConfig{AllowPublicTrafficOption: Bool(false)}}); err != nil {
		t.Fatal(err)
	}
}

func TestSnapshotListConvenienceUsesNextToken(t *testing.T) {
	server := newIPv4Server(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("nextToken") != "cursor-token" || r.URL.Query().Has("cursor") {
			t.Errorf("snapshot query=%s", r.URL.RawQuery)
		}
		io.WriteString(w, `[]`)
	}))
	client, err := NewNovita(&SandboxOpts{APIKey: "dummy", APIURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = client.Sandbox.ListSnapshotsTyped(context.Background(), SnapshotListOptions{Cursor: "cursor-token"}); err != nil {
		t.Fatal(err)
	}
}
func TestCoreFacadeInheritsOptionsWithoutMutatingCaller(t *testing.T) {
	timeout := 22
	constructor := &SandboxOpts{APIKey: "base", APIURL: "https://example.test", SandboxURL: "https://sandbox.test", SandboxDomain: "initial.test", RequestTimeoutMs: &timeout, Headers: map[string]string{"X-Base": "base", "X-Override": "base"}}
	client, err := NewClient(constructor)
	if err != nil {
		t.Fatal(err)
	}
	constructor.SandboxDomain = "mutated.test"
	call := &SandboxOpts{APIKey: "override", Headers: map[string]string{"X-Override": "call"}}
	merged := client.inheritOptions(call)
	if merged.APIKey != "override" || merged.APIURL != "https://example.test" || merged.SandboxURL != "https://sandbox.test" || merged.SandboxDomain != "initial.test" || *merged.RequestTimeoutMs != 22 || merged.Headers["X-Base"] != "base" || merged.Headers["X-Override"] != "call" {
		t.Fatalf("merged=%+v", merged)
	}
	merged.Headers["X-Override"] = "changed"
	if call.Headers["X-Override"] != "call" || call.APIURL != "" || constructor.Headers["X-Override"] != "base" {
		t.Fatal("options were mutated")
	}
}

func TestDockerfileCombinesEntrypointAndCMD(t *testing.T) {
	for _, tc := range []struct{ name, source, want string }{
		{"exec pair", `ENTRYPOINT ["python"]` + "\n" + `CMD ["app.py","a b"]`, "python app.py 'a b'"},
		{"reverse order", `CMD ["app.py","a b"]` + "\n" + `ENTRYPOINT ["python"]`, "python app.py 'a b'"},
		{"last values win", `ENTRYPOINT ["unused"]` + "\n" + `CMD ["old"]` + "\n" + `ENTRYPOINT ["python"]` + "\n" + `CMD ["new.py"]`, "python new.py"},
		{"shell entrypoint ignores cmd", "ENTRYPOINT exec python app.py\n" + `CMD ["ignored"]`, "exec python app.py"},
		{"exec entrypoint shell cmd", `ENTRYPOINT ["launcher"]` + "\nCMD echo $VALUE", "launcher /bin/sh -c 'echo $VALUE'"},
		{"empty entrypoint", `ENTRYPOINT []` + "\nCMD echo $VALUE", "echo $VALUE"},
		{"empty cmd", `ENTRYPOINT ["python","app.py"]` + "\nCMD []", "python app.py"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := NewTemplateBuilder().FromDockerfile("FROM debian:12\n" + tc.source)
			raw, err := b.JSON()
			if err != nil {
				t.Fatal(err)
			}
			var spec map[string]any
			if err = json.Unmarshal(raw, &spec); err != nil {
				t.Fatal(err)
			}
			if spec["startCmd"] != tc.want {
				t.Fatalf("effective command=%v want=%s", spec["startCmd"], tc.want)
			}
		})
	}
}
