package core

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestAlphaLongRunningCreateTimeout(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		for _, value := range []string{"true", "false", "TRUE", ""} {
			for _, idle := range []bool{false, true} {
				t.Run(value+map[bool]string{true: "legacy", false: "modern"}[legacy]+map[bool]string{true: "Idle", false: "NoIdle"}[idle], func(t *testing.T) {
					var got map[string]interface{}
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						_ = json.NewDecoder(r.Body).Decode(&got)
						_, _ = w.Write([]byte(`{"sandboxID":"sb","envdVersion":"0.5.7"}`))
					}))
					defer server.Close()
					metadata := map[string]string{"long_running": value}
					if idle {
						metadata["idle_timeout"] = "120"
					}
					before := map[string]string{}
					for k, v := range metadata {
						before[k] = v
					}
					domain := "us-phx-1.sandbox.novita.ai"
					if legacy {
						domain = "sandbox.novita.ai"
					}
					opts := &SandboxOpts{APIKey: "test", APIURL: server.URL, Domain: domain, Template: "base", Timeout: 900, Metadata: metadata}
					client, err := NewClient(opts)
					if err != nil {
						t.Fatal(err)
					}
					_, err = client.CreateSandbox(context.Background(), opts)
					if err != nil {
						t.Fatal(err)
					}
					_, exists := got["timeout"]
					if exists == (value == "true") {
						t.Errorf("timeout presence %v for %q: %#v", exists, value, got)
					}
					if !reflect.DeepEqual(metadata, before) || opts.Timeout != 900 {
						t.Fatal("caller options changed")
					}
					encoded, _ := json.Marshal(metadata)
					actual, _ := json.Marshal(got["metadata"])
					if string(encoded) != string(actual) {
						t.Errorf("metadata changed: %s", actual)
					}
				})
			}
		}
	}
}
