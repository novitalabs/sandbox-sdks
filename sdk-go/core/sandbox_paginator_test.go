package core

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"reflect"
	"testing"
)

func TestSandboxPaginatorUsesLegacyPathAndHeaderToken(t *testing.T) {
	var queries []string
	server := newIPv4Server(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/sandboxes" {
			t.Errorf("path = %q, want /v2/sandboxes", r.URL.Path)
		}
		queries = append(queries, r.URL.RawQuery)
		if len(queries) == 1 {
			w.Header().Set("X-Next-Token", "next/token")
			writeJSON(t, w, []map[string]any{{"sandboxID": "one"}})
			return
		}
		writeJSON(t, w, []map[string]any{{"sandboxID": "two"}})
	}))
	client, err := NewClient(&SandboxOpts{APIKey: "key", APIURL: server.URL, Domain: "sandbox.novita.ai"})
	if err != nil {
		t.Fatal(err)
	}
	paginator := client.SandboxPaginator(SandboxListOptions{States: []SandboxState{SandboxStateRunning, SandboxStatePaused}, Metadata: map[string]string{"team": "sdk go"}, Limit: 1})
	first, err := paginator.Next(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 1 || first[0].SandboxID != "one" || !paginator.HasNext() {
		t.Fatalf("first = %#v", first)
	}
	second, err := paginator.Next(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(second) != 1 || second[0].SandboxID != "two" || paginator.HasNext() {
		t.Fatalf("second = %#v", second)
	}
	if len(queries) != 2 || queries[1] == queries[0] || !containsQueryValue(queries[1], "nextToken", "next/token") {
		t.Fatalf("queries = %#v", queries)
	}
}

func containsQueryValue(raw, key, value string) bool {
	values, err := url.ParseQuery(raw)
	return err == nil && values.Get(key) == value
}

func TestModernOnlySandboxAPIsReturnUnsupportedOnLegacy(t *testing.T) {
	client, err := NewClient(&SandboxOpts{APIKey: "key", APIURL: "https://example.invalid", Domain: "sandbox.novita.ai"})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		call func() error
	}{
		{"snapshot", func() error { _, err := client.CreateSnapshot(context.Background(), "id"); return err }},
		{"resize", func() error { return client.ResizeSandbox(context.Background(), "id", 512) }},
		{"network", func() error { return client.SetSandboxNetwork(context.Background(), "id", &SandboxNetworkConfig{}) }},
		{"mount", func() error {
			_, err := client.MountVolume(context.Background(), "id", VolumeMountOptions{Name: "v", Path: "/data"})
			return err
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var unsupported *UnsupportedError
			if err := test.call(); !errors.As(err, &unsupported) {
				t.Fatalf("error = %T %v", err, err)
			}
		})
	}
}

func TestDeleteSnapshotUsesTemplateEndpoint(t *testing.T) {
	server := newIPv4Server(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("method = %q, want DELETE", r.Method)
		}
		if r.URL.Path != "/templates/snapshot-id:default" {
			t.Errorf("path = %q, want template deletion endpoint", r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	client, err := NewClient(&SandboxOpts{APIKey: "key", APIURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	if deleted, err := client.DeleteSnapshot(context.Background(), "snapshot-id:default"); err != nil || !deleted {
		t.Fatalf("DeleteSnapshot = %v, %v; want true, nil", deleted, err)
	}
}

func TestSandboxListWrapperCompatibility(t *testing.T) {
	server := newIPv4Server(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(t, w, map[string]any{"sandboxes": []map[string]any{{"sandboxID": "wrapped"}}, "nextToken": "next"})
	}))
	client, _ := NewClient(&SandboxOpts{APIKey: "key", APIURL: server.URL})
	items, next, err := client.listSandboxesPage(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual([]string{items[0].SandboxID, next}, []string{"wrapped", "next"}) {
		t.Fatalf("items=%#v next=%q", items, next)
	}
}
