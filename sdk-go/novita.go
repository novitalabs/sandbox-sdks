// Package novita provides the high-level entry point for the Novita Sandbox
// Go SDK. Lower-level APIs remain available from the core, code_interpreter,
// and desktop subpackages.
package novita

import (
	"context"

	"github.com/novitalabs/sandbox-sdks/sdk-go/v2/code_interpreter"
	"github.com/novitalabs/sandbox-sdks/sdk-go/v2/core"
	"github.com/novitalabs/sandbox-sdks/sdk-go/v2/desktop"
)

// Novita is the high-level SDK facade. Its namespaces share the connection
// configuration supplied to New.
type Novita struct {
	Client          *core.Client
	Sandbox         *SandboxNamespace
	Template        *core.TemplateNamespace
	Volume          *core.VolumeNamespace
	Secret          *core.SecretNamespace
	Job             *core.JobClient
	CodeInterpreter *CodeInterpreterNamespace
	Desktop         *DesktopNamespace
}

// SandboxNamespace exposes the core sandbox control-plane methods while
// ensuring runtime creation and connection inherit the facade configuration.
type SandboxNamespace struct {
	*core.SandboxNamespace
	opts core.SandboxOpts
}

// CodeInterpreterNamespace creates and connects code-interpreter sandboxes.
type CodeInterpreterNamespace struct {
	opts core.SandboxOpts
}

// DesktopNamespace creates and connects desktop sandboxes.
type DesktopNamespace struct {
	opts core.SandboxOpts
}

// New creates a high-level SDK client.
func New(opts *core.SandboxOpts) (*Novita, error) {
	base := cloneSandboxOpts(opts)
	coreClient, err := core.NewNovita(&base)
	if err != nil {
		return nil, err
	}

	return &Novita{
		Client:          coreClient.Client,
		Sandbox:         &SandboxNamespace{SandboxNamespace: coreClient.Sandbox, opts: base},
		Template:        coreClient.Template,
		Volume:          coreClient.Volume,
		Secret:          coreClient.Secret,
		Job:             coreClient.Job,
		CodeInterpreter: &CodeInterpreterNamespace{opts: base},
		Desktop:         &DesktopNamespace{opts: base},
	}, nil
}

// Create creates a regular sandbox.
func (n *SandboxNamespace) Create(ctx context.Context, opts *core.SandboxOpts) (*core.Sandbox, error) {
	return core.CreateSandbox(ctx, mergeSandboxOpts(&n.opts, opts))
}

// Connect connects to a regular sandbox.
func (n *SandboxNamespace) Connect(ctx context.Context, sandboxID string, opts *core.SandboxOpts) (*core.Sandbox, error) {
	return core.Connect(ctx, sandboxID, mergeSandboxOpts(&n.opts, opts))
}

// NewNovita is an explicit alias for New.
func NewNovita(opts *core.SandboxOpts) (*Novita, error) {
	return New(opts)
}

// Create creates a regular sandbox through the core namespace.
func (n *Novita) Create(ctx context.Context, opts *core.SandboxOpts) (*core.Sandbox, error) {
	return n.Sandbox.Create(ctx, opts)
}

// Connect connects to a regular sandbox through the core namespace.
func (n *Novita) Connect(ctx context.Context, sandboxID string, opts *core.SandboxOpts) (*core.Sandbox, error) {
	return n.Sandbox.Connect(ctx, sandboxID, opts)
}

// Create creates a sandbox using the code-interpreter runtime.
func (n *CodeInterpreterNamespace) Create(ctx context.Context, opts *core.SandboxOpts) (*code_interpreter.Sandbox, error) {
	return code_interpreter.CreateSandbox(ctx, mergeSandboxOpts(&n.opts, opts))
}

// Connect connects to an existing code-interpreter sandbox.
func (n *CodeInterpreterNamespace) Connect(ctx context.Context, sandboxID string, opts *core.SandboxOpts) (*code_interpreter.Sandbox, error) {
	return code_interpreter.Connect(ctx, sandboxID, mergeSandboxOpts(&n.opts, opts))
}

// Create creates a sandbox using the desktop runtime.
func (n *DesktopNamespace) Create(ctx context.Context, opts *core.SandboxOpts) (*desktop.Sandbox, error) {
	return desktop.CreateDesktopSandbox(ctx, mergeSandboxOpts(&n.opts, opts))
}

// CreateWithOptions creates and initializes a desktop with display settings.
func (n *DesktopNamespace) CreateWithOptions(ctx context.Context, opts desktop.CreateOptions) (*desktop.Sandbox, error) {
	opts.Sandbox = mergeSandboxOpts(&n.opts, opts.Sandbox)
	return desktop.Create(ctx, opts)
}

// Connect connects to an existing desktop sandbox.
func (n *DesktopNamespace) Connect(ctx context.Context, sandboxID string, opts *core.SandboxOpts) (*desktop.Sandbox, error) {
	return desktop.ConnectDesktopSandbox(ctx, sandboxID, mergeSandboxOpts(&n.opts, opts))
}

// mergeSandboxOpts inherits connection settings from the client and keeps all
// sandbox-specific fields from the per-call options.
func mergeSandboxOpts(base, call *core.SandboxOpts) *core.SandboxOpts {
	merged := cloneSandboxOpts(call)
	if base == nil {
		return &merged
	}

	if merged.APIKey == "" {
		merged.APIKey = base.APIKey
	}
	if merged.AccessToken == "" {
		merged.AccessToken = base.AccessToken
	}
	if merged.Domain == "" {
		merged.Domain = base.Domain
	}
	if merged.APIURL == "" {
		merged.APIURL = base.APIURL
	}
	if merged.SandboxURL == "" {
		merged.SandboxURL = base.SandboxURL
	}
	if merged.SandboxDomain == "" {
		merged.SandboxDomain = base.SandboxDomain
	}
	if merged.Debug == nil {
		merged.Debug = base.Debug
	}
	if merged.RequestTimeoutMs == nil {
		merged.RequestTimeoutMs = base.RequestTimeoutMs
	}
	if merged.Logger == nil {
		merged.Logger = base.Logger
	}
	merged.Headers = mergeHeaders(base.Headers, merged.Headers)
	return &merged
}

func cloneSandboxOpts(opts *core.SandboxOpts) core.SandboxOpts {
	if opts == nil {
		return core.SandboxOpts{}
	}
	clone := *opts
	clone.Headers = mergeHeaders(nil, opts.Headers)
	return clone
}

func mergeHeaders(base, override map[string]string) map[string]string {
	if len(base) == 0 && len(override) == 0 {
		return nil
	}
	merged := make(map[string]string, len(base)+len(override))
	for key, value := range base {
		merged[key] = value
	}
	for key, value := range override {
		merged[key] = value
	}
	return merged
}
