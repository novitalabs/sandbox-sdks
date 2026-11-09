package core

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
)

// Client is the control-plane REST client. It talks to the Novita Sandbox API
// and is authenticated with an API key or access token.
type Client struct {
	connection *ConnectionConfig
	http       *http.Client
	opts       *SandboxOpts
}

type cancelOnCloseBody struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (b *cancelOnCloseBody) Close() error {
	err := b.ReadCloser.Close()
	b.cancel()
	return err
}

// NewClient builds a control-plane client from explicit options. The options
// are resolved against the usual NOVITA_* environment variables.
func NewClient(opts *SandboxOpts) (*Client, error) {
	connection, err := NewConnectionConfig(opts)
	if err != nil {
		return nil, err
	}

	// Debug mode talks directly to local envd and does not need control-plane
	// authentication. Remote control-plane clients require an API key or access token.
	if connection.APIKey == "" && connection.AccessToken == "" && !connection.Debug {
		return nil, &AuthenticationError{
			Message: "API key or access token is required: set NOVITA_API_KEY / NOVITA_ACCESS_TOKEN or pass credentials in options",
		}
	}

	return &Client{
		connection: connection,
		http:       &http.Client{},
		opts:       cloneClientOptions(opts),
	}, nil
}

// Connection returns the resolved connection configuration.
func (c *Client) Connection() *ConnectionConfig {
	return c.connection
}

// do performs a control-plane request with the API key and SDK headers applied.
func (c *Client) do(ctx context.Context, method, path string, query string, body io.Reader) (*http.Response, error) {
	target := c.connection.APIURL + path
	if query != "" {
		target += "?" + query
	}

	ctx, cancel := c.connection.contextWithTimeout(ctx, nil)

	req, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		cancel()
		return nil, err
	}

	if c.connection.APIKey != "" {
		req.Header.Set("X-API-Key", c.connection.APIKey)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range c.connection.Headers {
		req.Header.Set(k, v)
	}
	if c.connection.AccessToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.connection.AccessToken)
	}

	res, err := c.http.Do(req)
	if err != nil {
		cancel()
		return nil, err
	}
	res.Body = &cancelOnCloseBody{ReadCloser: res.Body, cancel: cancel}
	return res, nil
}

// createSandboxRequest is the JSON body for POST /sandboxes.
type createSandboxRequest struct {
	TemplateID           string                   `json:"templateID,omitempty"`
	Image                string                   `json:"image,omitempty"`
	Timeout              int32                    `json:"timeout,omitempty"`
	AutoPause            bool                     `json:"autoPause,omitempty"`
	AutoResume           *SandboxAutoResumeConfig `json:"autoResume,omitempty"`
	Secure               *bool                    `json:"secure,omitempty"`
	AllowInternetAccess  *bool                    `json:"allow_internet_access,omitempty"`
	LegacyInternetAccess *bool                    `json:"allowInternetAccess,omitempty"`
	Network              *SandboxNetworkConfig    `json:"network,omitempty"`
	Metadata             map[string]string        `json:"metadata,omitempty"`
	EnvVars              map[string]string        `json:"envVars,omitempty"`
	Secrets              map[string]string        `json:"secrets,omitempty"`
	Mcp                  map[string]interface{}   `json:"mcp,omitempty"`
	VolumeMounts         []SandboxVolumeMount     `json:"volumeMounts,omitempty"`
	NodeID               string                   `json:"nodeID,omitempty"`
}

// CreateSandbox creates a sandbox and returns the resulting sandbox detail.
func (c *Client) CreateSandbox(ctx context.Context, opts *SandboxOpts) (*SandboxInfo, error) {
	if opts == nil {
		opts = &SandboxOpts{}
	}
	validatedSecrets, err := ValidateSecretEnvs(opts.Secrets, opts.EnvVars)
	if err != nil {
		return nil, err
	}

	if opts.Template == "" && opts.Image == "" {
		return nil, &InvalidArgumentError{
			Argument: "Template/Image",
			Reason:   "either a template or an image must be specified",
		}
	}
	if opts.Template != "" && opts.Image != "" {
		return nil, &InvalidArgumentError{
			Argument: "Template/Image",
			Reason:   "template and image are mutually exclusive",
		}
	}
	if c.connection.IsLegacyDomain {
		switch {
		case opts.Mcp != nil:
			return nil, &UnsupportedError{Operation: "Sandbox.Create MCP", Reason: "requires a modern domain"}
		case opts.Network != nil && (opts.Network.AllowOut != nil || opts.Network.DenyOut != nil || opts.Network.MaskRequestHost != ""):
			return nil, &UnsupportedError{Operation: "Sandbox.Create network", Reason: "requires a modern domain"}
		case len(opts.Secrets) != 0:
			return nil, &UnsupportedError{Operation: "Sandbox.Create secrets", Reason: "requires a modern domain"}
		case len(opts.VolumeMounts) != 0:
			return nil, &UnsupportedError{Operation: "Sandbox.Create volume mounts", Reason: "requires a modern domain"}
		}
	}

	autoPause := opts.AutoPause
	autoResume := opts.AutoResume
	if opts.Lifecycle != nil {
		autoPause = opts.Lifecycle.OnTimeout == OnTimeoutPause
		if autoPause {
			autoResume = &SandboxAutoResumeConfig{Enabled: opts.Lifecycle.AutoResume}
		} else {
			autoResume = nil
		}
	}
	modernInternetAccess := opts.AllowInternetAccess
	var legacyInternetAccess *bool
	if c.connection.IsLegacyDomain {
		modernInternetAccess = nil
		legacyInternetAccess = opts.AllowInternetAccess
	}

	payload := createSandboxRequest{
		TemplateID:           opts.Template,
		Image:                opts.Image,
		Timeout:              opts.Timeout,
		AutoPause:            autoPause,
		AutoResume:           autoResume,
		Secure:               opts.secureValue(!c.connection.IsLegacyDomain),
		AllowInternetAccess:  modernInternetAccess,
		LegacyInternetAccess: legacyInternetAccess,
		Network:              opts.Network,
		Metadata:             opts.Metadata,
		EnvVars:              opts.EnvVars,
		Secrets:              opts.Secrets,
		Mcp:                  opts.Mcp,
		VolumeMounts:         opts.VolumeMounts,
		NodeID:               opts.NodeID,
	}

	payload.Secrets = validatedSecrets
	// A long-running sandbox has no creation lifetime. An explicitly supplied
	// metadata idle_timeout still controls idleness and must remain untouched.
	if opts.Metadata["long_running"] == "true" {
		payload.Timeout = 0
	}
	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	res, err := c.do(ctx, http.MethodPost, "/sandboxes", "", bytesReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	if err := c.checkResponse(res); err != nil {
		return nil, err
	}

	var info SandboxInfo
	if err := json.NewDecoder(res.Body).Decode(&info); err != nil {
		return nil, err
	}
	c.normalizeSandboxInfo(&info)
	return &info, nil
}

// GetSandboxInfo retrieves the details of a sandbox.
func (c *Client) GetSandboxInfo(ctx context.Context, sandboxID string) (*SandboxInfo, error) {
	res, err := c.do(ctx, http.MethodGet, "/sandboxes/"+url.PathEscape(sandboxID), "", nil)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	if err := c.checkResponse(res); err != nil {
		return nil, err
	}

	var info SandboxInfo
	if err := json.NewDecoder(res.Body).Decode(&info); err != nil {
		return nil, err
	}
	c.normalizeSandboxInfo(&info)
	return &info, nil
}

// connectSandboxRequest is the JSON body for POST /sandboxes/{id}/connect.
type connectSandboxRequest struct {
	Timeout            int32                 `json:"timeout"`
	AutoPause          *bool                 `json:"autoPause,omitempty"`
	Secure             *bool                 `json:"secure,omitempty"`
	AllowPublicTraffic *bool                 `json:"allowPublicTraffic,omitempty"`
	Network            *SandboxNetworkConfig `json:"network,omitempty"`
}

// ConnectSandbox resumes or refreshes a sandbox and returns its details.
func (c *Client) ConnectSandbox(ctx context.Context, sandboxID string, opts *SandboxOpts) (*SandboxInfo, error) {
	timeout := int32(DefaultSandboxTimeoutMS / 1000)
	if opts != nil && opts.Timeout != 0 {
		timeout = opts.Timeout
	}

	payload := connectSandboxRequest{
		Timeout:            timeout,
		AllowPublicTraffic: opts.allowPublicTraffic(),
	}
	if opts != nil {
		payload.Secure = opts.secureValue(false)
		if opts.SecureOption == nil && !opts.Secure {
			payload.Secure = nil
		}
		if opts.AllowPublicTraffic == nil {
			payload.Network = opts.Network
		}
	}
	if c.connection.IsLegacyDomain {
		autoPause := false
		payload.AutoPause = &autoPause
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	res, err := c.do(ctx, http.MethodPost, "/sandboxes/"+url.PathEscape(sandboxID)+"/connect", "", bytesReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	if err := c.checkResponse(res); err != nil {
		return nil, err
	}

	var info SandboxInfo
	if err := json.NewDecoder(res.Body).Decode(&info); err != nil {
		return nil, err
	}
	c.normalizeSandboxInfo(&info)
	return &info, nil
}

// KillSandbox terminates a sandbox, matching JS/Python semantics: 404 reports
// false (already gone), any other error is returned, and success reports true.
func (c *Client) KillSandbox(ctx context.Context, sandboxID string) (bool, error) {
	if c.connection.Debug {
		return true, nil
	}
	res, err := c.do(ctx, http.MethodDelete, "/sandboxes/"+url.PathEscape(sandboxID), "", nil)
	if err != nil {
		return false, err
	}
	defer res.Body.Close()

	if res.StatusCode == http.StatusNotFound {
		return false, nil
	}
	if err := c.checkResponse(res); err != nil {
		return false, err
	}
	return true, nil
}

func (c *Client) postJSON(ctx context.Context, path string, payload interface{}, out interface{}) error {
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	res, err := c.do(ctx, http.MethodPost, path, "", bytesReader(b))
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if err := c.checkResponse(res); err != nil {
		return err
	}
	if out != nil {
		return json.NewDecoder(res.Body).Decode(out)
	}
	return nil
}

func (c *Client) ListSandboxes(ctx context.Context, query string) ([]SandboxInfo, error) {
	items, _, err := c.listSandboxesPage(ctx, query)
	return items, err
}
func (c *Client) listSandboxesPage(ctx context.Context, query string) ([]SandboxInfo, string, error) {
	res, err := c.do(ctx, http.MethodGet, "/v2/sandboxes", query, nil)
	if err != nil {
		return nil, "", err
	}
	defer res.Body.Close()
	if err := c.checkResponse(res); err != nil {
		return nil, "", err
	}
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, "", err
	}
	var items []SandboxInfo
	next := res.Header.Get("X-Next-Token")
	if err := json.Unmarshal(raw, &items); err != nil {
		var body struct {
			Sandboxes []SandboxInfo `json:"sandboxes"`
			NextToken string        `json:"nextToken"`
			Cursor    string        `json:"cursor"`
		}
		if wrapperErr := json.Unmarshal(raw, &body); wrapperErr != nil {
			return nil, "", err
		}
		items = body.Sandboxes
		if body.NextToken != "" {
			next = body.NextToken
		} else if body.Cursor != "" {
			next = body.Cursor
		}
	}
	for i := range items {
		c.normalizeSandboxInfo(&items[i])
	}
	return items, next, nil
}

// PauseSandbox pauses a sandbox, matching JS/Python semantics: 409 (already
// paused) reports false, 404 raises SandboxNotFoundError, success reports true.
func (c *Client) PauseSandbox(ctx context.Context, id string) (bool, error) {
	return c.PauseSandboxWithOptions(ctx, id, PauseOptions{})
}

// PauseSandboxWithOptions pauses a sandbox. Modern clusters send no request
// body; legacy clusters send {"sync": false} (JS sandboxApi.ts pause).
func (c *Client) PauseSandboxWithOptions(ctx context.Context, id string, opts PauseOptions) (bool, error) {
	if c.connection.Debug {
		return true, nil
	}
	var body io.Reader
	if c.connection.IsLegacyDomain {
		sync := opts.Sync
		raw, err := json.Marshal(map[string]bool{"sync": sync})
		if err != nil {
			return false, err
		}
		body = bytesReader(raw)
	}
	res, err := c.do(ctx, http.MethodPost, "/sandboxes/"+url.PathEscape(id)+"/pause", "", body)
	if err != nil {
		return false, err
	}
	defer res.Body.Close()

	if res.StatusCode == http.StatusConflict {
		return false, nil
	}
	if err := c.checkResponse(res); err != nil {
		return false, err
	}
	return true, nil
}
func (c *Client) SetSandboxTimeout(ctx context.Context, id string, timeoutMs int64) error {
	if c.connection.Debug {
		return nil
	}
	return c.postJSON(ctx, "/sandboxes/"+url.PathEscape(id)+"/timeout", map[string]int64{"timeout": timeoutMs / 1000}, nil)
}
func (c *Client) ResizeSandbox(ctx context.Context, id string, memoryMiB int64) error {
	if c.connection.IsLegacyDomain {
		return &UnsupportedError{Operation: "Sandbox.Resize", Reason: "requires a modern domain"}
	}
	return c.postJSON(ctx, "/sandboxes/"+url.PathEscape(id)+"/hotplug-memory", map[string]int64{"requested_size_mib": memoryMiB}, nil)
}

// ResetSandbox resets a sandbox to its latest snapshot, matching JS/Python
// semantics: 409 reports false (cannot reset right now), 404 raises
// SandboxNotFoundError, any 2xx (including an empty body) reports true.
// JS/Python always include resume in the reset body (defaulting to true when
// the caller does not specify it).
func (c *Client) ResetSandbox(ctx context.Context, id string, payload map[string]interface{}) (bool, error) {
	result, err := c.ResetSandboxWithResult(ctx, id, payload)
	if err != nil {
		return false, err
	}
	return result.Success, nil
}

// SetSandboxNetwork updates sandbox egress rules, matching JS/Python: PUT
// /sandboxes/{id}/network with a body carrying only allowOut/denyOut
// (SandboxNetworkUpdateOpts); other network fields belong to create only.
func (c *Client) SetSandboxNetwork(ctx context.Context, id string, network *SandboxNetworkConfig) error {
	if c.connection.IsLegacyDomain {
		return &UnsupportedError{Operation: "Sandbox.SetNetwork", Reason: "requires a modern domain"}
	}
	body := map[string]any{}
	if network != nil {
		// JS/Python omit the field only when it is not provided at all; an
		// explicitly set (even empty) list is sent as-is. A nil slice in Go
		// models "not provided", like undefined in JS and None in Python.
		if network.AllowOut != nil {
			body["allowOut"] = network.AllowOut
		}
		if network.DenyOut != nil {
			body["denyOut"] = network.DenyOut
		}
	}
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	res, err := c.do(ctx, http.MethodPut, "/sandboxes/"+url.PathEscape(id)+"/network", "", bytesReader(b))
	if err != nil {
		return err
	}
	defer res.Body.Close()

	return c.checkResponse(res)
}
func (c *Client) GetSandboxMetrics(ctx context.Context, id string, query string) ([]SandboxMetrics, error) {
	if c.connection.Debug {
		return []SandboxMetrics{}, nil
	}
	res, err := c.do(ctx, http.MethodGet, "/sandboxes/"+url.PathEscape(id)+"/metrics", query, nil)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if err := c.checkResponse(res); err != nil {
		return nil, err
	}
	var out []SandboxMetrics
	err = json.NewDecoder(res.Body).Decode(&out)
	if err != nil {
		return nil, err
	}
	// JS legacy metrics always report diskUsed/diskTotal as -1 (the legacy
	// control plane has no disk figures); memUsedMiB/memTotalMiB are already
	// normalized to bytes by SandboxMetrics.UnmarshalJSON.
	if c.connection.IsLegacyDomain {
		for i := range out {
			out[i].DiskUsed = -1
			out[i].DiskTotal = -1
		}
	}
	return out, nil
}

func (c *Client) MountVolume(ctx context.Context, sandboxID string, opts VolumeMountOptions) (*SandboxInfo, error) {
	if c.connection.IsLegacyDomain {
		return nil, &UnsupportedError{Operation: "Sandbox.MountVolume", Reason: "requires a modern domain"}
	}
	if opts.Name == "" || opts.Path == "" {
		return nil, &InvalidArgumentError{Argument: "volume", Reason: "name and path are required"}
	}
	var out SandboxInfo
	err := c.postJSON(ctx, "/sandboxes/"+url.PathEscape(sandboxID)+"/volume-mounts", opts, &out)
	return &out, err
}

func (c *Client) UnmountVolume(ctx context.Context, sandboxID, path string, force bool) (*SandboxInfo, error) {
	if c.connection.IsLegacyDomain {
		return nil, &UnsupportedError{Operation: "Sandbox.UnmountVolume", Reason: "requires a modern domain"}
	}
	if path == "" {
		return nil, &InvalidArgumentError{Argument: "path", Reason: "cannot be empty"}
	}
	query := url.Values{"path": []string{path}}
	if force {
		query.Set("force", "true")
	}
	res, err := c.do(ctx, http.MethodDelete, "/sandboxes/"+url.PathEscape(sandboxID)+"/volume-mounts", query.Encode(), nil)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if err := c.checkResponse(res); err != nil {
		return nil, err
	}
	var out SandboxInfo
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		return nil, err
	}
	return &out, nil
}
func (c *Client) GetSandboxQuota(ctx context.Context) (*SandboxQuota, error) {
	if c.connection.IsLegacyDomain {
		return nil, &UnsupportedError{Operation: "Sandbox.GetQuota", Reason: "requires a modern domain"}
	}
	res, err := c.do(ctx, http.MethodGet, "/sandboxes/quota", "", nil)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if err := c.checkResponse(res); err != nil {
		return nil, err
	}
	var out SandboxQuota
	err = json.NewDecoder(res.Body).Decode(&out)
	return &out, err
}
func (c *Client) GetSandboxEvents(ctx context.Context, query string) (*SandboxEventsResult, error) {
	res, err := c.do(ctx, http.MethodGet, "/sandboxes/events", query, nil)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if err := c.checkResponse(res); err != nil {
		return nil, err
	}
	var out SandboxEventsResult
	err = json.NewDecoder(res.Body).Decode(&out)
	return &out, err
}

func (c *Client) CloneSandbox(ctx context.Context, id string, req CloneSandboxRequest) (*CloneSandboxResponse, error) {
	var out CloneSandboxResponse
	err := c.postJSON(ctx, "/sandboxes/"+url.PathEscape(id)+"/clone", req, &out)
	if err == nil {
		for i := range out.Sandboxes {
			c.normalizeSandboxInfo(&out.Sandboxes[i])
		}
	}
	return &out, err
}
func (c *Client) CreateSnapshot(ctx context.Context, id string) (*SnapshotInfo, error) {
	if c.connection.IsLegacyDomain {
		return nil, &UnsupportedError{Operation: "Sandbox.CreateSnapshot", Reason: "requires a modern domain"}
	}
	var out SnapshotInfo
	err := c.postJSON(ctx, "/sandboxes/"+url.PathEscape(id)+"/snapshots", map[string]interface{}{}, &out)
	return &out, err
}
func (c *Client) ListSnapshots(ctx context.Context, query string) ([]SnapshotInfo, error) {
	if c.connection.IsLegacyDomain {
		return nil, &UnsupportedError{Operation: "Sandbox.ListSnapshots", Reason: "requires a modern domain"}
	}
	res, err := c.do(ctx, http.MethodGet, "/snapshots", query, nil)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if err := c.checkResponse(res); err != nil {
		return nil, err
	}
	var out []SnapshotInfo
	err = json.NewDecoder(res.Body).Decode(&out)
	return out, err
}

// DeleteSnapshot deletes a snapshot, matching JS/Python semantics: 404
// reports false (already gone), any other error is returned, success
// reports true.
func (c *Client) DeleteSnapshot(ctx context.Context, id string) (bool, error) {
	if c.connection.IsLegacyDomain {
		return false, &UnsupportedError{Operation: "Sandbox.DeleteSnapshot", Reason: "requires a modern domain"}
	}
	// Snapshots are persisted as template builds and use the template deletion
	// endpoint in the control-plane API, matching the JS and Python SDKs.
	res, err := c.do(ctx, http.MethodDelete, "/templates/"+url.PathEscape(id), "", nil)
	if err != nil {
		return false, err
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusNotFound {
		return false, nil
	}
	if err := c.checkResponse(res); err != nil {
		return false, err
	}
	return true, nil
}
func (c *Client) CommitSandbox(ctx context.Context, id, alias string) (*TemplateInfo, error) {
	if !c.connection.IsLegacyDomain {
		return nil, &UnsupportedError{Operation: "Sandbox.CommitLegacy", Reason: "requires a legacy domain"}
	}
	var out TemplateInfo
	body := map[string]string{}
	if alias != "" {
		body["alias"] = alias
	}
	err := c.postJSON(ctx, "/sandboxes/"+url.PathEscape(id)+"/commit", body, &out)
	// JS/Python parse the commit response through toTemplateInfo(data,
	// 'snapshot_template'), which defaults the type when omitted.
	if err == nil && out.TemplateType == "" {
		out.TemplateType = "snapshot_template"
	}
	return &out, err
}

// checkResponse converts non-2xx responses into typed errors.
func (c *Client) checkResponse(res *http.Response) error {
	if res.StatusCode >= 200 && res.StatusCode < 300 {
		return nil
	}

	bodyBytes, _ := io.ReadAll(res.Body)
	message := errorMessageFromBody(bodyBytes)
	if message == "" {
		message = res.Status
	}

	switch res.StatusCode {
	case http.StatusBadRequest:
		return &InvalidArgumentError{Argument: "", Reason: message}
	case http.StatusUnauthorized:
		return &AuthenticationError{Message: message}
	case http.StatusForbidden:
		return &AuthenticationError{Message: "forbidden: " + message}
	case http.StatusNotFound:
		return &SandboxNotFoundError{SandboxID: message}
	case http.StatusConflict:
		return &SandboxError{Message: message, StatusCode: res.StatusCode}
	case http.StatusTooManyRequests:
		return &RateLimitError{RetryAfter: res.Header.Get("Retry-After")}
	case http.StatusGatewayTimeout:
		return &TimeoutError{Operation: "request", Duration: "unknown"}
	case http.StatusInsufficientStorage:
		return &NotEnoughSpaceError{}
	default:
		return &SandboxError{Message: message, StatusCode: res.StatusCode}
	}
}

// bytesReader wraps a byte slice in a reader.
func bytesReader(b []byte) io.Reader {
	return bytes.NewReader(b)
}

// Keep client defaults independent of subsequent edits to the options object.
func cloneClientOptions(opts *SandboxOpts) *SandboxOpts {
	if opts == nil {
		return nil
	}
	copy := *opts
	return &copy
}
