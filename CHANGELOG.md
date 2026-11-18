# Changelog

## Unreleased

- Add sandbox resource targets: Go and Java can request absolute CPU, memory and
  disk totals at creation time and resize a running sandbox, reporting each
  requested resource's outcome.
- **Breaking:** replace the incremental memory-only `resize` entry points in both
  SDKs with the resource API. `ResizeSandbox`/`Resize` (Go) and
  `resize`/`resizeAsync` (Java) now take absolute CPU, memory and disk totals and
  return per-resource results; the old `/hotplug-memory` call is removed. See the
  migration notes in each SDK README.
- Restrict this repository to the Go and Java SDKs and their build, test and release tooling.
- Keep the Java package coordinates and version; initialize the Go module at
  `github.com/novitalabs/sandbox-sdks/sdk-go` with version `v0.0.1`.
- Require Go 1.25, update IDNA dependencies, and reject malformed secret host labels.

See each SDK README for supported operations and current installation instructions.
