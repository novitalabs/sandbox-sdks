package core

// SandboxOpts contains options for creating or connecting to a sandbox.
//
// The first block mirrors the other SDKs' ConnectionOpts (control-plane
// concerns); the second block mirrors the NewSandbox schema in the OpenAPI spec.
type SandboxOpts struct {
	// --- Connection options (control plane) ---

	// APIKey is the Novita API key. Defaults to NOVITA_API_KEY.
	APIKey string
	// AccessToken is the Novita access token. Defaults to NOVITA_ACCESS_TOKEN.
	AccessToken string
	// Domain is the sandbox domain. Defaults to NOVITA_DOMAIN or us-phx-1.sandbox.novita.ai.
	Domain string
	// APIURL overrides the control-plane URL. Defaults to NOVITA_API_URL or https://api.{domain}.
	APIURL string
	// SandboxURL overrides the envd URL used for data-plane calls.
	SandboxURL string
	// Debug points the SDK at a local envd instead of a remote sandbox.
	Debug *bool
	// RequestTimeoutMs is the per-request timeout in milliseconds.
	//
	// Defaults to 60s, or 30 minutes on legacy domains.
	RequestTimeoutMs *int
	// Logger receives SDK log messages.
	Logger Logger
	// Headers are extra headers attached to every request.
	Headers map[string]string
	// SandboxDomain overrides the domain used to build the envd URL only,
	// leaving the control-plane domain untouched.
	SandboxDomain string

	// --- Sandbox creation options (matches NewSandbox in the OpenAPI spec) ---

	// Template is the template ID to launch (alternative to Image).
	Template string
	// Image is an OCI image reference (alternative to Template).
	Image string

	// Timeout is the sandbox lifetime in seconds (default 300).
	Timeout int32
	// AutoPause pauses the sandbox when the timeout is reached.
	AutoPause bool
	// AutoResume configures automatic resume for paused sandboxes.
	AutoResume *SandboxAutoResumeConfig

	// Metadata is arbitrary key/value data attached to the sandbox.
	Metadata map[string]string
	// EnvVars are environment variables created inside the sandbox.
	EnvVars map[string]string
	// Secrets maps an env var name to the name of a stored secret.
	//
	// The sandbox receives a placeholder rather than the real value.
	Secrets map[string]string

	// Secure secures all system communication with the sandbox.
	// Use SecureOption to explicitly select false; the zero bool uses the operation default.
	Secure bool
	// SecureOption overrides Secure, including an explicit false. Nil uses the operation default.
	SecureOption *bool
	// AllowInternetAccess controls outbound internet access. Nil leaves it unset.
	AllowInternetAccess *bool
	// Network holds egress/ingress network rules.
	Network *SandboxNetworkConfig

	// Lifecycle is the timeout/resume policy.
	Lifecycle *SandboxLifecycleConfig

	// VolumeMounts are volumes to mount into the sandbox.
	VolumeMounts []SandboxVolumeMount

	// Mcp is the MCP server configuration.
	Mcp map[string]interface{}

	// NodeID pins the sandbox to a specific orchestrator node.
	NodeID string

	// AllowPublicTraffic controls whether sandbox URLs are reachable without a
	// traffic access token. Only used when connecting to a sandbox.
	AllowPublicTraffic *bool
}

// SandboxAutoResumeConfig configures auto-resume for paused sandboxes.
type SandboxAutoResumeConfig struct {
	// Enabled turns auto-resume on or off.
	Enabled bool `json:"enabled"`
}

// SandboxLifecycleConfig is the lifecycle policy used at creation time.
//
// It mirrors SandboxLifecycle but is a separate type because the create
// request accepts a plain object rather than the API's returned shape.
type SandboxLifecycleConfig struct {
	// AutoResume allows the sandbox to resume automatically.
	AutoResume bool `json:"autoResume"`
	// OnTimeout is the action taken when the sandbox times out.
	OnTimeout SandboxOnTimeout `json:"onTimeout"`
}

// allowPublicTraffic returns the option, or nil when it was not set.
func (o *SandboxOpts) allowPublicTraffic() *bool {
	if o == nil {
		return nil
	}
	return o.AllowPublicTraffic
}

// Bool is a convenient pointer for optional boolean fields.
func Bool(value bool) *bool { return &value }
func (o *SandboxOpts) secureValue(fallback bool) *bool {
	if o != nil {
		if o.SecureOption != nil {
			return o.SecureOption
		}
		if o.Secure {
			return Bool(true)
		}
	}
	return Bool(fallback)
}
