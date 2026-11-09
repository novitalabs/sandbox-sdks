package core

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
)

// Novita is the high-level Go SDK facade. Namespaces are intentionally small
// value wrappers so callers can keep the same shape as the JS/Python SDKs.
type Novita struct {
	Client   *Client
	Sandbox  *SandboxNamespace
	Volume   *VolumeNamespace
	Secret   *SecretNamespace
	Template *TemplateNamespace
	Job      *JobClient
}

type SandboxNamespace struct{ client *Client }
type VolumeNamespace struct{ client *Client }
type SecretNamespace struct{ client *Client }
type TemplateNamespace struct{ client *TemplateClient }

func NewNovita(opts *SandboxOpts) (*Novita, error) {
	c, err := NewClient(opts)
	if err != nil {
		return nil, err
	}
	return &Novita{Client: c, Sandbox: &SandboxNamespace{client: c}, Volume: &VolumeNamespace{client: c}, Secret: &SecretNamespace{client: c}, Template: &TemplateNamespace{client: c.Templates()}, Job: c.Jobs()}, nil
}

func (t *TemplateNamespace) List(ctx context.Context, o *TemplateListOptions) (*TemplateListResult, error) {
	return t.client.List(ctx, o)
}
func (t *TemplateNamespace) Get(ctx context.Context, id string) (*TemplateInfo, error) {
	return t.client.Get(ctx, id)
}
func (t *TemplateNamespace) Delete(ctx context.Context, id string) error {
	return t.client.Delete(ctx, id)
}
func (t *TemplateNamespace) Build(ctx context.Context, name string, o BuildOptions) (*BuildInfo, error) {
	return t.client.Build(ctx, name, o)
}
func (t *TemplateNamespace) BuildTemplate(ctx context.Context, name string, b *TemplateBuilder, o BuildOptions) (*BuildInfo, error) {
	return t.client.BuildTemplate(ctx, name, b, o)
}
func (t *TemplateNamespace) BuildInBackground(ctx context.Context, name string, o BuildOptions) (*BuildInfo, error) {
	return t.client.BuildInBackground(ctx, name, o)
}
func (t *TemplateNamespace) BuildTemplateInBackground(ctx context.Context, name string, b *TemplateBuilder, o BuildOptions) (*BuildInfo, error) {
	return t.client.BuildTemplateInBackground(ctx, name, b, o)
}
func (t *TemplateNamespace) BuildStatus(ctx context.Context, templateID, buildID string) (*BuildStatus, error) {
	return t.client.BuildStatus(ctx, templateID, buildID)
}
func (t *TemplateNamespace) Exists(ctx context.Context, alias string) (bool, error) {
	return t.client.Exists(ctx, alias)
}
func (t *TemplateNamespace) AssignTags(ctx context.Context, target string, tags []string) (*TemplateTagInfo, error) {
	return t.client.AssignTags(ctx, target, tags)
}
func (t *TemplateNamespace) RemoveTags(ctx context.Context, name string, tags []string) error {
	return t.client.RemoveTags(ctx, name, tags)
}
func (t *TemplateNamespace) Tags(ctx context.Context, id string) ([]TemplateTag, error) {
	return t.client.Tags(ctx, id)
}
func (t *TemplateNamespace) Paginator(opts TemplateListOptions) Paginator[TemplateInfo] {
	return t.client.Paginator(opts)
}

func (v *VolumeNamespace) Create(ctx context.Context, name string, quotaGiB *int64) (*VolumeInfo, error) {
	return v.client.CreateVolume(ctx, name, quotaGiB)
}
func (v *VolumeNamespace) Get(ctx context.Context, id string) (*VolumeInfo, error) {
	return v.client.GetVolume(ctx, id)
}
func (v *VolumeNamespace) List(ctx context.Context) ([]VolumeInfo, error) {
	return v.client.ListVolumes(ctx)
}
func (v *VolumeNamespace) Delete(ctx context.Context, id string) (bool, error) {
	return v.client.DeleteVolume(ctx, id)
}
func (v *VolumeNamespace) UpdateQuota(ctx context.Context, id string, quotaGiB *int64) (*VolumeInfo, error) {
	return v.client.UpdateVolumeQuota(ctx, id, quotaGiB)
}
func (s *SecretNamespace) Create(ctx context.Context, p SecretWriteParams) (*SecretBinding, error) {
	return s.client.CreateSecret(ctx, p)
}
func (s *SecretNamespace) List(ctx context.Context) ([]SecretBinding, error) {
	return s.client.ListSecrets(ctx)
}
func (s *SecretNamespace) Get(ctx context.Context, name string) (*SecretBinding, error) {
	return s.client.GetSecret(ctx, name)
}
func (s *SecretNamespace) Update(ctx context.Context, p SecretWriteParams) (*SecretBinding, error) {
	return s.client.UpdateSecret(ctx, p)
}
func (s *SecretNamespace) Delete(ctx context.Context, name string) (string, error) {
	return s.client.DeleteSecret(ctx, name)
}

func (n *Novita) Create(ctx context.Context, opts *SandboxOpts) (*Sandbox, error) {
	return CreateSandbox(ctx, n.Client.inheritOptions(opts))
}
func (n *Novita) Connect(ctx context.Context, id string, opts *SandboxOpts) (*Sandbox, error) {
	return Connect(ctx, id, n.Client.inheritOptions(opts))
}

func (s *SandboxNamespace) Create(ctx context.Context, opts *SandboxOpts) (*Sandbox, error) {
	return s.clientSandbox(ctx, opts)
}
func (s *SandboxNamespace) Connect(ctx context.Context, id string, opts *SandboxOpts) (*Sandbox, error) {
	return Connect(ctx, id, s.client.inheritOptions(opts))
}
func (s *SandboxNamespace) Get(ctx context.Context, id string) (*SandboxInfo, error) {
	return s.client.GetSandboxInfo(ctx, id)
}
func (s *SandboxNamespace) List(ctx context.Context, query string) ([]SandboxInfo, error) {
	return s.client.ListSandboxes(ctx, query)
}
func (s *SandboxNamespace) Kill(ctx context.Context, id string) (bool, error) {
	return s.client.KillSandbox(ctx, id)
}
func (s *SandboxNamespace) Pause(ctx context.Context, id string) (bool, error) {
	return s.client.PauseSandbox(ctx, id)
}
func (s *SandboxNamespace) PauseWithOptions(ctx context.Context, id string, opts PauseOptions) (bool, error) {
	return s.client.PauseSandboxWithOptions(ctx, id, opts)
}
func (s *SandboxNamespace) SetTimeout(ctx context.Context, id string, timeoutMs int64) error {
	return s.client.SetSandboxTimeout(ctx, id, timeoutMs)
}
func (s *SandboxNamespace) Resize(ctx context.Context, id string, memoryMiB int64) error {
	return s.client.ResizeSandbox(ctx, id, memoryMiB)
}
func (s *SandboxNamespace) Reset(ctx context.Context, id string, payload map[string]interface{}) (bool, error) {
	return s.client.ResetSandbox(ctx, id, payload)
}
func (s *SandboxNamespace) SetNetwork(ctx context.Context, id string, network *SandboxNetworkConfig) error {
	return s.client.SetSandboxNetwork(ctx, id, network)
}
func (s *SandboxNamespace) Metrics(ctx context.Context, id, query string) ([]SandboxMetrics, error) {
	return s.client.GetSandboxMetrics(ctx, id, query)
}
func (s *SandboxNamespace) Quota(ctx context.Context) (*SandboxQuota, error) {
	return s.client.GetSandboxQuota(ctx)
}
func (s *SandboxNamespace) Events(ctx context.Context, query string) (*SandboxEventsResult, error) {
	return s.client.GetSandboxEvents(ctx, query)
}
func (s *SandboxNamespace) CloneWithOptions(ctx context.Context, id string, opts CloneOptions) (*CloneSandboxResponse, error) {
	req := CloneSandboxRequest{Count: opts.Count, Strict: opts.Strict}
	if req.Count == 0 {
		req.Count = 1
	}
	if opts.NodeID != "" {
		req.NodeID = opts.NodeID
	}
	if opts.Timeout > 0 {
		req.Timeout = int32(opts.Timeout.Seconds())
	}
	return s.client.CloneSandbox(ctx, id, req)
}
func (s *SandboxNamespace) CreateSnapshot(ctx context.Context, id string) (*SnapshotInfo, error) {
	return s.client.CreateSnapshot(ctx, id)
}
func (s *SandboxNamespace) ListSnapshotsTyped(ctx context.Context, opts SnapshotListOptions) ([]SnapshotInfo, error) {
	q := url.Values{}
	if opts.SourceSandboxID != "" {
		q.Set("sandboxID", opts.SourceSandboxID)
	}
	if opts.Limit > 0 {
		q.Set("limit", fmt.Sprint(opts.Limit))
	}
	if opts.Cursor != "" {
		q.Set("nextToken", opts.Cursor)
	}
	return s.client.ListSnapshots(ctx, q.Encode())
}
func (s *SandboxNamespace) SnapshotPaginator(opts SnapshotListOptions) Paginator[SnapshotInfo] {
	return s.client.SnapshotPaginator(opts)
}
func (s *SandboxNamespace) DeleteSnapshot(ctx context.Context, id string) (bool, error) {
	return s.client.DeleteSnapshot(ctx, id)
}

func (s *SandboxNamespace) Paginator(opts SandboxListOptions) Paginator[SandboxInfo] {
	return s.client.SandboxPaginator(opts)
}

func (s *SandboxNamespace) ListWithOptions(ctx context.Context, opts SandboxListOptions) ([]SandboxInfo, error) {
	return s.client.SandboxPaginator(opts).Next(ctx)
}

func (s *SandboxNamespace) ResetWithOptions(ctx context.Context, id string, opts ResetOptions) (bool, error) {
	return s.client.ResetSandbox(ctx, id, resetPayload(opts))
}

func (s *SandboxNamespace) MetricsWithOptions(ctx context.Context, id string, opts SandboxMetricsOptions) ([]SandboxMetrics, error) {
	q := url.Values{}
	// JS legacy metrics ignores start/end entirely (the legacy endpoint takes
	// no query parameters); modern clusters filter by epoch second.
	if !s.client.connection.IsLegacyDomain {
		if opts.Start != nil {
			q.Set("start", fmt.Sprint(opts.Start.Unix()))
		}
		if opts.End != nil {
			q.Set("end", fmt.Sprint(opts.End.Unix()))
		}
	}
	return s.client.GetSandboxMetrics(ctx, id, q.Encode())
}

func (s *SandboxNamespace) EventsWithOptions(ctx context.Context, opts SandboxEventsOptions) (*SandboxEventsResult, error) {
	q := url.Values{}
	setInt64 := func(key string, value *int64) {
		if value != nil {
			q.Set(key, strconv.FormatInt(*value, 10))
		}
	}
	setInt := func(key string, value *int) {
		if value != nil {
			q.Set(key, strconv.Itoa(*value))
		}
	}
	setInt64("startTime", opts.StartTime)
	setInt64("endTime", opts.EndTime)
	if opts.SandboxID != "" {
		q.Set("sandboxID", opts.SandboxID)
	}
	if opts.TemplateID != "" {
		q.Set("templateID", opts.TemplateID)
	}
	if opts.Events != "" {
		q.Set("events", opts.Events)
	}
	if opts.State != "" {
		q.Set("state", opts.State)
	}
	if opts.OrderAsc != nil {
		q.Set("orderAsc", strconv.FormatBool(*opts.OrderAsc))
	}
	setInt("offset", opts.Offset)
	setInt("limit", opts.Limit)
	return s.client.GetSandboxEvents(ctx, q.Encode())
}

func (s *SandboxNamespace) MountVolume(ctx context.Context, id string, opts VolumeMountOptions) (*SandboxInfo, error) {
	return s.client.MountVolume(ctx, id, opts)
}

func (s *SandboxNamespace) UnmountVolume(ctx context.Context, id, path string, force bool) (*SandboxInfo, error) {
	return s.client.UnmountVolume(ctx, id, path, force)
}
func (s *SandboxNamespace) CommitLegacy(ctx context.Context, id, alias string) (*TemplateInfo, error) {
	if !s.client.connection.IsLegacyDomain {
		return nil, &SandboxError{Message: "sandbox commit is only supported on legacy domains"}
	}
	return s.client.CommitSandbox(ctx, id, alias)
}

func (s *SandboxNamespace) clientSandbox(ctx context.Context, opts *SandboxOpts) (*Sandbox, error) {
	return CreateSandbox(ctx, s.client.inheritOptions(opts))
}

func (c *Client) inheritOptions(call *SandboxOpts) *SandboxOpts {
	out := SandboxOpts{}
	if call != nil {
		out = *call
	}
	cfg := c.connection
	if out.APIKey == "" {
		out.APIKey = cfg.APIKey
	}
	if out.AccessToken == "" {
		out.AccessToken = cfg.AccessToken
	}
	if out.Domain == "" {
		out.Domain = cfg.Domain
	}
	if out.APIURL == "" {
		out.APIURL = cfg.APIURL
	}
	if out.SandboxURL == "" {
		out.SandboxURL = cfg.SandboxURL
	}
	if out.SandboxDomain == "" && c.opts != nil {
		out.SandboxDomain = c.opts.SandboxDomain
	}
	if out.Debug == nil {
		out.Debug = Bool(cfg.Debug)
	}
	if out.RequestTimeoutMs == nil {
		timeout := cfg.RequestTimeoutMs
		out.RequestTimeoutMs = &timeout
	}
	if out.Logger == nil {
		out.Logger = cfg.Logger
	}
	headers := map[string]string{}
	for k, v := range cfg.Headers {
		headers[k] = v
	}
	for k, v := range out.Headers {
		headers[k] = v
	}
	out.Headers = headers
	return &out
}

func (t *TemplateNamespace) BuildStatusWithOptions(ctx context.Context, templateID, buildID string, opts *BuildStatusOptions) (*BuildStatus, error) {
	return t.client.BuildStatusWithOptions(ctx, templateID, buildID, opts)
}
