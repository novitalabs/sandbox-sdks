package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func jobFixture(t *testing.T, handler http.HandlerFunc) *JobClient {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	n, err := NewNovita(&SandboxOpts{APIKey: "fixture", APIURL: server.URL, Domain: "us-phx-1.sandbox.novita.ai"})
	if err != nil {
		t.Fatal(err)
	}
	return n.Job
}

func TestJobCreateWireAndValidation(t *testing.T) {
	calls := 0
	job := jobFixture(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/v1/migrations" || r.Method != "POST" || r.Header.Get("X-API-Key") != "fixture" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL)
		}
		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		expected := `{"resources":[{"id":"a"},{"id":"b"}],"resource_type":"sandbox","source_region":"source"}`
		if calls == 2 {
			expected = `{"resources":[{"id":"a","template_alias":" a "},{"id":"b","template_alias":" b "}],"resource_type":"template","source_region":"source"}`
		}
		var want map[string]interface{}
		_ = json.Unmarshal([]byte(expected), &want)
		if !reflect.DeepEqual(body, want) {
			t.Errorf("got %#v want %#v", body, want)
		}
		fmt.Fprint(w, `{"jobId":"j-1"}`)
	})
	id, err := job.MigrateSandboxes(context.Background(), []string{"a", "a", "b"}, JobCreateOptions{SourceRegion: "source"})
	if err != nil || id != "j-1" {
		t.Fatalf("%s %v", id, err)
	}
	_, err = job.DistributeTemplates(context.Background(), []string{"a", "b"}, JobCreateOptions{SourceRegion: "source", AliasForResource: func(id string) string { return " " + id + " " }})
	if err != nil {
		t.Fatal(err)
	}
	alias := "same"
	_, err = job.DistributeTemplates(context.Background(), []string{"a", "b"}, JobCreateOptions{SourceRegion: "source", TemplateAlias: &alias})
	if err == nil {
		t.Fatal("shared distribution alias accepted")
	}
	_, err = job.DistributeTemplate(context.Background(), "a", JobCreateOptions{SourceRegion: "source"})
	if err == nil {
		t.Fatal("missing distribution alias accepted")
	}
	blank := " \t"
	_, err = job.MigrateSandbox(context.Background(), "a", JobCreateOptions{SourceRegion: "source", TemplateAlias: &blank})
	if err == nil {
		t.Fatal("blank alias accepted")
	}
	if calls != 2 {
		t.Fatalf("invalid input sent %d requests", calls)
	}
}

func TestJobGetListCancelAndUnknownStatuses(t *testing.T) {
	job := jobFixture(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/migrations/cancel":
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			if r.Method != "POST" || body["job_id"] != "a/b ?" {
				t.Errorf("cancel: %s %#v", r.Method, body)
			}
			fmt.Fprint(w, `{"jobId":"a/b ?","status":"reverting","items":[{"itemId":"i","resourceType":"base","resourceId":"tpl","status":"future"}]}`)
		case r.URL.Query().Get("job_id") != "":
			if r.URL.Query().Get("job_id") != "a/b ?" {
				t.Error(r.URL)
			}
			fmt.Fprint(w, `{"items":[{"jobId":"a/b ?","status":"future","total":2}]}`)
		default:
			q := r.URL.Query()
			if q.Get("source_region") != "s" || q.Get("target_region") != "t" || q.Get("limit") != "0" || q.Get("resource_id") != "r" || q.Get("status") != "reverting" || q.Get("type") != "customer" || q.Get("cursor") != "next" {
				t.Error(q)
			}
			fmt.Fprint(w, `{"items":[{"jobId":"j","status":"migrating"}],"nextCursor":"page2"}`)
		}
	})
	got, err := job.Get(context.Background(), "a/b ?")
	if err != nil || got.Status != "future" || got.Total != 2 || got.Items == nil {
		t.Fatalf("%+v %v", got, err)
	}
	got, err = job.Cancel(context.Background(), "a/b ?")
	if err != nil || got.Status != JobReverting || len(got.Items) != 1 || got.Items[0].Status != "future" {
		t.Fatalf("%+v %v", got, err)
	}
	zero := 0
	page, err := job.List(context.Background(), JobListOptions{SourceRegion: "s", TargetRegion: "t", Limit: &zero, ResourceID: "r", Status: JobReverting, Type: JobCustomer, Cursor: "next"})
	if err != nil || page.NextCursor != "page2" || page.Items[0].Items == nil {
		t.Fatalf("%+v %v", page, err)
	}
}

func TestJobReasonErrors(t *testing.T) {
	for _, tc := range []struct {
		status int
		reason string
		kind   interface{}
	}{
		{400, "RESOURCE_NOT_FOUND", &NotFoundError{}}, {400, "MIGRATION_TEMPLATE_ALIAS_CONFLICT", &TemplateAliasConflictError{}},
		{409, "MIGRATION_RESOURCE_ALREADY_MIGRATING", &ResourceAlreadyMigratingError{}}, {409, "MIGRATION_PAST_CANCEL_LINE", &CancelTooLateError{}},
		{409, "MIGRATION_SHARED_DEPENDENCY", &SharedDependencyError{}}, {409, "FUTURE_CONFLICT", &MigrationConflictError{}},
		{501, "", &MigrationNotSupportedError{}}, {400, "VALIDATOR_PARAM", &InvalidArgumentError{}}, {401, "", &AuthenticationError{}},
	} {
		t.Run(tc.reason+fmt.Sprint(tc.status), func(t *testing.T) {
			job := jobFixture(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_ = json.NewEncoder(w).Encode(map[string]string{"reason": tc.reason, "message": "detail"})
			})
			_, err := job.Get(context.Background(), "id")
			if reflect.TypeOf(err) != reflect.TypeOf(tc.kind) {
				t.Fatalf("got %T %v want %T", err, err, tc.kind)
			}
			if conflict, ok := err.(*MigrationConflictError); ok && conflict.Reason != "FUTURE_CONFLICT" {
				t.Error(conflict)
			}
		})
	}
}

func TestJobRateLimitPreservesDiagnosticAndRetryAfter(t *testing.T) {
	job := jobFixture(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "5")
		w.WriteHeader(http.StatusTooManyRequests)
		fmt.Fprint(w, `{"reason":"RATE_LIMITED","message":"migration concurrency limit"}`)
	})
	_, err := job.Get(context.Background(), "j")
	var limited *RateLimitError
	if !errors.As(err, &limited) || limited.RetryAfter != "5" ||
		!strings.Contains(err.Error(), "RATE_LIMITED") || !strings.Contains(err.Error(), "migration concurrency limit") {
		t.Fatalf("rate limit error lost details: %v", err)
	}
}

func TestJobWaitTerminalAndDeadline(t *testing.T) {
	var calls atomic.Int32
	job := jobFixture(t, func(w http.ResponseWriter, r *http.Request) {
		status := "reverting"
		if calls.Add(1) > 1 {
			status = "completed"
		}
		fmt.Fprintf(w, `{"items":[{"jobId":"j","status":%q,"failed":1,"total":2}]}`, status)
	})
	var progress []JobStatus
	got, err := job.Wait(context.Background(), "j", JobWaitOptions{PollInterval: time.Millisecond, OnProgress: func(s MigrationStatus) { progress = append(progress, s.Status) }})
	if err != nil || got.Failed != 1 || !reflect.DeepEqual(progress, []JobStatus{JobReverting, JobCompleted}) {
		t.Fatalf("%+v %v %v", got, err, progress)
	}
	for _, status := range []string{"failed", "cancelled"} {
		t.Run(status, func(t *testing.T) {
			j := jobFixture(t, func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprintf(w, `{"items":[{"jobId":"j","status":%q,"items":[{"itemId":"i"}]}]}`, status)
			})
			_, e := j.Wait(context.Background(), "j", JobWaitOptions{})
			if status == "failed" {
				var f *JobFailedError
				if !errors.As(e, &f) || len(f.Status.Items) != 1 {
					t.Fatal(e)
				}
			} else {
				var c *JobCancelledError
				if !errors.As(e, &c) || len(c.Status.Items) != 1 {
					t.Fatal(e)
				}
			}
		})
	}
	t.Run("in-flight request", func(t *testing.T) {
		j := jobFixture(t, func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() })
		timeout := 40 * time.Millisecond
		start := time.Now()
		_, err := j.Wait(context.Background(), "j", JobWaitOptions{Timeout: &timeout})
		var timed *TimeoutError
		if !errors.As(err, &timed) || time.Since(start) > time.Second {
			t.Fatalf("%v after %v", err, time.Since(start))
		}
	})
	t.Run("callback consumes budget", func(t *testing.T) {
		timeout := 20 * time.Millisecond
		_, err := job.Wait(context.Background(), "j", JobWaitOptions{Timeout: &timeout, OnProgress: func(MigrationStatus) { time.Sleep(35 * time.Millisecond) }})
		var timed *TimeoutError
		if !errors.As(err, &timed) || !strings.Contains(err.Error(), "completed") {
			t.Fatal(err)
		}
	})
	t.Run("zero budget makes no request", func(t *testing.T) {
		before := calls.Load()
		zero := time.Duration(0)
		_, err := job.Wait(context.Background(), "j", JobWaitOptions{Timeout: &zero})
		if err == nil || calls.Load() != before {
			t.Fatal(err)
		}
	})
}

func TestJobWaitContextAndPollingBounds(t *testing.T) {
	var calls atomic.Int32
	job := jobFixture(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		fmt.Fprint(w, `{"items":[{"jobId":"j","status":"completed"}]}`)
	})
	if _, err := job.Wait(nil, "j", JobWaitOptions{}); err != nil {
		t.Fatal(err)
	}
	zero := time.Duration(0)
	if _, err := job.Wait(nil, "j", JobWaitOptions{Timeout: &zero}); err == nil || calls.Load() != 1 {
		t.Fatalf("zero timeout: calls=%d error=%v", calls.Load(), err)
	}
	t.Run("cancel active request", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		job := jobFixture(t, func(w http.ResponseWriter, r *http.Request) {
			cancel()
			<-r.Context().Done()
		})
		if _, err := job.Wait(ctx, "j", JobWaitOptions{}); !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	})
	t.Run("clip polling delay", func(t *testing.T) {
		var polls atomic.Int32
		job := jobFixture(t, func(w http.ResponseWriter, r *http.Request) {
			polls.Add(1)
			fmt.Fprint(w, `{"items":[{"jobId":"j","status":"migrating"}]}`)
		})
		timeout := 100 * time.Millisecond
		start := time.Now()
		_, err := job.Wait(context.Background(), "j", JobWaitOptions{Timeout: &timeout, PollInterval: time.Minute})
		var timed *TimeoutError
		if !errors.As(err, &timed) || polls.Load() != 1 || time.Since(start) > time.Second {
			t.Fatalf("polls=%d elapsed=%v error=%v", polls.Load(), time.Since(start), err)
		}
	})
}
