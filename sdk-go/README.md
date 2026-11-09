# Novita Sandbox Go SDK

Go SDK for [Novita Agent Sandbox](https://novita.ai/sandbox) — isolated cloud sandboxes for
running untrusted or AI-generated code, executing notebooks, and driving desktop environments.

## Cluster compatibility

The SDK automatically uses legacy behavior for `sandbox.novita.ai` and
`us-ga-1.sandbox.novita.ai`.
Every other domain uses modern behavior by default. No additional cluster
domains are built into the SDK.

Set `NOVITA_IS_LEGACY=true` to force legacy behavior, or
`NOVITA_IS_LEGACY=false` to force modern behavior. The override applies to SDK
connections in the process; set it before starting the application. Values are
case-insensitive and surrounding whitespace is ignored. Unset, empty, or other
values use automatic domain detection.

This flag changes compatibility behavior and default request timeouts; it does
not change the connection address or an explicitly configured request timeout.
Set `NOVITA_DOMAIN` (or the domain option) separately, for example:

```sh
export NOVITA_DOMAIN=custom.example.invalid
export NOVITA_IS_LEGACY=true
```

## Installation

Use a Go toolchain that satisfies the requirement in [go.mod](go.mod).

The SDK lives in the `sdk-go` directory of the
[sandbox-sdks](https://github.com/novitalabs/sandbox-sdks) repository. Install it with:

```bash
go get github.com/novitalabs/sandbox-sdks/sdk-go/v2
```

## Quick start

Get an API key from [key management](https://novita.ai/settings/key-management) and export it as
`NOVITA_API_KEY`. An access token through `AccessToken` or `NOVITA_ACCESS_TOKEN` is also supported.

Use the root `novita` facade as the default entrypoint. Configure credentials and
service addresses on the client; set the template and lifetime on each sandbox
creation call. Public options and callback types are imported from `core`,
`code_interpreter` or `desktop` as needed.

```go
package main

import (
	"context"
	"fmt"
	"log"

	novita "github.com/novitalabs/sandbox-sdks/sdk-go/v2"
	"github.com/novitalabs/sandbox-sdks/sdk-go/v2/core"
)

func main() {
	ctx := context.Background()

	client, err := novita.New(&core.SandboxOpts{})
	if err != nil {
		log.Fatal(err)
	}

	sandbox, err := client.Sandbox.Create(ctx, &core.SandboxOpts{
		Template: "base",
		Timeout:  300, // seconds
	})
	if err != nil {
		log.Fatal(err)
	}
	defer sandbox.Kill(ctx)

	// Filesystem
	if _, err := sandbox.Files.WriteText(ctx, "/tmp/hello.txt", "Hello, World!", nil); err != nil {
		log.Fatal(err)
	}

	data, err := sandbox.Files.ReadText(ctx, "/tmp/hello.txt", nil)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(data)

	// Commands
	result, err := sandbox.Commands.Run(ctx, "ls -la /tmp", nil)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(result.Stdout)
}
```

Code Interpreter uses the same client configuration and returns a sandbox with
`RunCode`. For example, with the imports shown above:

```go
func runCode(ctx context.Context, client *novita.Novita) error {
	sandbox, err := client.CodeInterpreter.Create(ctx, &core.SandboxOpts{
		Template: "code-interpreter-v1",
		Timeout:  300,
	})
	if err != nil {
		return err
	}
	defer sandbox.Kill(context.Background())

	execution, err := sandbox.RunCode(ctx, "print('Hello, World!')\n6 * 7", nil)
	if err != nil {
		return err
	}
	if execution.Error != nil {
		return execution.Error
	}
	fmt.Println(execution.Logs.Stdout)
	for _, result := range execution.Results {
		fmt.Println(result.Text)
	}
	return nil
}
```

Use `client.CodeInterpreter.Connect` to reconnect with `RunCode` support.
For streaming output, pass `code_interpreter.RunCodeOpts` with `OnStdout` and
`OnStderr` callbacks to `RunCode`. The `code_interpreter` import supplies these
types; sandbox creation and connection still go through the facade.

Desktop creation can configure its display without exposing untyped maps
(import `github.com/novitalabs/sandbox-sdks/sdk-go/v2/desktop`):

```go
desktopSandbox, err := client.Desktop.CreateWithOptions(ctx, desktop.CreateOptions{
	Resolution: desktop.ScreenSize{Width: 1440, Height: 900},
	DPI:        96,
	Display:    ":0",
})

err = desktopSandbox.VNC.Start(ctx, desktop.VNCStartOptions{RequireAuth: true})
authKey, err := desktopSandbox.VNC.AuthKey()
streamURL, err := desktopSandbox.VNC.URL(desktop.VNCURLOptions{
	AutoConnect: true,
	Resize:      "scale",
	AuthKey:     authKey,
})
```

Templates use a typed, chainable builder. `BuildTemplate` waits for completion;
`BuildTemplateInBackground` returns after file upload and build trigger:

```go
builder := core.NewTemplateBuilder().
	FromPythonImage("slim").
	Workdir("/app").
	Copy("main.py", "/app/main.py", core.CopyOptions{Mode: 0o755}).
	PipInstall([]string{"requests"}, true).
	StartCommand("python /app/main.py", core.WaitForPort(8080))

build, err := client.Template.BuildTemplate(ctx, "python-app", builder, core.BuildOptions{
	ContextPath: ".",
	OnLog: func(entry core.BuildLog) {
		log.Printf("%s", entry.Message)
	},
})
```

## Connecting to an existing sandbox

```go
sandbox, err := client.Sandbox.Connect(ctx, "sandbox-id", &core.SandboxOpts{Timeout: 300})
codeSandbox, err := client.CodeInterpreter.Connect(ctx, "code-sandbox-id", &core.SandboxOpts{Timeout: 300})
desktopSandbox, err := client.Desktop.Connect(ctx, "desktop-sandbox-id", &core.SandboxOpts{Timeout: 300})
```


## Reset and private traffic tokens

Resetting an existing handle refreshes its traffic token when the server returns
one. Use that token for subsequent requests to private sandbox URLs:

```go
ok, err := sandbox.Reset(ctx, core.ResetOptions{
	AllowPublicTraffic: core.Bool(false),
})
if err != nil {
	return err
}
if ok {
	token := sandbox.TrafficAccessToken()
	_ = token // Supply this token when accessing private sandbox URLs.
}
```

For an operation by ID, `client.Sandbox.ResetWithResult(ctx, id, opts)` returns
`ResetResult` with `Success` and an optional `TrafficAccessToken`. It does not
update previously created handles. The existing namespace `Reset` and
`ResetWithOptions` methods still return `(bool, error)`. A missing response token
leaves the handle's current token unchanged; an explicitly empty token clears it.
Envd access credentials are separate and are not changed by a traffic token.
Concurrent resets on the same handle are serialized to keep the latest token.

## Legacy lifecycle and template operations

Legacy domains support `CloneWithOptions`, `Template.List` and `Template.Delete`.
Sandbox pagination uses `/v2/sandboxes` on both domain types and follows the
returned continuation token. Legacy create, connect, info, list and clone results
use complete sandbox IDs, including the client suffix exactly once.

`long_running` and `idle_timeout` metadata are passed through to the service,
matching Python. The SDK does not override an explicit lifetime based on these
metadata fields or enforce a 100-year lifetime; the cluster controls their
precedence and idle policy. Ordinary create/connect defaults remain 300 seconds.

Template file paths are resolved against `BuildOptions.ContextPath`, defaulting
to the working directory at build time. Set an absolute context path if the
working directory can change between constructing and building a template.

## Streaming files and process lifecycle

Use `ReadStream` for downloads and `WriteStream` for uploads that should not be
buffered in memory. Close a returned download reader; an upload reader remains
caller-owned, so close it separately if needed. Cancellation and the resolved
request timeout cover the entire transfer. A source reader must return from
`Read` when its own input is closed or cancelled.

```go
reader, err := sandbox.Files.ReadStream(ctx, "/tmp/archive.tar", nil)
if err != nil {
    return err
}
defer reader.Close()
_, err = io.Copy(destination, reader)

// source is an io.Reader, such as an *os.File.
info, err := sandbox.Files.WriteStream(ctx, "/tmp/archive.tar", source,
    &core.FilesystemWriteOpts{UseOctetStream: true})
```

Octet-stream uploads are used when the runtime supports them; older runtimes use
a streamed multipart upload. `Gzip` compresses octet uploads. `ReadStream`
decodes gzip responses. See [runtime compatibility](docs/runtime-compatibility.md)
for capability thresholds.

`Commands.Start` returns when the process starts and preserves its PID and output.
`Commands.Run` waits for exit. The compatibility `Background` field no longer
rewrites the command or redirects output. Keep the handle to inspect `Stdout()`
and `Stderr()`, call `Wait` for the final result, or detach with `Disconnect()`:

```go
handle, err := sandbox.Commands.Start(ctx, "python server.py", nil)
if err != nil {
    return err
}
pid := handle.PID()
handle.Disconnect() // closes the local stream; the remote process keeps running
handle, err = sandbox.Commands.Connect(ctx, pid, nil)
```

`PtyHandle.Disconnect()` has the same lifecycle. Waiting on a disconnected handle
returns `context.Canceled` unless the final process result was already received.
Transport failure, deadline expiry and a stream ending before a terminal result
are errors. Code-interpreter cancellation also stops dispatching buffered output
callbacks. Both successful output streams and HTTP error bodies remain bounded
by the applicable execution/request deadlines. `WatchDir` waits for server registration before returning; call `Stop`
to cancel and `Wait` to observe completion. `Stop` is safe inside an event callback.

## Explicit boolean options

Creation and connection default to a 300-second lifetime. Modern creation defaults
to secure communication, while legacy creation preserves its legacy default.
Connection leaves secure mode unchanged when no secure option is supplied.

Creation with `Metadata: map[string]string{"long_running": "true"}` omits the
sandbox lifetime timeout on both modern and legacy domains. An explicit
`idle_timeout` in the metadata is preserved; the SDK adds no idle timeout for
long-running sandboxes. Request timeouts still apply.

The original bool fields remain source compatible, but a Go zero bool cannot
express the difference between omitted and explicitly false. Migrate an explicit
false to the pointer override:

```go
opts := &core.SandboxOpts{
    SecureOption: core.Bool(false),
    Network: &core.SandboxNetworkConfig{
        AllowPublicTrafficOption: core.Bool(false),
    },
}
```

`Secure: true` still enables secure mode. `Secure: false` uses the operation's
default unless `SecureOption` is supplied. A network config defaults public
traffic to true, including when only egress rules are set; use
`AllowPublicTrafficOption` to disable it. Legacy domains accept public-traffic
configuration but do not support egress lists or request-host masking.

## Image defaults and build logs

`Build`, `BuildInBackground`, `BuildTemplate` and `BuildTemplateInBackground`
restore an image's declared environment, working directory, entrypoint and CMD by
default. Caller-provided values win. Machine-specific variables such as `HOME`
and `HOSTNAME` are excluded. The inherited start command uses shell-safe argument
quoting and a default `sleep 20` readiness check; supply your own readiness check
for a service health check.

The default resolver reads Registry V2 manifests and config blobs for linux/amd64.
It supports Docker Hub names, custom registries, multi-architecture manifests,
Basic/Bearer authentication, and bounded retries for transient responses. It only
uses credentials supplied for the image. Selecting a different base image clears
credentials associated with the previous image. Basic credentials are restricted to the
registry's origin or Docker Hub's known token service, and HTTPS redirects cannot
downgrade to HTTP. Cloud-specific credential exchanges can
be provided through `BuildOptions.ImageConfigResolver`; GCP service-account JSON
and ordinary `RegistryCredentials` work directly. Reads have a 15-second total
limit, shortened by the build context. Each build reads fresh image defaults, so
reusing a builder with a mutable tag sees registry updates.

`BuildOptions.ImageConfigOptions` accepts `OnConfigFailure`:

- `core.ImageConfigWarn` (default): log a warning and continue without inherited
  defaults. The warning also reaches `OnLog` when supplied.
- `core.ImageConfigRaise`: stop the build with an `ImageConfigError`.
- `core.ImageConfigIgnore`: continue quietly.

`BuildInfo.ImageConfigFailure` retains nonfatal lookup diagnostics in both warn
and ignore modes. Set `ImageConfigOptions.Transport` to a caller-owned
`http.RoundTripper` for a private CA or registry proxy; it also applies to registry
token exchange. Cancelling the build context stops the build.

Template `Env` and `SetEnvs` now default to build-only scope, matching the JS and
Python builders. Pass `true` to also persist a value into the sandbox runtime:

```go
builder := core.NewTemplateBuilder().FromImage("python:slim").
    Env("BUILD_MODE", "release").
    Run("echo $BUILD_MODE").
    SetEnvs(map[string]string{"APP_MODE": "production", "PATH": "/opt/app/bin:$PATH"}, true)
```

Declarations take effect in order. All values in one `SetEnvs` call expand against
the preceding environment; separate calls can refer to earlier declarations.
Dockerfile `ARG` is build-only and `ENV` persists at runtime. Named references
`$NAME` and `${NAME}` expand in builder declarations; other dollar expressions
stay literal. Image-provided ENV values are always literal. Runtime persistence
preserves inherited environment files, empty values and quoted values; declarations
scoped to a user stay scoped to that user. Inherited WORKDIR precedes caller steps,
and a later explicit `Workdir` changes the directory from that point onward.

Disable registry reads for offline builds or when you set all defaults yourself:

```go
builder := core.NewTemplateBuilder().FromImage("python:slim").InheritImageConfig(false)
// Or override at call time with core.BuildOptions{InheritImageConfig: core.Bool(false)}.
```

`FetchImageConfig` and `FetchImageConfigWithOptions` are available for explicit inspection. `TemplateBuilder.JSON()`
serializes the locally configured builder without making registry requests.
`BuildStatusWithOptions(ctx, templateID, buildID, &core.BuildStatusOptions{LogsOffset: 100})`
reads logs after an offset; the existing `BuildStatus` call remains available.

## Migration jobs

`client.Job` migrates sandboxes and distributes templates between modern Novita
clusters. The client's `Domain` or `APIURL` selects the target cluster;
`SourceRegion` identifies the source. It shares the facade's credentials, headers
and request timeout. Lower-level entry points are `core.NewJob(opts)` and
`client.Client.Jobs()`.

```go
client, err := novita.New(&core.SandboxOpts{Domain: "us-phx-1.sandbox.novita.ai"})
if err != nil {
    return err
}
jobID, err := client.Job.MigrateSandboxes(ctx, []string{"sandbox-a", "sandbox-b"},
    core.JobCreateOptions{SourceRegion: "source-region-id"})
if err != nil {
    return err
}
budget := 15 * time.Minute
status, err := client.Job.Wait(ctx, jobID, core.JobWaitOptions{
    Timeout: &budget,
    PollInterval: 5 * time.Second,
    OnProgress: func(s core.MigrationStatus) { fmt.Println(s.Status, s.Completed, s.Total) },
})
if err != nil {
    return err
}
fmt.Println(status.Items)
```

`MigrateSandbox` accepts one ID. Sandbox aliases are optional; use `TemplateAlias`
or `AliasForResource` to supply them. `DistributeTemplate` requires a nonblank
alias, and `DistributeTemplates` requires `AliasForResource` for multiple distinct
templates. Duplicate resource IDs are submitted once. Aliases are preserved
verbatim; do not set both alias options.

`Get`, `Cancel` and `List` expose status, per-item outcomes and cursor pagination.
`JobListOptions` supports resource ID, job type/status, source/target region, limit
and cursor. `Cancel` starts cooperative rollback: `reverting` remains nonterminal.
`Wait` defaults to 30 minutes and a 10-second poll interval. Its budget covers
requests, synchronous progress callbacks and sleeps; an explicit zero timeout
performs no request. Context cancellation cancels active requests and polling.

A `completed` job can contain failed items: inspect `Failed`, `Cancelled` and
`Items`. Terminal `failed`/`cancelled` jobs return `JobFailedError` /
`JobCancelledError` carrying the final status. Use `errors.As` to handle typed
errors, including alias conflicts, resources already migrating, shared
dependencies, cancellation past its allowed point, unsupported migration and
rate limits. Unknown server statuses and conflict reasons are retained.

## Architecture

The SDK talks to two planes, mirroring the JS and Python SDKs:

- **Control plane** — the Novita Sandbox API (`https://api.{domain}`), authenticated with
  `X-API-Key` or `Authorization: Bearer`. Used for sandbox lifecycle: create, connect, kill, info.
- **Data plane** — `envd` inside the sandbox (`https://49983-{sandboxID}.{domain}`),
  authenticated with `X-Access-Token` plus Basic auth for the user. Used for filesystem,
  commands, PTY, and streaming operations.

Data-plane calls speak the Connect RPC protocol with JSON encoding and need no
generated protobuf code. Secret host normalization rejects malformed ACE labels
and uses `golang.org/x/net/idna` to convert internationalized domain names to
ASCII before validation.

## Modules

| Module | Status | Description |
|---|---|---|
| `core` | available | Sandbox/template/volume/secret control plane, filesystem watchers, commands, PTY, and Git |
| `code_interpreter` | available | Jupyter execution, code contexts, MIME results, and typed charts |
| `desktop` | available | Display, screen, mouse, keyboard, windows, applications, and VNC/noVNC |

## Runtime compatibility

The SDK negotiates capabilities with `envd`, matching the other SDKs. See the
[runtime compatibility guide](docs/runtime-compatibility.md) for feature thresholds.

Requests carry `X-Novita-SDK-Name` and `X-Novita-SDK-Version` headers so the service can
track SDK usage.

## Development

The test suite needs no cloud credentials or external network. Tests drive the SDK against
in-process `httptest` servers that emulate the control plane and `envd`.

```bash
make test        # go test ./...
make test-race   # race detector with coverage
make vet
```

The required Go toolchain is declared in [go.mod](go.mod).

## License

MIT — see [LICENSE](../LICENSE).
