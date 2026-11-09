package core

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Sandbox represents a running Novita Agent Sandbox.
//
// A Sandbox bundles two clients: a control-plane client (sandbox lifecycle,
// authenticated with an API key) and data-plane clients for envd inside the
// sandbox (filesystem, commands), authenticated with the sandbox access token.
type Sandbox struct {
	// sandboxID is the sandbox identifier; use the ID method to read it.
	sandboxID string

	// Files provides filesystem access inside the sandbox.
	Files *Filesystem
	// Commands runs processes inside the sandbox.
	Commands *Commands
	// Pty provides pseudo-terminal access inside the sandbox.
	Pty *Pty
	// Git runs Git operations inside the sandbox.
	Git *Git

	client     *Client
	connection *ConnectionConfig

	// envd connection details
	envdVersion        string
	envdAccessToken    string
	trafficAccessToken string
	trafficTokenMu     sync.RWMutex
	resetMu            sync.Mutex
	sandboxDomain      string
	envdPort           int

	// mcpToken caches the MCP gateway token read from inside the sandbox.
	mcpToken string

	baseHeaders map[string]string

	rpc  *rpcClient
	rest *restClient
}

// SandboxInfo is defined in types.go.

// CreateSandbox creates a new sandbox and returns a connected handle to it.
func CreateSandbox(ctx context.Context, opts *SandboxOpts) (*Sandbox, error) {
	if opts == nil {
		opts = &SandboxOpts{}
	} else {
		clone := *opts
		opts = &clone
	}

	if opts.Template == "" && opts.Image == "" {
		opts.Template = "base"
	}
	if opts.Timeout == 0 {
		opts.Timeout = int32(DefaultSandboxTimeoutMS / 1000)
	}

	connection, err := NewConnectionConfig(opts)
	if err != nil {
		return nil, err
	}
	if connection.Debug {
		client := &Client{connection: connection, http: &http.Client{}}
		return newSandboxFromInfo(client, &SandboxInfo{
			SandboxID:   "debug_sandbox_id",
			TemplateID:  opts.Template,
			EnvdVersion: EnvdDebugFallback,
		}, opts)
	}

	client, err := NewClient(opts)
	if err != nil {
		return nil, err
	}

	info, err := client.CreateSandbox(ctx, opts)
	if err != nil {
		return nil, err
	}
	if compareVersions(info.EnvdVersion, "0.1.0") < 0 {
		_, _ = client.KillSandbox(ctx, info.SandboxID)
		return nil, &TemplateError{
			TemplateID: info.TemplateID,
			Reason:     "template envd version is older than 0.1.0; rebuild the template with the current SDK",
		}
	}

	return newSandboxFromInfo(client, info, opts)
}

// Connect attaches to an existing sandbox, refreshing its expiry.
//
// The sandbox must already be running; its envd endpoint is discovered from
// the control plane.
func Connect(ctx context.Context, sandboxID string, opts *SandboxOpts) (*Sandbox, error) {
	if opts == nil {
		opts = &SandboxOpts{}
	}

	client, err := NewClient(opts)
	if err != nil {
		return nil, err
	}

	info, err := client.ConnectSandbox(ctx, sandboxID, opts)
	if err != nil {
		return nil, err
	}

	return newSandboxFromInfo(client, info, opts)
}

// newSandboxFromInfo builds the sandbox handle and its sub-clients.
func newSandboxFromInfo(client *Client, info *SandboxInfo, opts *SandboxOpts) (*Sandbox, error) {
	connection := client.Connection()
	sandboxID := sandboxIDForDomain(info.SandboxID, info.ClientID, connection.IsLegacyDomain)

	sandboxDomain := opts.SandboxDomain
	if sandboxDomain == "" && info.Domain != nil {
		sandboxDomain = *info.Domain
	}
	if sandboxDomain == "" {
		sandboxDomain = connection.Domain
	}

	sandbox := &Sandbox{
		sandboxID:          sandboxID,
		client:             client,
		connection:         connection,
		envdVersion:        info.EnvdVersion,
		envdAccessToken:    info.EnvdAccessToken,
		trafficAccessToken: info.TrafficAccessToken,
		sandboxDomain:      sandboxDomain,
		envdPort:           EnvdPort,
	}

	if sandbox.envdVersion == "" {
		// Without a version we cannot gate features; assume the newest behaviour.
		sandbox.envdVersion = EnvdDebugFallback
	}

	// Headers sent on every envd request.
	baseHeaders := map[string]string{
		"Novita-Sandbox-Id":   sandbox.sandboxID,
		"Novita-Sandbox-Port": fmt.Sprintf("%d", sandbox.envdPort),
	}
	for k, v := range connection.Headers {
		baseHeaders[k] = v
	}
	if sandbox.envdAccessToken != "" {
		baseHeaders["X-Access-Token"] = sandbox.envdAccessToken
	}
	sandbox.baseHeaders = baseHeaders

	envdURL := connection.GetSandboxURL(sandbox.sandboxID, sandboxDomain, sandbox.envdPort)
	sandbox.rpc = newRPCClient(envdURL, baseHeaders)
	sandbox.rest = newRESTClient(envdURL, baseHeaders)

	// Sub-clients.
	sandbox.Files = newFilesystem(sandbox)
	sandbox.Commands = newCommands(sandbox)
	sandbox.Pty = newPty(sandbox)
	sandbox.Git = newGit(sandbox)

	return sandbox, nil
}

// NewSandboxFromConfig builds a sandbox handle from an explicit connection
// configuration and sandbox details, without contacting the control plane.
//
// This suits callers that already know the envd endpoint and credentials, for
// example when reconnecting to a sandbox whose details were cached elsewhere.
func NewSandboxFromConfig(config *ConnectionConfig, info *SandboxInfo) (*Sandbox, error) {
	if config == nil {
		return nil, &InvalidArgumentError{Argument: "config", Reason: "connection config is required"}
	}
	if info == nil || info.SandboxID == "" {
		return nil, &InvalidArgumentError{Argument: "info", Reason: "sandbox ID is required"}
	}

	client := &Client{connection: config, http: &http.Client{}}
	return newSandboxFromInfo(client, info, &SandboxOpts{})
}

// ID returns the sandbox identifier.
func (s *Sandbox) ID() string {
	return s.sandboxID
}

// EnvdVersion returns the envd version reported by the sandbox.
func (s *Sandbox) EnvdVersion() string {
	return s.envdVersion
}

// SandboxDomain returns the domain used to reach this sandbox.
func (s *Sandbox) SandboxDomain() string {
	return s.sandboxDomain
}

// EnvdURL returns the base URL of the sandbox's envd endpoint.
func (s *Sandbox) EnvdURL() string {
	return s.connection.GetSandboxURL(s.sandboxID, s.sandboxDomain, s.envdPort)
}

// GetHost returns `${port}-${sandboxID}.${domain}` for reaching a service
// running inside the sandbox.
func (s *Sandbox) GetHost(port int) string {
	return s.connection.GetHost(s.sandboxID, port, s.sandboxDomain)
}

// Kill terminates the sandbox, reporting false when it was already gone
// (404), matching JS/Python kill().
func (s *Sandbox) Kill(ctx context.Context) (bool, error) {
	return s.client.KillSandbox(ctx, s.sandboxID)
}

// Info retrieves the current sandbox details.
func (s *Sandbox) Info(ctx context.Context) (*SandboxInfo, error) {
	return s.client.GetSandboxInfo(ctx, s.sandboxID)
}

// Connection exposes the connection configuration backing this sandbox.
func (s *Sandbox) Connection() *ConnectionConfig {
	return s.connection
}

// AccessToken returns the envd access token issued for this sandbox, which is
// also what other in-sandbox services such as the Jupyter server accept.
func (s *Sandbox) AccessToken() string {
	return s.envdAccessToken
}

// TrafficAccessToken returns the token required to reach sandbox URLs when
// public traffic is disabled.
func (s *Sandbox) TrafficAccessToken() string {
	s.trafficTokenMu.RLock()
	defer s.trafficTokenMu.RUnlock()
	return s.trafficAccessToken
}

// sandboxIDForDomain accepts either a legacy short ID or an already complete ID.
// Modern IDs are opaque and must not be rewritten.
func sandboxIDForDomain(id, clientID string, legacy bool) string {
	if legacy && id != "" && clientID != "" && !strings.HasSuffix(id, "-"+clientID) {
		return id + "-" + clientID
	}
	return id
}

func (c *Client) normalizeSandboxInfo(info *SandboxInfo) {
	info.SandboxID = sandboxIDForDomain(info.SandboxID, info.ClientID, c.connection.IsLegacyDomain)
}

// PortURL returns the HTTPS URL for a port exposed by this sandbox, used by
// modules that talk to in-sandbox services other than envd.
//
// An explicitly configured SandboxURL takes precedence, which lets callers
// (and tests) redirect every in-sandbox service to a single endpoint.
func (s *Sandbox) PortURL(port int) string {
	if s.connection.SandboxURL != "" {
		return strings.TrimSuffix(s.connection.SandboxURL, "/")
	}

	scheme := "https"
	if s.connection.Debug {
		scheme = "http"
	}
	return fmt.Sprintf("%s://%s", scheme, s.connection.GetHost(s.sandboxID, port, s.sandboxDomain))
}

// Headers returns a copy of the headers sent on every sandbox request.
func (s *Sandbox) Headers() map[string]string {
	headers := make(map[string]string, len(s.baseHeaders))
	for k, v := range s.baseHeaders {
		headers[k] = v
	}
	return headers
}

// IsRunning reports whether the sandbox is still running, matching JS
// isRunning(): the envd /health endpoint answers 502 while the sandbox is
// down, which reports false; any other failure is an error.
func (s *Sandbox) IsRunning(ctx context.Context) (bool, error) {
	res, err := s.rest.do(ctx, http.MethodGet, "/health", nil, nil, "")
	if err != nil {
		return false, err
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusBadGateway {
		return false, nil
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return false, &SandboxError{Message: "health check failed: " + res.Status, StatusCode: res.StatusCode}
	}
	return true, nil
}

// GetMcpURL returns the MCP server URL for the sandbox, matching JS
// getMcpUrl(). Not supported on legacy domains.
func (s *Sandbox) GetMcpURL() (string, error) {
	if s.connection.IsLegacyDomain {
		return "", &UnsupportedError{Operation: "Sandbox.GetMcpURL", Reason: "requires a modern domain"}
	}
	return fmt.Sprintf("https://%s/mcp", s.GetHost(McpPort)), nil
}

// GetMcpToken returns the MCP gateway token, matching JS getMcpToken(): the
// token is read from /etc/mcp-gateway/.token inside the sandbox as root and
// cached for later calls. Not supported on legacy domains.
func (s *Sandbox) GetMcpToken(ctx context.Context) (string, error) {
	if s.connection.IsLegacyDomain {
		return "", &UnsupportedError{Operation: "Sandbox.GetMcpToken", Reason: "requires a modern domain"}
	}
	if s.mcpToken == "" {
		data, err := s.Files.Read(ctx, "/etc/mcp-gateway/.token", &FilesystemReadOpts{
			FilesystemRequestOpts: FilesystemRequestOpts{User: Username{Name: "root"}},
		})
		if err != nil {
			return "", err
		}
		s.mcpToken = string(data)
	}
	return s.mcpToken, nil
}

// SandboxURLOpts are options for UploadURL and DownloadURL.
type SandboxURLOpts struct {
	// User that will access the file, like JS Username (a plain string).
	// nil means "not provided" (JS undefined); an explicit pointer to ""
	// keeps the empty user like JS/Java do.
	User *string
	// UseSignatureExpiration requests a signature valid for this many
	// seconds; only allowed for secured sandboxes (envd access token set).
	// Zero is treated as unset, matching the JS truthiness check.
	UseSignatureExpiration *int
}

// UploadURL returns the URL for uploading a file to the sandbox, matching JS
// uploadUrl(). Secured sandboxes (envd access token present) get a signed URL.
func (s *Sandbox) UploadURL(path string, opts *SandboxURLOpts) (string, error) {
	return s.sandboxFileURL(path, opts, "write")
}

// DownloadURL returns the URL for downloading a file from the sandbox,
// matching JS downloadUrl(). Secured sandboxes get a signed URL.
func (s *Sandbox) DownloadURL(path string, opts *SandboxURLOpts) (string, error) {
	return s.sandboxFileURL(path, opts, "read")
}

// sandboxFileURL mirrors the JS fileUrl() + getSignature() flow: the plain
// /files URL gains username/path query params (in that order), and secured
// sandboxes append a v1_ signature plus its expiration when requested.
func (s *Sandbox) sandboxFileURL(path string, opts *SandboxURLOpts, operation string) (string, error) {
	o := opts
	if o == nil {
		o = &SandboxURLOpts{}
	}
	useSignature := s.envdAccessToken != ""
	if !useSignature && o.UseSignatureExpiration != nil {
		return "", &InvalidArgumentError{
			Argument: "useSignatureExpiration",
			Reason:   "signature expiration can be used only when sandbox is created as secured",
		}
	}
	// JS applies the envd<0.4.0 default user only when the caller did not
	// provide one; an explicit empty user stays empty (JS undefined check).
	var username string
	if o.User != nil {
		username = *o.User
	} else if compareVersions(s.envdVersion, EnvdDefaultUser) < 0 {
		username = DefaultUsername
	}

	var parts []string
	if username != "" {
		parts = append(parts, "username="+url.QueryEscape(username))
	}
	if path != "" {
		parts = append(parts, "path="+url.QueryEscape(path))
	}

	if !useSignature {
		return s.rest.baseURL + "/files" + joinQuery(parts), nil
	}
	signature, expiration, err := fileAccessSignature(path, operation, username, o.UseSignatureExpiration, s.envdAccessToken)
	if err != nil {
		return "", err
	}
	parts = append(parts, "signature="+url.QueryEscape(signature))
	if expiration != nil {
		parts = append(parts, "signature_expiration="+strconv.FormatInt(*expiration, 10))
	}
	return s.rest.baseURL + "/files" + joinQuery(parts), nil
}

func joinQuery(parts []string) string {
	if len(parts) == 0 {
		return ""
	}
	return "?" + strings.Join(parts, "&")
}

// fileAccessSignature computes the v1_ file URL signature, matching the JS
// getSignature(): "v1_" + base64(SHA-256("{path}:{operation}:{user}:{token}"
// [+":{expiration unix seconds}"])) with trailing '=' stripped. A zero
// expiration is treated as no expiration, matching the JS truthiness check.
func fileAccessSignature(path, operation, user string, expirationInSeconds *int, envdAccessToken string) (string, *int64, error) {
	if envdAccessToken == "" {
		return "", nil, &SandboxError{Message: "Access token is not set and signature cannot be generated!"}
	}
	raw := path + ":" + operation + ":" + user + ":" + envdAccessToken
	var expiration *int64
	if expirationInSeconds != nil && *expirationInSeconds != 0 {
		value := time.Now().Unix() + int64(*expirationInSeconds)
		expiration = &value
		raw += ":" + strconv.FormatInt(value, 10)
	}
	sum := sha256.Sum256([]byte(raw))
	encoded := strings.TrimRight(base64.StdEncoding.EncodeToString(sum[:]), "=")
	return "v1_" + encoded, expiration, nil
}
