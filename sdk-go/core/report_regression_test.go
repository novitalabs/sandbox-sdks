package core

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

// Keep the real SDK request/response path while replacing only network I/O.
type reportTransport func(*http.Request) (*http.Response, error)

func (f reportTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func reportClient(legacy bool, transport reportTransport) *Client {
	return &Client{
		connection: &ConnectionConfig{APIURL: "https://api.example.test", Domain: "sandbox.example.test", APIKey: "test-key", RequestTimeoutMs: 60000, IsLegacyDomain: legacy},
		http:       &http.Client{Transport: transport},
	}
}

func reportResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}

func TestReportSandboxIdentity(t *testing.T) {
	for _, tc := range []struct {
		name, id, clientID, want string
		legacy                   bool
	}{
		{"legacy short", "demo", "node", "demo-node", true},
		{"legacy complete", "demo-node", "node", "demo-node", true},
		{"legacy no client", "demo", "", "demo", true},
		{"modern", "demo-other", "node", "demo-other", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := reportClient(tc.legacy, func(r *http.Request) (*http.Response, error) {
				raw, _ := json.Marshal(SandboxInfo{SandboxID: tc.id, ClientID: tc.clientID, EnvdVersion: "0.5.7"})
				return reportResponse(200, string(raw)), nil
			})
			for name, call := range map[string]func() (*SandboxInfo, error){
				"create": func() (*SandboxInfo, error) {
					return c.CreateSandbox(context.Background(), &SandboxOpts{Template: "base"})
				},
				"connect": func() (*SandboxInfo, error) { return c.ConnectSandbox(context.Background(), tc.want, nil) },
				"info":    func() (*SandboxInfo, error) { return c.GetSandboxInfo(context.Background(), tc.want) },
			} {
				t.Run(name, func(t *testing.T) {
					info, err := call()
					if err != nil {
						t.Fatal(err)
					}
					if info.SandboxID != tc.want {
						t.Errorf("response ID = %q, want %q", info.SandboxID, tc.want)
					}
					s, err := NewSandboxFromConfig(c.Connection(), info)
					if err != nil {
						t.Fatal(err)
					}
					if s.ID() != tc.want {
						t.Errorf("handle ID = %q, want %q", s.ID(), tc.want)
					}
				})
			}
		})
	}
}

func TestReportLegacyPagination(t *testing.T) {
	for _, wrapped := range []bool{false, true} {
		t.Run(map[bool]string{false: "array", true: "wrapper"}[wrapped], func(t *testing.T) {
			calls := 0
			c := reportClient(true, func(r *http.Request) (*http.Response, error) {
				calls++
				if r.Method != "GET" || r.URL.Path != "/v2/sandboxes" {
					t.Errorf("request = %s %s", r.Method, r.URL.Path)
				}
				q := r.URL.Query()
				if q.Get("state") != "running,paused" || q.Get("limit") != "2" || q.Get("metadata") != "team=sdk+go" {
					t.Errorf("query = %v", q)
				}
				if calls == 2 && q.Get("nextToken") != "next/token" {
					t.Errorf("next token = %q", q.Get("nextToken"))
				}
				body := `[{"sandboxID":"one","clientID":"node","state":"running"},{"sandboxID":"two-node","clientID":"node","state":"paused"}]`
				token := "next/token"
				if calls > 1 {
					body = `[{"sandboxID":"three","clientID":"node","state":"paused"}]`
					token = ""
				}
				if wrapped {
					body = `{"sandboxes":` + body + `,"nextToken":"` + token + `"}`
				}
				res := reportResponse(200, body)
				if !wrapped {
					res.Header.Set("X-Next-Token", token)
				}
				return res, nil
			})
			p := c.SandboxPaginator(SandboxListOptions{States: []SandboxState{SandboxStateRunning, SandboxStatePaused}, Limit: 2, Metadata: map[string]string{"team": "sdk go"}})
			var ids []string
			for p.HasNext() && calls < 3 {
				items, err := p.Next(context.Background())
				if err != nil {
					t.Fatal(err)
				}
				for _, item := range items {
					ids = append(ids, item.SandboxID)
				}
			}
			if !reflect.DeepEqual(ids, []string{"one-node", "two-node", "three-node"}) || calls != 2 || p.HasNext() {
				t.Fatalf("IDs = %v, requests = %d, hasNext = %v", ids, calls, p.HasNext())
			}
		})
	}
}

func TestReportLegacyClone(t *testing.T) {
	calls := 0
	c := reportClient(true, func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Method != "POST" || r.URL.Path != "/sandboxes/source-node/clone" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(body, map[string]any{"count": float64(2), "strict": true, "timeout": float64(90), "nodeID": "destination"}) {
			t.Errorf("body = %v", body)
		}
		return reportResponse(200, `{"count":2,"snapshotTemplateId":"snapshot","sandboxes":[{"sandboxID":"one","clientID":"a","envdVersion":"0.5.7","envdAccessToken":"envd-one","trafficAccessToken":"traffic-one"},{"sandboxID":"two-b","clientID":"b","envdVersion":"0.5.8","envdAccessToken":"envd-two","trafficAccessToken":"traffic-two"}]}`), nil
	})
	out, err := c.CloneSandbox(context.Background(), "source-node", CloneSandboxRequest{Count: 2, Strict: true, Timeout: 90, NodeID: "destination"})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || out.Count != 2 || out.SnapshotTemplateId != "snapshot" || len(out.Sandboxes) != 2 {
		t.Fatalf("clone = %+v, calls = %d", out, calls)
	}
	for i, want := range []string{"one-a", "two-b"} {
		info := out.Sandboxes[i]
		s, err := NewSandboxFromConfig(c.Connection(), &info)
		if err != nil {
			t.Fatal(err)
		}
		if info.SandboxID != want || s.ID() != want || s.TrafficAccessToken() == "" || info.EnvdAccessToken == "" {
			t.Errorf("clone info = %+v; handle = %s", info, s.ID())
		}
	}
}

func TestReportLegacyTemplates(t *testing.T) {
	for _, status := range []int{204, 400} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			c := reportClient(true, func(r *http.Request) (*http.Response, error) {
				if r.Method == "GET" {
					if r.URL.Path != "/v2/templates" || r.URL.Query().Get("template_type") != "snapshot_template" || r.URL.Query().Get("page") != "2" || r.URL.Query().Get("limit") != "3" {
						t.Errorf("list URL = %s", r.URL)
					}
					return reportResponse(200, `{"templates":[{"templateID":"snapshot","templateType":"snapshot_template"}],"page":2,"limit":3,"total":4,"totalPages":2}`), nil
				}
				if r.Method != "DELETE" || r.URL.Path != "/templates/snapshot" {
					t.Errorf("delete = %s %s", r.Method, r.URL)
				}
				return reportResponse(status, `{"message":"snapshot dependency"}`), nil
			})
			list, err := c.Templates().List(context.Background(), &TemplateListOptions{TemplateType: "snapshot_template", Page: 2, Limit: 3})
			if err != nil {
				t.Error(err)
			}
			if err == nil && (len(list.Templates) != 1 || list.Page != 2 || list.Total != 4) {
				t.Fatalf("list = %+v", list)
			}
			err = c.Templates().Delete(context.Background(), "snapshot")
			if status == 204 && err != nil {
				t.Fatal(err)
			}
			var invalid *InvalidArgumentError
			if status == 400 && !errors.As(err, &invalid) {
				t.Fatalf("delete error = %T %v, want server 400 mapping", err, err)
			}
		})
	}
}

func TestReportResetUpdatesHandleToken(t *testing.T) {
	c := reportClient(false, func(r *http.Request) (*http.Response, error) {
		if r.Method != "POST" || r.URL.Path != "/sandboxes/demo/reset" {
			t.Errorf("reset = %s %s", r.Method, r.URL)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["resume"] != true || body["allowPublicTraffic"] != false {
			t.Errorf("body = %v", body)
		}
		return reportResponse(200, `{"trafficAccessToken":"new-token"}`), nil
	})
	s, err := newSandboxFromInfo(c, &SandboxInfo{SandboxID: "demo", TrafficAccessToken: "old-token"}, &SandboxOpts{})
	if err != nil {
		t.Fatal(err)
	}
	resetter, ok := any(s).(interface {
		Reset(context.Context, ResetOptions) (bool, error)
	})
	if !ok {
		t.Fatal("sandbox has no reset API that can refresh its traffic token")
	}
	success, err := resetter.Reset(context.Background(), ResetOptions{AllowPublicTraffic: Bool(false)})
	if err != nil || !success {
		t.Fatalf("reset = %v %v", success, err)
	}
	if s.TrafficAccessToken() != "new-token" {
		t.Fatalf("token = %q", s.TrafficAccessToken())
	}
}
