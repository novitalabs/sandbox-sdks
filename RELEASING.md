# Go and Java releases

| Component | Package | Tag example |
| --- | --- | --- |
| Go | github.com/novitalabs/sandbox-sdks/sdk-go | sdk-go/v0.0.1 |
| Java | ai.novita:novita-sandbox | java-2.1.2-alpha.5 |

Stable versions come from main, beta versions from beta, and alpha/rc versions
from alpha. Tags must match the component version in Go SDKVersion or the Java
POM and must point to a commit on the appropriate remote branch. Released
versions and tags are immutable. The examples describe the current metadata,
not permission to reuse an existing published version.

Before tagging, run CI, isolated [package consumers](release-smoke-tests/README.md)
and the appropriate live acceptance tests. Install the local guard using
`git config core.hooksPath .githooks`, then install its scanner with
`bash .github/scripts/install-gitleaks.sh`. Push the release branch before
pushing its component tag. Configure GitHub rulesets to restrict tag creation
and block tag updates/deletions; local hooks alone cannot enforce policy.

Go tags make source modules downloadable. Public installation uses the normal
Go proxy/checksum service without GitHub credentials. The Go team verifies
module availability separately after tagging; proxy propagation does not block
the GitHub Release. An existing immutable tag can be published with the manual
Publish validated component workflow if its original run failed.

## Installing a Java prerelease

Java stable and prerelease versions publish signed binary/source/Javadoc JARs
and POMs to Maven Central. Binary/source JARs include META-INF/LICENSE; the Javadoc JAR includes doc-files/LICENSE. Consumers need no
GitHub Packages settings or registry credentials. For example:

```xml
<dependency>
  <groupId>ai.novita</groupId>
  <artifactId>novita-sandbox</artifactId>
  <version>2.1.2-alpha.5</version>
</dependency>
```

## Publisher setup

Verify the ai.novita namespace in the Central Publisher Portal and grant the
publishing account access. Configure these GitHub Actions secrets:

- CENTRAL_USERNAME and CENTRAL_TOKEN: Central User Token credentials.
- MAVEN_GPG_PRIVATE_KEY and MAVEN_GPG_PASSPHRASE: the signing key and passphrase.

Publish the public signing key through a Central-supported keyserver. Never
commit private keys or tokens. GitHub Releases use GITHUB_TOKEN with contents
write access. CI on fork pull requests requires no secrets.

The release workflows reject invalid tags, mismatched coordinates/versions and
already-published Java versions. They build/test the component, verify archives,
scan distributable contents, publish and retain artifacts for 90 days. Java
publication waits until Central reports published and its registry bytes match
the build. No release workflow is triggered by a normal branch push.

If upload succeeds but verification or GitHub Release creation fails, inspect
the existing registry artifacts and recover using the original workflow's
matching files. Do not overwrite versions or move/delete Go tags. Use a new
version when a source or artifact change is required. Publication remains a
maintainer action.
