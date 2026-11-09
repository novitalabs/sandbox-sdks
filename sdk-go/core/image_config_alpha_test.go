package core

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestImageConfigFailurePolicies(t *testing.T) {
	old := log.Writer()
	defer log.SetOutput(old)
	var output bytes.Buffer
	log.SetOutput(&output)
	b := NewTemplateBuilder().FromImage("registry/image")
	for _, policy := range []ImageConfigFailurePolicy{ImageConfigWarn, ImageConfigIgnore, ImageConfigRaise} {
		output.Reset()
		diagnostic := "old"
		logs := 0
		_, err := prepareImageSpecWithDiagnostic(context.Background(), b.Spec(), BuildOptions{ImageConfigOptions: &ImageConfigOptions{OnConfigFailure: policy}, OnLog: func(BuildLog) { logs++ }, ImageConfigResolver: func(context.Context, string, *RegistryConfig) (*ImageConfig, error) {
			return nil, fmt.Errorf("unavailable")
		}}, &diagnostic)
		if (err != nil) != (policy == ImageConfigRaise) {
			t.Fatalf("%s: %v", policy, err)
		}
		if !strings.Contains(diagnostic, "unavailable") {
			t.Fatal(diagnostic)
		}
		if policy == ImageConfigWarn {
			if output.Len() == 0 || logs != 1 {
				t.Fatal("warning not reported")
			}
		} else if output.Len() != 0 || logs != 0 {
			t.Fatal("unexpected warning")
		}
	}
	_, err := prepareImageSpec(context.Background(), b.Spec(), BuildOptions{ImageConfigOptions: &ImageConfigOptions{OnConfigFailure: "invalid"}})
	if err == nil {
		t.Fatal("invalid failure policy accepted")
	}
}

func TestImageConfigCustomTLSAndMixedChallenge(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			if r.URL.Query().Get("service") != "registry,quoted\"service" || r.URL.Query().Get("scope") != "repository:org/image:pull" {
				t.Errorf("token query: %v", r.URL.Query())
			}
			fmt.Fprint(w, `{"token":"bearer"}`)
		case "/v2/org/image/manifests/sha256:pin":
			if r.Header.Get("Authorization") != "Bearer bearer" {
				w.Header().Add("WWW-Authenticate", `Basic realm="basic"`)
				w.Header().Add("WWW-Authenticate", `Bearer realm="`+server.URL+`/token",service="registry,quoted\"service"`)
				w.WriteHeader(401)
				return
			}
			fmt.Fprint(w, `{"config":{"digest":"sha256:cfg"}}`)
		case "/v2/org/image/blobs/sha256:cfg":
			fmt.Fprint(w, `{"config":{"WorkingDir":"/app"}}`)
		default:
			t.Error(r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	got, err := FetchImageConfigWithOptions(context.Background(), server.URL+"/org/image:tag@sha256:pin", nil, ImageConfigOptions{Transport: server.Client().Transport})
	if err != nil || got.Workdir != "/app" {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestImageConfigChallengeTokenParameters(t *testing.T) {
	for _, challenge := range []string{`Bearer realm=https://example.test/token,service=registry,scope=aws`, `Bearer realm="https://example.test/token",service="registry",scope="aws", Basic realm="ignored"`} {
		got := bearerChallenge(challenge)
		if got["realm"] != "https://example.test/token" || got["scope"] != "aws" || got["service"] != "registry" {
			t.Fatal(got)
		}
	}
}
