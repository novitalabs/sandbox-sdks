# Java SDK API Parity Matrix

This matrix describes the Java 11 SDK's implemented surface and protocol
fixtures. It is not a claim that every operation has been accepted by a live
service. The consumer tests under `release-smoke-tests/java` additionally load
the installed JAR from a separate Maven project and use only public APIs.
The wire contracts are covered by the protocol fixtures in this SDK.
The runtime service names are `process.Process` and `filesystem.Filesystem`.

| Area | Java surface | Wire contract | Lifecycle/error coverage |
| --- | --- | --- | --- |
| Root facade | `Novita.createSandbox`, `connectSandbox`, `listSandboxes`, `template()`, `volume()`, `secret()`, `job()`; separate `CodeInterpreterSandbox` / `DesktopSandbox` entry points | Shared control-plane options and module clients | Facade configuration is inherited; module wrappers expose public create/connect methods |
| Sandbox control plane | `create`, `connect/get`, `list`, `kill`, `pause`, `clone`, `reset`, `commit`, `setNetwork`, `setTimeout`, `info`, `metrics`, `quota`, `events`, `resize`, snapshot APIs | `/sandboxes`, `/sandboxes/{id}`, action endpoints, `/snapshots`, legacy domain rules | Encoded IDs/query; array and envelope pagination; 401/404/429 typed errors; list must not create per-item clients |
| Migration jobs | `job()`/`Job.create`, migrate/distribute/get/list/cancel/wait and async variants | `/v1/migrations`, `/v1/migrations/cancel`; snake_case requests, camelCase status/page responses | Alias rules, duplicate IDs, reason-specific errors, reverting and per-item results, request/callback/poll deadlines, HTTP cancellation, no automatic accept replay |
| Sandbox URLs | `portUrl`, upload/download URL helpers, MCP URL/token | `<port>-<sandboxId>.<domain>` plus traffic/access headers and signatures | New and legacy domains; cached runtime metadata invalidated by reset |
| Template builder | image/template/registry bases; copy/remove/rename/mkdir/symlink; run/package installs; MCP/Git/devcontainer; env/workdir/user/start/ready/patch; skip cache | Ordered `steps`; `fromImage`/`fromTemplate`; SDK `patchCmd` maps to `preBootScript`; only Debian + `fromImage` applies patch | Literal serialization fixtures; no duplicate/reordered steps; invalid combinations rejected |
| Image configuration and environment | `onConfigFailure`, `imageConfigHttpClient`, `environment`/`setEnvs` with runtime flag | Generic registry auth; inherited WORKDIR before caller steps; ordered ENV groups, build-only default | Custom TLS, warning/raise/ignore, mutable-tag refresh, digest cache, literal image values, runtime shell fixtures |
| Template lifecycle | `list/get/delete/build/buildInBackground/getBuildStatus/exists/aliasExists/tags` | `/v3/templates` allocate, files hash/cache, presigned tar upload, `/v2/templates/{template}/builds/{build}`, polling/log offset | `.dockerignore`, glob, symlink, gzip tar, cache hit, timeout, failure reason, incremental log tests |
| Volume | `create/connect/get/list/updateQuota/delete` | `/volumes`, `/volumes/{id}` (quota via `PATCH`), create omits unset quota fields | Disabled-on-legacy handling; quota wire fields; typed errors; no unrelated resource cleanup |
| Secret | `create/list/get/update/delete` | `/secrets`, `/secrets/{encodedName}` | Name/host validation, encoded names, legacy rejection, typed errors |
| Filesystem REST | `read/readText/readStream/write/writeText/writeFiles/writeStream` | envd `/files`; multipart on old envd, octet-stream on envd `>=0.5.7`; user/path/mode query | Binary preservation, stream ownership, gzip, empty body, 400/401/404/507 mapping, response metadata |
| Filesystem RPC | `list/stat/exists/makeDir/move/remove/watch/createWatcher/getWatcherEvents/removeWatcher` | `filesystem.Filesystem/{ListDir,Stat,MakeDir,Move,Remove,WatchDir,CreateWatcher,GetWatcherEvents,RemoveWatcher}` | JSON proto field aliases, start/keepalive/data events, recursive version gate, cancel and close |
| Commands | `run/start/connect/list/kill/sendSignal` and `CommandHandle.waitFor/sendStdin/closeStdin` | `process.Process/{Start,Connect,List,SendInput,CloseStdin,SendSignal}` | Start event before return, real PID, stdout/stderr/PTY base64 data, end/error event, non-zero exit, EOF/cancel |
| PTY | `start`, input, resize, wait, kill | `process.Process/Start`, `/SendInput`, `/Update`, `/SendSignal` | PTY input uses returned PID; reject after exit; delayed PID and close ordering tests |
| Git | clone/init/status/branches/add/commit/push/pull/checkout/create/delete/reset/remote/config/restore; `withOptions` scoped request configuration; clone depth; push/pull remote, branch and auth options | Commands/envd process API; credentials supplied through environment, never argv | Shell escaping, request-option isolation, auth, upstream selection and non-zero exit |
| Code Interpreter | create/connect/wrap, run code, context create/list/restart/remove | Jupyter port `49999`, `/execute`, `/contexts`, sandbox access headers | NDJSON records, stdout/stderr, structured errors, rich results, execution count, callbacks and cancellation |
| Desktop | create/connect, screenshot, mouse, keyboard, text, drag, wait, open, windows, launch, VNC | Commands + Filesystem + public noVNC port | Exact shell escaping, PNG bytes, display startup errors, VNC start/stop and sandbox cleanup |
| Connect transport | unary, server stream, cancellable stream | Connect JSON; 5-byte envelopes; end-stream trailer flag `0x02` | Data, empty trailer, error trailer, compressed flag rejection, invalid length, truncated frame, EOF and cancellation |

## Required fixture assertions

- A sandbox list response may be a bare array or an object envelope and never
  constructs an envd client for each entry.
- Connect server streams accept data followed by an empty or metadata-only
  end-stream trailer, decode a nested `error` as `ConnectRpcException`, reject malformed
  or truncated frames, and cancel the actual HTTP call.
- A process handle does not return from `start` until the PID event is known;
  PTY/stdin/update/signal requests use that PID and fail after terminal exit.
- File upload preserves bytes and server-returned metadata across legacy
  multipart and modern octet-stream modes.
- Template serialization preserves the order of literal steps and maps
  `setPatchCmd` to `preBootScript`; tar/hash/cache/log behavior is tested with
  fixed filesystem fixtures.
- Code interpreter and desktop tests assert parsed consumer results and binary
  screenshots, rather than only asserting that a mock endpoint was called.

## Control-plane wire contracts (ControlPlaneProtocolTest)

| Operation | Endpoint and body | Parity notes |
| --- | --- | --- |
| `Sandbox.create` | `POST /sandboxes` | Modern: `templateID`, `metadata?`, `envVars?`, `timeout` (default 300, omitted when metadata `long_running` is `"true"`; explicit `idle_timeout` is preserved), `secure` (default true), `allow_internet_access` (default true), `autoPause?`, `autoResume:{enabled}?`, `network?`, `secrets?`, `mcp?`, `volumeMounts:[{name,path}]?`, `nodeID?`. Legacy: defaults `secure:false`, `autoPause:false`; only `network.allowPublicTraffic` is accepted, including explicit false, while other network fields and `mcp`/`volumeMounts`/`secretEnvs` are rejected. `envdVersion < 0.1.0` kills the sandbox and raises `TemplateException`. |
| `Sandbox.connect` | `POST /sandboxes/{id}/connect` | `{timeout, secure?, allowPublicTraffic?|network?}`; the response supplies domain/envd credentials. No extra connect-after-create call. |
| `Sandbox.reset` | `POST /sandboxes/{id}/reset` | `{resume:true default, secure?, allowPublicTraffic?|network?, timeout?}`; rotated `trafficAccessToken` is persisted. |
| `mountVolume`/`unmountVolume` | `POST /sandboxes/{id}/volume-mounts` `{name,path}`; `DELETE ...?path=&force=true` | Rejected on legacy domains; both return `SandboxInfo`. |
| `clone` | `POST /sandboxes/{id}/clone` `{count,strict,nodeID?,timeout?}` | Response `sandboxes[]` with `sandboxID`+`clientID` concatenation, `snapshotTemplateId`; each clone owns its control-plane client. |
| `commit` | `POST /sandboxes/{id}/commit` `{alias?}` | Legacy-only; modern clusters raise. |
| `getEvents` | `GET /sandboxes/events` | Query `startTime, endTime, sandboxID, templateID, events, state, orderAsc, offset, limit` in JS/Python order; returns `{items,total,hasMore}`. |
| `list` | `GET /v2/sandboxes` | Query `metadata` (pairs encoded like JS `URLSearchParams`/Python `urlencode(quote(...))`), `state` (comma-joined), `limit`, `nextToken`; pagination via `x-next-token` header; client ID appended to the sandbox ID only on legacy clusters; listed fields (`alias`, `startedAt`, `endAt`, `cpuCount`, `memoryMB`, `volumeMounts`) are mapped. |
| `isRunning` | envd `GET /health` | 502 → `false`; `X-Access-Token` plus Basic auth headers. |
| `uploadUrl`/`downloadUrl` | envd `/files?...` | v1 signature `sha256(path:operation:user:token[:expiration])` → base64 → strip `=` → `v1_` prefix; `+` percent-encoded like JS `URLSearchParams`/Python `quote_via=quote`; expiration requires a secure sandbox; envd `< 0.4.0` defaults `username=user`. |
| `getMcpUrl`/`getMcpToken` | `https://{host}:50005/mcp` | `getMcpToken` reads `/etc/mcp-gateway/.token` as root through the envd files API and caches it; both raise on legacy domains. |
| `setNetwork` | `PUT /sandboxes/{id}/network` | Body carries only the explicitly set `allowOut`/`denyOut` lists; modern-only. |
| `pause`/`kill`/`reset` | `POST .../pause`, `DELETE /sandboxes/{id}`, `POST .../reset` | JS/Python boolean semantics: `pause` 409 → `false` (legacy body `{"sync":false}`), `kill` 404 → `false`, `reset` 409 → `false` via the `success` flag. |
| `createSnapshot`/`listSnapshots` | `POST /sandboxes/{id}/snapshots` `{}`; `GET /snapshots?sandboxID&limit&nextToken` | Create reads `snapshotID`; list is a static call paginated via the `x-next-token` header; both raise on legacy domains. |
| `metrics` | `GET /sandboxes/{id}/metrics?start&end` | `start`/`end` are epoch-second filters; the legacy path normalizes `memUsedMiB`/`memTotalMiB` to bytes and reports disk as `-1`. |
| Volume quota | `PATCH /volumes/{id}` `{quotaSizeGiB, quotaInodes}` | `quotaInodes` defaults to `quotaGiB * 1000`; create omits quota fields unless provided; all volume operations are modern-only; `delete` 404 → `false`. |
| Secrets | `GET /secrets`, `PUT /secrets/{name}`, `DELETE /secrets/{name}` | List parses the `{secrets:[...]}` envelope; the update body is `{value, hosts, description?}` with the name only in the URL; hosts are trimmed, lowercased and deduplicated; delete returns the `deleted` field; names follow `^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`. |
| Template trigger | `POST /v2/templates/{template}/builds/{build}` | Body is exactly `{startCmd, readyCmd, preBootScript, steps, force, fromImage?, fromTemplate?, fromImageRegistry?}` — no alias/tags/envVars; `force` is always serialized; build status polls send `logsOffset` as a query parameter; `exists` maps 404 → `false` and 403 → `true`; `assignTags` returns `{buildID, tags}`. |

## Compatibility constants

| Capability | envd version |
| --- | --- |
| Recursive watch | `0.1.4` |
| Command stdin | `0.3.0` |
| Server default user (Basic auth no longer implicit) | `0.4.0` |
| Close stdin | `0.5.2` |
| Octet-stream upload | `0.5.7` |

## Verification boundaries

Run `mvn test` for the SDK fixtures. Run the repository's
`release-smoke-tests/local_packages.py --language java` for the independent
Maven consumer. The default tests use local protocol fixtures and no cloud
credentials. The opt-in live consumer covers create, files, commands,
interpreter output, context persistence, reconnect and cleanup.

Live X11/VNC rendering, registry build acceptance, large-file performance and
each cluster's volume/secret permissions require separate integration checks.

`BuildFailureReason.getStep()` returns a string, matching the API wire format.
Callers that previously assumed an integer step index must use that string.
Rich execution results and structured errors are available alongside the
existing map/string accessors to preserve existing consumer code.
