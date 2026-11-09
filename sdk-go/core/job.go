package core

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// JobClient migrates sandboxes and distributes templates on modern Novita
// clusters. The target region comes from the configured Domain or APIURL.
type JobClient struct{ client *Client }

func (c *Client) Jobs() *JobClient { return &JobClient{client: c} }
func NewJob(opts *SandboxOpts) (*JobClient, error) {
	c, err := NewClient(opts)
	if err != nil {
		return nil, err
	}
	return c.Jobs(), nil
}

func (j *JobClient) MigrateSandbox(ctx context.Context, id string, opts JobCreateOptions) (string, error) {
	return j.create(ctx, "sandbox", []string{id}, opts)
}
func (j *JobClient) MigrateSandboxes(ctx context.Context, ids []string, opts JobCreateOptions) (string, error) {
	return j.create(ctx, "sandbox", ids, opts)
}
func (j *JobClient) DistributeTemplate(ctx context.Context, id string, opts JobCreateOptions) (string, error) {
	return j.create(ctx, "template", []string{id}, opts)
}
func (j *JobClient) DistributeTemplates(ctx context.Context, ids []string, opts JobCreateOptions) (string, error) {
	return j.create(ctx, "template", ids, opts)
}

func (j *JobClient) create(ctx context.Context, kind string, ids []string, opts JobCreateOptions) (string, error) {
	invalid := func(reason string) (string, error) {
		return "", &InvalidArgumentError{Argument: "migration", Reason: reason}
	}
	if opts.SourceRegion == "" {
		return invalid("SourceRegion is required")
	}
	if len(ids) == 0 {
		return invalid("at least one resource ID is required")
	}
	if opts.TemplateAlias != nil && opts.AliasForResource != nil {
		return invalid("set only one of TemplateAlias and AliasForResource")
	}
	unique := make([]string, 0, len(ids))
	seen := map[string]bool{}
	for _, id := range ids {
		if id == "" {
			return invalid("resource IDs must not be empty")
		}
		if !seen[id] {
			unique = append(unique, id)
			seen[id] = true
		}
	}
	if kind == "template" && len(unique) > 1 && opts.TemplateAlias != nil {
		return invalid("multiple templates require AliasForResource")
	}
	resources := make([]map[string]string, 0, len(unique))
	for _, id := range unique {
		resource := map[string]string{"id": id}
		if kind == "template" || opts.TemplateAlias != nil || opts.AliasForResource != nil {
			alias := ""
			if opts.AliasForResource != nil {
				alias = opts.AliasForResource(id)
			} else if opts.TemplateAlias != nil {
				alias = *opts.TemplateAlias
			}
			if strings.TrimSpace(alias) == "" {
				return invalid("template alias must not be blank for resource " + id)
			}
			resource["template_alias"] = alias
		}
		resources = append(resources, resource)
	}
	var response struct {
		JobID string `json:"jobId"`
	}
	err := j.request(ctx, "POST", "", nil, map[string]interface{}{"resources": resources, "resource_type": kind, "source_region": opts.SourceRegion}, &response)
	if err != nil {
		return "", err
	}
	if response.JobID == "" {
		return "", &SandboxError{Message: "migration accept response is missing jobId"}
	}
	return response.JobID, nil
}

func (j *JobClient) Get(ctx context.Context, id string) (*MigrationStatus, error) {
	if id == "" {
		return nil, &InvalidArgumentError{Argument: "jobID", Reason: "is required"}
	}
	var page JobPage
	if err := j.request(ctx, "GET", "", url.Values{"job_id": {id}}, nil, &page); err != nil {
		return nil, err
	}
	normalizeJobPage(&page)
	if len(page.Items) == 0 {
		return nil, &NotFoundError{Resource: "migration job", ID: id}
	}
	return &page.Items[0], nil
}
func (j *JobClient) List(ctx context.Context, opts JobListOptions) (*JobPage, error) {
	q := url.Values{}
	for key, value := range map[string]string{"resource_id": opts.ResourceID, "type": string(opts.Type), "status": string(opts.Status), "source_region": opts.SourceRegion, "target_region": opts.TargetRegion, "cursor": opts.Cursor} {
		if value != "" {
			q.Set(key, value)
		}
	}
	if opts.Limit != nil {
		q.Set("limit", strconv.Itoa(*opts.Limit))
	}
	var page JobPage
	err := j.request(ctx, "GET", "", q, nil, &page)
	if err != nil {
		return nil, err
	}
	normalizeJobPage(&page)
	return &page, nil
}

// Cancel accepts cooperative rollback; poll until reverting becomes terminal.
func (j *JobClient) Cancel(ctx context.Context, id string) (*MigrationStatus, error) {
	if id == "" {
		return nil, &InvalidArgumentError{Argument: "jobID", Reason: "is required"}
	}
	var status MigrationStatus
	err := j.request(ctx, "POST", "/cancel", nil, map[string]string{"job_id": id}, &status)
	if err != nil {
		return nil, err
	}
	normalizeJobStatus(&status)
	return &status, nil
}

// Wait polls within a total budget. It returns completed jobs even if individual
// items failed; failed/cancelled jobs return typed errors carrying their status.
func (j *JobClient) Wait(ctx context.Context, id string, opts JobWaitOptions) (*MigrationStatus, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if id == "" {
		return nil, &InvalidArgumentError{Argument: "jobID", Reason: "is required"}
	}
	timeout := 30 * time.Minute
	if opts.Timeout != nil {
		timeout = *opts.Timeout
	}
	interval := opts.PollInterval
	if interval == 0 {
		interval = 10 * time.Second
	}
	if timeout < 0 || interval < 0 {
		return nil, &InvalidArgumentError{Argument: "wait", Reason: "timeout must be nonnegative and poll interval positive"}
	}
	deadline := time.Now().Add(timeout)
	waitCtx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	var last *MigrationStatus
	timeoutError := func() error {
		detail := "waiting for migration job " + id
		if last != nil {
			detail += fmt.Sprintf("; last status was %s (completed=%d, failed=%d, cancelled=%d, total=%d)", last.Status, last.Completed, last.Failed, last.Cancelled, last.Total)
		}
		return &TimeoutError{Operation: detail, Duration: timeout.String()}
	}
	for {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if !time.Now().Before(deadline) {
			return nil, timeoutError()
		}
		status, err := j.Get(waitCtx, id)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			if !time.Now().Before(deadline) {
				return nil, timeoutError()
			}
			return nil, err
		}
		last = status
		if !time.Now().Before(deadline) {
			return nil, timeoutError()
		}
		if opts.OnProgress != nil {
			snapshot := *status
			snapshot.Items = append([]MigrationItemStatus{}, status.Items...)
			opts.OnProgress(snapshot)
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if !time.Now().Before(deadline) {
			return nil, timeoutError()
		}
		switch status.Status {
		case JobCompleted:
			return status, nil
		case JobFailed:
			return nil, &JobFailedError{Status: *status}
		case JobCancelled:
			return nil, &JobCancelledError{Status: *status}
		}
		delay := interval
		if remaining := time.Until(deadline); delay > remaining {
			delay = remaining
		}
		timer := time.NewTimer(delay)
		select {
		case <-waitCtx.Done():
			timer.Stop()
		case <-timer.C:
		}
	}
}

func (j *JobClient) request(ctx context.Context, method, suffix string, query url.Values, payload, out interface{}) error {
	if j.client.connection.IsLegacyDomain {
		return &UnsupportedError{Operation: "Job", Reason: "requires a modern domain"}
	}
	var body io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		body = bytes.NewReader(data)
	}
	response, err := j.client.do(ctx, method, "/v1/migrations"+suffix, query.Encode(), body)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		return err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return jobResponseError(response.StatusCode, data, response.Header.Get("Retry-After"))
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return &SandboxError{Message: "invalid migration response", Err: err}
	}
	return nil
}
func normalizeJobStatus(s *MigrationStatus) {
	if s.Items == nil {
		s.Items = []MigrationItemStatus{}
	}
}
func normalizeJobPage(p *JobPage) {
	if p.Items == nil {
		p.Items = []MigrationStatus{}
	}
	for i := range p.Items {
		normalizeJobStatus(&p.Items[i])
	}
}
