#!/usr/bin/env bash
set -euo pipefail
VERSION=8.30.1
case "$(uname -s)" in Linux) OS=linux ;; Darwin) OS=darwin ;; *) echo 'Install Gitleaks 8.30.1 manually on this platform' >&2; exit 1 ;; esac
case "$(uname -m)" in x86_64|amd64) ARCH=x64 ;; arm64|aarch64) ARCH=arm64 ;; *) echo 'Unsupported Gitleaks architecture' >&2; exit 1 ;; esac
DIR=$(mktemp -d)
trap 'rm -rf "$DIR"' EXIT
NAME="gitleaks_${VERSION}_${OS}_${ARCH}.tar.gz"
curl --fail --silent --show-error --location "https://github.com/gitleaks/gitleaks/releases/download/v${VERSION}/${NAME}" -o "$DIR/$NAME"
curl --fail --silent --show-error --location "https://github.com/gitleaks/gitleaks/releases/download/v${VERSION}/gitleaks_${VERSION}_checksums.txt" -o "$DIR/checksums"
if command -v sha256sum >/dev/null 2>&1; then
  (cd "$DIR" && grep " $NAME$" checksums | sha256sum --check -)
else
  (cd "$DIR" && grep " $NAME$" checksums | shasum -a 256 --check -)
fi
tar -xzf "$DIR/$NAME" -C "$DIR" gitleaks
if [ -n "${RUNNER_TEMP:-}" ]; then TARGET="$RUNNER_TEMP/release-tools"; else TARGET="$(git rev-parse --git-path release-tools)"; fi
mkdir -p "$TARGET"
cp "$DIR/gitleaks" "$TARGET/gitleaks"
if [ -n "${GITHUB_PATH:-}" ]; then echo "$TARGET" >> "$GITHUB_PATH"; fi
echo 'Installed Gitleaks 8.30.1 for release checks'
