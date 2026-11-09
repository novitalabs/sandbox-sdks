package core

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestValidateSecretEnvs(t *testing.T) {
	got, err := ValidateSecretEnvs(map[string]string{"TOKEN": "service.token"}, map[string]string{"MODE": "test"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, map[string]string{"TOKEN": "service.token"}) {
		t.Fatalf("bindings = %#v", got)
	}

	for _, test := range []struct {
		name    string
		secrets map[string]string
		envs    map[string]string
	}{
		{name: "invalid env", secrets: map[string]string{"1TOKEN": "secret"}},
		{name: "collision", secrets: map[string]string{"TOKEN": "secret"}, envs: map[string]string{"TOKEN": "plain"}},
		{name: "invalid secret", secrets: map[string]string{"TOKEN": "bad secret"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := ValidateSecretEnvs(test.secrets, test.envs)
			var invalid *InvalidArgumentError
			if !errors.As(err, &invalid) {
				t.Fatalf("error = %T %v, want *InvalidArgumentError", err, err)
			}
		})
	}
}

func TestNormalizeSecretHosts(t *testing.T) {
	got, err := normalizeSecretHosts([]string{" Example.COM ", "*.API.example.com", "example.com"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"example.com", "*.api.example.com"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("hosts = %#v, want %#v", got, want)
	}

	for _, host := range []string{"", "https://example.com", "example.com:443", "*.*.example.com"} {
		if _, err := normalizeSecretHosts([]string{host}); err == nil {
			t.Fatalf("host %q unexpectedly accepted", host)
		}
	}
}

func TestNormalizeSecretHostsIDNA(t *testing.T) {
	for _, tc := range []struct {
		name  string
		hosts []string
		want  []string
	}{
		{"unicode", []string{" BÜCHER.Example "}, []string{"xn--bcher-kva.example"}},
		{"wildcard", []string{"*.BÜCHER.Example"}, []string{"*.xn--bcher-kva.example"}},
		{"deduplication", []string{"BÜCHER.Example", "xn--bcher-kva.example", "bücher.example", "*.BÜCHER.Example", "*.xn--bcher-kva.example"}, []string{"xn--bcher-kva.example", "*.xn--bcher-kva.example"}},
		{"python mapping", []string{"faß.de", "fass.de"}, []string{"fass.de"}},
		{"dotted I", []string{"İstanbul.Example", "i̇stanbul.example", "*.İstanbul.Example", "*.i̇stanbul.example", "istanbul.example"}, []string{"xn--istanbul-o0e.example", "*.xn--istanbul-o0e.example", "istanbul.example"}},
		{"valid ace labels", []string{"XN--BCHER-KVA.Example", "xn--fa-hia.de"}, []string{"xn--bcher-kva.example", "xn--fa-hia.de"}},
		{"ascii compatibility", []string{"AB--CD.Example", "*.ab--cd.example"}, []string{"ab--cd.example", "*.ab--cd.example"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := normalizeSecretHosts(tc.hosts)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("hosts = %#v, want %#v", got, tc.want)
			}
		})
	}
}

func TestSecretWritesNormalizeIDNAHosts(t *testing.T) {
	for _, update := range []bool{false, true} {
		t.Run(map[bool]string{false: "create", true: "update"}[update], func(t *testing.T) {
			var got map[string]any
			client := reportClient(false, func(r *http.Request) (*http.Response, error) {
				method, path := http.MethodPost, "/secrets"
				if update {
					method, path = http.MethodPut, "/secrets/idna-fixture"
				}
				if r.Method != method || r.URL.Path != path {
					t.Errorf("request = %s %s, want %s %s", r.Method, r.URL.Path, method, path)
				}
				if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
					return nil, err
				}
				return reportResponse(200, `{"name":"idna-fixture","hosts":["xn--bcher-kva.example","*.xn--bcher-kva.example"]}`), nil
			})
			params := SecretWriteParams{Name: "idna-fixture", Value: "SYNTHETIC_PLACEHOLDER", Hosts: []string{"BÜCHER.Example", "xn--bcher-kva.example", "*.BÜCHER.Example"}}
			write := client.CreateSecret
			if update {
				write = client.UpdateSecret
			}
			if _, err := write(context.Background(), params); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got["hosts"], []any{"xn--bcher-kva.example", "*.xn--bcher-kva.example"}) {
				t.Fatalf("request hosts = %#v", got["hosts"])
			}
			if params.Hosts[0] != "BÜCHER.Example" {
				t.Fatal("write mutated the caller's hosts")
			}
		})
	}
}

func TestSecretWritesRejectInvalidIDNAHostsLocally(t *testing.T) {
	for _, host := range []string{"https://bücher.example", "bücher.example:443", "*.bücher.example/path", "*.*.bücher.example", "bücher.*.example", "bücher..example", "-bücher.example", "bücher-.example", "bad host.bücher.example", "xn--example-.bücher.example", "xn--example-。bücher.example", "ｘｎ－－ｅｘａｍｐｌｅ－.bücher.example", "xn--example-.com", "\xff.example", strings.Repeat("é", 64) + ".example"} {
		t.Run(host, func(t *testing.T) {
			client := reportClient(false, func(r *http.Request) (*http.Response, error) {
				t.Error("invalid host reached HTTP transport")
				return reportResponse(200, `{}`), nil
			})
			params := SecretWriteParams{Name: "idna-fixture", Value: "SYNTHETIC_PLACEHOLDER", Hosts: []string{host}}
			for _, write := range []func(context.Context, SecretWriteParams) (*SecretBinding, error){client.CreateSecret, client.UpdateSecret} {
				_, err := write(context.Background(), params)
				var invalid *InvalidArgumentError
				if !errors.As(err, &invalid) {
					t.Errorf("error = %T %v, want *InvalidArgumentError", err, err)
				}
			}
		})
	}
}

func TestVolumeDeleteReportsFalseOn404(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != http.MethodDelete || r.URL.Path != "/volumes/vol-1" {
			http.Error(w, "unexpected "+r.Method+" "+r.URL.Path, http.StatusBadRequest)
			return
		}
		// First call: already gone. Second call: destroyed.
		if calls == 1 {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client, err := NewClient(&SandboxOpts{APIKey: "k", APIURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	// 404 reports false like JS/Python Volume.destroy().
	if deleted, err := client.DeleteVolume(context.Background(), "vol-1"); err != nil || deleted {
		t.Fatalf("DeleteVolume(404) = %v, %v; want false, nil", deleted, err)
	}
	if deleted, err := client.DeleteVolume(context.Background(), "vol-1"); err != nil || !deleted {
		t.Fatalf("DeleteVolume = %v, %v; want true, nil", deleted, err)
	}
}

func TestVolumeUpdateQuotaSendsPatchWithDerivedInodes(t *testing.T) {
	var gotMethod string
	var gotBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotBody, _ = io.ReadAll(r.Body)
		_ = json.NewEncoder(w).Encode(map[string]any{"volumeID": "vol-1", "name": "data"})
	}))
	defer server.Close()

	client, err := NewClient(&SandboxOpts{APIKey: "k", APIURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}

	quota := int64(5)
	if _, err := client.UpdateVolumeQuota(context.Background(), "vol-1", &quota); err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodPatch {
		t.Errorf("method = %q, want PATCH", gotMethod)
	}
	var body map[string]any
	if err := json.Unmarshal(gotBody, &body); err != nil {
		t.Fatal(err)
	}
	if body["quotaSizeGiB"] != float64(5) || body["quotaInodes"] != float64(5000) {
		t.Fatalf("quota body = %#v, want quotaSizeGiB=5 quotaInodes=5000", body)
	}

	// nil quota sends an empty body like JS/Python (quotaSizeGiB undefined).
	if _, err := client.UpdateVolumeQuota(context.Background(), "vol-1", nil); err != nil {
		t.Fatal(err)
	}
	var empty map[string]any
	if err := json.Unmarshal(gotBody, &empty); err != nil {
		t.Fatal(err)
	}
	if len(empty) != 0 {
		t.Fatalf("nil quota body = %#v, want {}", empty)
	}

	// Zero is an explicit value, not "unset": it is sent like JS/Python
	// (quotaSizeGiB 0, quotaInodes 0), not omitted.
	zero := int64(0)
	if _, err := client.UpdateVolumeQuota(context.Background(), "vol-1", &zero); err != nil {
		t.Fatal(err)
	}
	var zeroBody map[string]any
	if err := json.Unmarshal(gotBody, &zeroBody); err != nil {
		t.Fatal(err)
	}
	if zeroBody["quotaSizeGiB"] != float64(0) || zeroBody["quotaInodes"] != float64(0) {
		t.Fatalf("zero quota body = %#v, want {quotaSizeGiB:0 quotaInodes:0}", zeroBody)
	}

	// Negative quotas are rejected client-side, matching the Java SDK.
	negative := int64(-1)
	if _, err := client.UpdateVolumeQuota(context.Background(), "vol-1", &negative); err == nil {
		t.Fatal("negative quota unexpectedly accepted")
	}
}

func TestVolumeCreateRejectsNegativeQuota(t *testing.T) {
	client, err := NewClient(&SandboxOpts{APIKey: "k", APIURL: "https://example.invalid"})
	if err != nil {
		t.Fatal(err)
	}
	negative := int64(-1)
	if _, err := client.CreateVolume(context.Background(), "vol-1", &negative); err == nil {
		t.Fatal("negative quota unexpectedly accepted")
	}
}
