# Novita Sandbox SDKs

Official Novita Agent Sandbox SDKs for Go and Java. Both SDKs expose the same
sandbox control-plane and runtime APIs while following the language conventions
of their respective ecosystems.

## SDKs

- [Go SDK](sdk-go/README.md) — module `github.com/novitalabs/sandbox-sdks/sdk-go/v2`
- [Java SDK](sdk-java/README.md) — Maven artifact `ai.novita:novita-sandbox`

Set `NOVITA_API_KEY` before creating a client. The default Novita Sandbox domain is
`sandbox.novita.ai`; applications can override it through the SDK options or
`NOVITA_DOMAIN` when they need to target another environment.

The source repository is [novitalabs/sandbox-sdks](https://github.com/novitalabs/sandbox-sdks).

## Development and releases

See [CONTRIBUTING.md](CONTRIBUTING.md), [RELEASING.md](RELEASING.md),
[CHANGELOG.md](CHANGELOG.md) and [package-consumer tests](release-smoke-tests/README.md).
Report vulnerabilities using [SECURITY.md](SECURITY.md).

## License

MIT — see [LICENSE](LICENSE). Both SDKs also include their license in the
component directory; Java binary/source archives carry META-INF/LICENSE; Javadoc archives include doc-files/LICENSE.
