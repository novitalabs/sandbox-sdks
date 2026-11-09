package core

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestImageReferenceNormalization(t *testing.T) {
	for _, tc := range []struct{ input, repository, reference string }{{"python", "library/python", "latest"}, {"docker.io/python:3", "library/python", "3"}, {"index.docker.io/team/image:dev", "team/image", "dev"}, {"ghcr.io/team/image@sha256:abc", "team/image", "sha256:abc"}, {"registry:5000/team/image:v1", "team/image", "v1"}} {
		got, err := parseImageReference(tc.input)
		if err != nil || got.repository != tc.repository || got.reference != tc.reference {
			t.Fatalf("%q => %+v %v", tc.input, got, err)
		}
	}
	for _, input := range []string{"", "https://user:pass@example.com/repo", "http://example.com/repo"} {
		if _, err := parseImageReference(input); err == nil {
			t.Fatalf("accepted %q", input)
		}
	}
}
func TestFetchImageConfigRegistryAuthAndAmd64Selection(t *testing.T) {
	var serverURL string
	tokenCalls, manifestCalls := 0, 0
	srv := newIPv4Server(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-API-Key") != "" {
			t.Error("sandbox credentials sent to registry")
		}
		if r.URL.Path == "/token" {
			tokenCalls++
			user, password, ok := r.BasicAuth()
			if !ok || user != "image-user" || password != "image-pass" {
				t.Error("registry credentials missing")
			}
			if r.URL.Query().Get("scope") != "repository:team/image:pull" {
				t.Error("invalid token scope")
			}
			io.WriteString(w, `{"access_token":"image-token"}`)
			return
		}
		if r.Header.Get("Authorization") != "Bearer image-token" {
			w.Header().Set("WWW-Authenticate", fmt.Sprintf(`Bearer realm="%s/token",service="fixture"`, serverURL))
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if !strings.Contains(r.Header.Get("Accept"), "application/vnd.oci.image.index.v1+json") {
			t.Error("missing OCI accept header")
		}
		switch r.URL.Path {
		case "/v2/team/image/manifests/latest":
			manifestCalls++
			io.WriteString(w, `{"manifests":[{"digest":"sha256:arm","platform":{"os":"linux","architecture":"arm64"}},{"digest":"sha256:amd","platform":{"os":"linux","architecture":"amd64"}}]}`)
		case "/v2/team/image/manifests/sha256:amd":
			io.WriteString(w, `{"config":{"digest":"sha256:config"}}`)
		case "/v2/team/image/blobs/sha256:config":
			io.WriteString(w, `{"architecture":"amd64","os":"linux","config":{"Env":["PATH=/tool/bin:/usr/bin","LANG=C.UTF-8","HOME=/root","TOKEN=a=b"],"WorkingDir":"/image","Entrypoint":["python"],"Cmd":["a b.py"]}}`)
		default:
			t.Errorf("unexpected URL %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	serverURL = srv.URL
	config, err := FetchImageConfig(context.Background(), srv.URL+"/team/image", &RegistryConfig{Type: "registry", Username: "image-user", Password: "image-pass"})
	if err != nil {
		t.Fatal(err)
	}
	if tokenCalls != 1 || manifestCalls != 1 || config.Env["PATH"] != "/tool/bin:/usr/bin" || config.Env["HOME"] != "" || config.Env["TOKEN"] != "a=b" || config.Workdir != "/image" {
		t.Fatalf("config=%+v tokenCalls=%d manifestCalls=%d", config, tokenCalls, manifestCalls)
	}
}
func TestFetchImageConfigRetriesAndRejectsMissingPlatform(t *testing.T) {
	attempts := 0
	srv := newIPv4Server(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(429)
			return
		}
		io.WriteString(w, `{"manifests":[{"digest":"sha256:arm","platform":{"os":"linux","architecture":"arm64"}}]}`)
	}))
	_, err := FetchImageConfig(context.Background(), srv.URL+"/image", nil)
	if err == nil || !strings.Contains(err.Error(), "linux/amd64") || attempts != 2 {
		t.Fatalf("err=%v attempts=%d", err, attempts)
	}
}
func TestFetchImageConfigCancellationAndMissingBlob(t *testing.T) {
	t.Run("cancel", func(t *testing.T) {
		srv := newIPv4Server(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		defer cancel()
		if _, err := FetchImageConfig(ctx, srv.URL+"/image", nil); err == nil {
			t.Fatal("cancelled request succeeded")
		}
	})
	t.Run("missing blob", func(t *testing.T) {
		srv := newIPv4Server(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, `{}`) }))
		if _, err := FetchImageConfig(context.Background(), srv.URL+"/image", nil); err == nil {
			t.Fatal("missing blob accepted")
		}
	})
}
func TestBuildTemplateInheritsImageConfigOnWire(t *testing.T) {
	var triggered map[string]any
	srv := newIPv4Server(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v3/templates":
			io.WriteString(w, `{"templateID":"t","buildID":"b"}`)
		case "/v2/templates/t/builds/b":
			if err := json.NewDecoder(r.Body).Decode(&triggered); err != nil {
				t.Error(err)
			}
			io.WriteString(w, `{}`)
		case "/v2/image/manifests/latest":
			io.WriteString(w, `{"config":{"digest":"sha256:c"}}`)
		case "/v2/image/blobs/sha256:c":
			io.WriteString(w, `{"config":{"Env":["PATH=/tool/bin"],"WorkingDir":"/work","Entrypoint":["python"],"Cmd":["app.py"]}}`)
		default:
			t.Errorf("unexpected URL %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	client, err := NewClient(&SandboxOpts{APIURL: srv.URL, APIKey: "sandbox-key"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Templates().BuildTemplateInBackground(context.Background(), "image-test", NewTemplateBuilder().FromImage(srv.URL+"/image"), BuildOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if triggered["startCmd"] != "python app.py" || triggered["readyCmd"] != "sleep 20" {
		t.Fatal(triggered)
	}
	if _, exists := triggered["inheritImageConfig"]; exists {
		t.Fatal("internal option leaked on wire")
	}
	steps := triggered["steps"].([]any)
	if len(steps) != 2 {
		t.Fatalf("expected workdir and environment restoration: %+v", steps)
	}
}

func TestRegistryChallengeDoesNotForwardCredentialsAcrossOrigins(t *testing.T) {
	realm := newIPv4Server(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Error("registry credentials forwarded to unrelated token origin")
		}
		io.WriteString(w, `{"token":"anonymous-token"}`)
	}))
	registry := newIPv4Server(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "Bearer anonymous-token" {
			io.WriteString(w, `{}`)
			return
		}
		w.Header().Set("WWW-Authenticate", fmt.Sprintf(`Bearer realm="%s/token"`, realm.URL))
		w.WriteHeader(401)
	}))
	_, err := FetchImageConfig(context.Background(), registry.URL+"/image", &RegistryConfig{Username: "private-user", Password: "private-password"})
	if err == nil {
		t.Fatal("invalid manifest should still fail after anonymous token exchange")
	}
}
func TestRegistryRedirectDoesNotForwardCredentials(t *testing.T) {
	target := newIPv4Server(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Error("registry credentials forwarded to redirect origin")
		}
		io.WriteString(w, `{}`)
	}))
	registry := newIPv4Server(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/manifest", http.StatusFound)
	}))
	_, err := FetchImageConfig(context.Background(), registry.URL+"/image", &RegistryConfig{Username: "private-user", Password: "private-password"})
	if err == nil {
		t.Fatal("invalid manifest should fail")
	}
}

func TestRegistryRedirectRejectsDowngradeAndScopesOrigins(t *testing.T) {
	original, _ := http.NewRequest(http.MethodGet, "https://127.0.0.1:4443/manifest", nil)
	downgrade, _ := http.NewRequest(http.MethodGet, "http://127.0.0.1:4443/manifest", nil)
	downgrade.Header.Set("Authorization", "Basic secret")
	if err := registryRedirect(downgrade, []*http.Request{original}); err == nil {
		t.Fatal("accepted HTTPS to HTTP redirect")
	}
	if downgrade.Header.Get("Authorization") != "" {
		t.Fatal("scheme change retained credentials")
	}
	for _, target := range []string{"https://127.0.0.1:4444/manifest", "https://example.test/manifest"} {
		req, _ := http.NewRequest(http.MethodGet, target, nil)
		req.Header.Set("Authorization", "Basic secret")
		if err := registryRedirect(req, []*http.Request{original}); err != nil {
			t.Fatal(err)
		}
		if req.Header.Get("Authorization") != "" {
			t.Fatalf("different origin retained credentials: %s", target)
		}
	}
	hub, _ := url.Parse("https://registry-1.docker.io")
	for _, tc := range []struct {
		realm   string
		allowed bool
	}{{"https://auth.docker.io/token", true}, {"https://registry-1.docker.io/token", true}, {"http://auth.docker.io/token", false}, {"https://auth.docker.io:444/token", false}, {"https://evil.auth.docker.io/token", false}} {
		realm, _ := url.Parse(tc.realm)
		if got := registryCredentialsAllowed(hub, realm); got != tc.allowed {
			t.Fatalf("%s credentials allowed=%v", tc.realm, got)
		}
	}
}

func TestBuildRetainsImageConfigFailureWithoutLogCallback(t *testing.T) {
	var triggered map[string]any
	server := newIPv4Server(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v3/templates" {
			io.WriteString(w, `{"templateID":"t","buildID":"b"}`)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&triggered); err != nil {
			t.Error(err)
		}
		io.WriteString(w, `{}`)
	}))
	client, err := NewClient(&SandboxOpts{APIKey: "dummy", APIURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	info, err := client.Templates().BuildTemplateInBackground(context.Background(), "diagnostics", NewTemplateBuilder().FromImage("unreachable/image"), BuildOptions{
		ImageConfigResolver: func(context.Context, string, *RegistryConfig) (*ImageConfig, error) {
			return nil, fmt.Errorf("registry unavailable")
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(info.ImageConfigFailure, "registry unavailable") {
		t.Fatalf("missing build diagnostics: %+v", info)
	}
	if _, exists := triggered["imageConfigFailure"]; exists {
		t.Fatal("local diagnosis leaked into trigger payload")
	}
}

func TestChangingImageDoesNotReuseRegistryCredentials(t *testing.T) {
	seen := make(chan string, 2)
	server := newIPv4Server(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/image/manifests/latest":
			seen <- r.Header.Get("Authorization")
			io.WriteString(w, `{"config":{"digest":"sha256:c"}}`)
		case "/v2/image/blobs/sha256:c":
			io.WriteString(w, `{"config":{}}`)
		case "/v3/templates":
			io.WriteString(w, `{"templateID":"t","buildID":"b"}`)
		case "/v2/templates/t/builds/b":
			io.WriteString(w, `{}`)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	client, err := NewClient(&SandboxOpts{APIKey: "fixture-api-key", APIURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	builder := NewTemplateBuilder().FromImage("old-private.invalid/image").RegistryCredentials("fixture-user", "fixture-password").FromImage(server.URL + "/image")
	if _, err = client.Templates().BuildTemplateInBackground(context.Background(), "image-change", builder, BuildOptions{}); err != nil {
		t.Fatal(err)
	}
	if auth := <-seen; auth != "" {
		t.Fatal("new registry received previous image credentials")
	}
	if _, exists := builder.Spec()["fromImageRegistry"]; exists {
		t.Fatal("old registry credentials retained in build payload")
	}
}
