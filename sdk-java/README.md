# Novita Sandbox Java SDK

Java SDK for [Novita Agent Sandbox](https://novita.ai/sandbox) — isolated cloud sandboxes for running untrusted or AI-generated code, executing notebooks, and driving desktop environments.

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

Choose a published release from [Maven Central](https://central.sonatype.com/artifact/ai.novita/novita-sandbox)
and replace `RELEASE_VERSION` in the examples below. For Maven, add the dependency
to your `pom.xml`:

```xml
<dependency>
    <groupId>ai.novita</groupId>
    <artifactId>novita-sandbox</artifactId>
    <version>RELEASE_VERSION</version>
</dependency>
```

Or for Gradle:

```gradle
implementation 'ai.novita:novita-sandbox:RELEASE_VERSION'
```

Stable and prerelease versions use Maven Central. See [prerelease installation](../RELEASING.md#installing-a-java-prerelease) for the alpha dependency; no registry credentials are required.

## Quick Start

Get an API key from [key management](https://novita.ai/settings/key-management) and set it as an environment variable `NOVITA_API_KEY`.

Use `Novita` as the default entry point for applications and examples. Configure it
once, then use it to create or reconnect sandboxes and access templates, volumes,
and secrets. Static entry points such as `Sandbox.create` and
`CodeInterpreterSandbox.create` remain available for advanced integrations and
backward compatibility.

```java
import ai.novita.sandbox.core.Novita;
import ai.novita.sandbox.core.Sandbox;
import ai.novita.sandbox.core.CommandResult;

public class Example {
    public static void main(String[] args) throws Exception {
        try (Novita client = Novita.create();
             Sandbox sandbox = client.createSandbox("base")) {
            // Write a file
            sandbox.files().write("/tmp/hello.txt", "Hello, World!".getBytes(), null);

            // Run a command
            CommandResult result = sandbox.commands().run("cat /tmp/hello.txt", null);
            System.out.println(result.getStdout());
        }
    }
}
```

## Modules

The SDK is organized into three main modules:

- **core**: Core sandbox functionality (lifecycle, filesystem, commands)
- **code_interpreter**: Jupyter-style code execution
- **desktop**: Desktop environment automation

## Documentation

- [Sandbox Overview](https://novita.ai/docs/guides/sandbox-overview)
- [Changelog](https://novita.ai/docs/changelog)
- [Implemented APIs and protocol verification](docs/api-parity.md)
- [Local package-consumer and live acceptance tests](../release-smoke-tests/README.md)

## Streaming files and interpreter results

File streams let applications avoid buffering an entire file. Close the returned
input stream when finished; the SDK does not close an input stream supplied to
`writeStream`. Upload cancellation is checked between reads. If your custom
input stream can block indefinitely, provide its own read deadline or cancellation.

```java
try (java.io.InputStream input = sandbox.files().readStream("/tmp/output.bin")) {
    java.nio.file.Files.copy(input, java.nio.file.Path.of("output.bin"));
}
```

Create a sandbox through `Novita`, then wrap it as a code interpreter to use
NDJSON callbacks, rich results and structured execution errors:

```java
import ai.novita.sandbox.core.Novita;
import ai.novita.sandbox.code_interpreter.CodeInterpreterSandbox;
import ai.novita.sandbox.code_interpreter.RunCodeOpts;
import ai.novita.sandbox.code_interpreter.ExecutionResult;

try (Novita client = Novita.create();
     CodeInterpreterSandbox interpreter =
         CodeInterpreterSandbox.wrap(client.createSandbox("code-interpreter-v1"))) {
    ExecutionResult result = interpreter.runCode("print('hello')\n6 * 7",
        new RunCodeOpts().setOnStdout(System.out::print));
    if (result.getExecutionError() != null) {
        throw new IllegalStateException(result.getError());
    }
    System.out.println(result.getRichResults().get(0).getText());
}
```

To reconnect with the same configuration, use
`CodeInterpreterSandbox.wrap(client.connectSandbox(sandboxId))`.

Closing a sandbox wrapper terminates its sandbox. Closing `Novita` releases the
facade's shared HTTP client; sandboxes have their own clients and must still be
closed or killed. A command handle's
`disconnect()` only detaches its stream and leaves the process running; reconnect
to its PID to continue observing it. Command and interpreter timeouts bound the
actual network operation, and zero explicitly disables the timeout.

## Git request options

Use an independent Git view to select a repository and per-call command options.
The view copies its options, so another caller's repository or environment is
unaffected:

```java
ai.novita.sandbox.core.CommandOpts options = new ai.novita.sandbox.core.CommandOpts();
options.setWorkDir("/home/user/project");
options.setUser("user");
options.setTimeout(30);
options.setRequestTimeoutSeconds(10);
ai.novita.sandbox.core.Git git = sandbox.git().withOptions(options);

ai.novita.sandbox.core.GitAuth auth = new ai.novita.sandbox.core.GitAuth()
    .setUsername("git-user").setPassword(System.getenv("GIT_TOKEN"));
git.pull(new ai.novita.sandbox.core.GitPullOptions()
    .setRemote("origin").setBranch("main").setAuth(auth).setRebase(true));
```

`GitPushOptions` additionally supports upstream tracking, and `GitCloneOptions`
supports shallow clone depth. Credentials are supplied through the command
environment and are not stored in the repository. Existing no-argument push
and pull methods remain available.

## Long-running sandboxes

Set `long_running` to the exact string `"true"` in `SandboxOpts.metadata` to omit
the sandbox lifetime timeout during creation. This applies to modern and legacy
domains. An explicit `idle_timeout` in the metadata is preserved, and the SDK adds
no default idle timeout for long-running sandboxes. Request timeouts still apply.

## Base image configuration

`fromImage` enables inheritance of the image's ENV, WORKDIR and ENTRYPOINT/CMD.
The builder reads registry metadata when serialized or built, with explicit
builder settings taking precedence. For local serialization without registry
access, use `fromImage("debian:12", false)` or `inheritImageConfig(false)`.

Mutable image tags and failed lookups are refreshed on each serialization or
build. Successful digest-pinned lookups are reused until the image, credentials or
registry HTTP client changes. An inherited WORKDIR is applied before caller
instructions; a later explicit `workdir` changes the directory at that step.

`onConfigFailure(ImageConfigFailurePolicy.WARN)` is the default: lookup failures
produce a warning and the build continues without image defaults. `RAISE` throws
`ImageConfigException`; `IGNORE` continues quietly. All modes retain diagnostics
in `getImageConfigFailure()`. Use `imageConfigHttpClient(okhttp3.OkHttpClient)` to
supply registry TLS trust or proxy configuration, including for token exchange.
The caller retains ownership of that client.

Generic registry Basic/Bearer authentication and GCP service-account JSON are
supported for these lookups. AWS, OCI and Huawei registry helpers retain their
server-side build configuration; they do not perform cloud-specific token
exchange locally.

Template `environment`, `environments` and `setEnvs` now default to build-only
scope, matching the JS and Python builders. Pass `true` to also persist values
into the sandbox runtime:

```java
TemplateBuilder builder = TemplateBuilder.create().fromImage("python:slim")
    .environment("BUILD_MODE", "release")
    .run("echo $BUILD_MODE")
    .setEnvs(java.util.Map.of("APP_MODE", "production", "PATH", "/opt/app/bin:$PATH"), true);
```

Declarations take effect in order. Values in one map expand against the preceding
environment; separate calls can refer to earlier declarations. Dockerfile `ARG`
is build-only and `ENV` persists at runtime. Named references `$NAME` and `${NAME}`
expand in builder declarations; other dollar expressions stay literal.
Image-provided ENV values are literal. Runtime persistence preserves inherited
environment files, empty values and quoted values, and respects user scopes.

Filesystem streams avoid whole-file buffering. Template COPY hashing and tar
assembly still buffer the build context and need separate sizing checks for
large contexts.

`BuildFailureReason.getStep()` now returns the API's step text. Consumer code
that treated it as an integer must use a string.

## Migration jobs

`client.job()` (also `jobs()`) migrates sandboxes and distributes templates between
modern Novita clusters. The client's domain/base URL selects the target cluster;
`sourceRegion` identifies the source. Requests share the facade's credentials,
headers and timeout configuration.

```java
import ai.novita.sandbox.core.*;
import java.time.Duration;
import java.util.Arrays;

SandboxOpts options = new SandboxOpts();
options.setDomain("us-phx-1.sandbox.novita.ai");
try (Novita client = Novita.create(options)) {
    String jobId = client.job().migrateSandboxes(Arrays.asList("sandbox-a", "sandbox-b"),
        new JobCreateOptions().setSourceRegion("source-region-id"));
    MigrationStatus status = client.job().waitFor(jobId, new JobWaitOptions()
        .setTimeout(Duration.ofMinutes(15))
        .setPollInterval(Duration.ofSeconds(5))
        .setOnProgress(progress -> System.out.println(progress.getStatus())));
    System.out.println(status.getItems());
}
```

`migrateSandbox` accepts one ID. Sandbox aliases are optional; use
`setTemplateAlias` or `setAliasForResource`. `distributeTemplate` requires a
nonblank alias, and `distributeTemplates` requires `setAliasForResource` for
multiple distinct templates. Resource IDs are deduplicated and aliases are
preserved verbatim. Set only one alias option.

`get`, `cancel` and `list` expose status, per-item outcomes and cursor pagination.
`JobListOptions` supports resource ID, job type/status, source/target region, limit
and cursor. Cancellation starts cooperative rollback; `reverting` is nonterminal.
`waitFor` defaults to a 30-minute budget and 10-second interval. The budget covers
requests, synchronous progress callbacks and polling delays; `Duration.ZERO`
performs no request. Every operation has a `CompletableFuture` variant such as
`migrateSandboxAsync` or `waitForAsync`. Cancelling that future with `cancel(true)`
cancels active HTTP work and interrupts polling.

`completed` can include failed items, so inspect the counters and `getItems()`.
`JobFailedException` and `JobCancelledException` retain the final status. Conflict
subtypes distinguish alias conflicts, resources already migrating, shared
dependencies and cancellation past its allowed point; unknown reasons and
statuses are retained. `MigrationNotSupportedException` identifies unsupported
clusters, including legacy domains.

`Job.create(options)` is an independent, closeable client. Closing a Job obtained
from `client.job()` leaves the owning Novita client open; close the facade to
release its shared resources.

## License

MIT — see [LICENSE](../LICENSE)
