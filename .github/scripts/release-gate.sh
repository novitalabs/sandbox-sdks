#!/usr/bin/env bash
set -euo pipefail
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TAG="${1:?tag required}"
if [ $# -gt 1 ]; then
  PREFIX=$(python3 "$HERE/release.py" describe "$TAG" | python3 -c 'import json,sys; print(json.load(sys.stdin)["prefix"])')
  EXPECTED="$2"
  case "$EXPECTED" in */v) ;; *) EXPECTED="$EXPECTED-" ;; esac
  [ "$PREFIX" = "$EXPECTED" ] || { echo "Tag prefix does not match workflow" >&2; exit 1; }
fi
TAG_COMMIT=$(git rev-parse --verify --end-of-options "refs/tags/$TAG^{commit}")
CHECKOUT_COMMIT=$(git rev-parse HEAD)
[ "$TAG_COMMIT" = "$CHECKOUT_COMMIT" ] || { echo "tag target does not match checkout" >&2; exit 1; }
BRANCH=$(python3 "$HERE/release.py" describe "$TAG" | python3 -c 'import json,sys; print(json.load(sys.stdin)["branch"])')
git fetch --no-tags origin "refs/heads/$BRANCH:refs/remotes/origin/$BRANCH"
python3 "$HERE/release.py" check "$TAG" --commit "${GITHUB_SHA:-HEAD}" --remote origin --github-env
