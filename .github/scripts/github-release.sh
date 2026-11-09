#!/usr/bin/env bash
set -euo pipefail
: "${TAG:?}" "${CHANNEL:?}" "${GITHUB_REPOSITORY:?}"
# Publish the Release only after the registry upload has succeeded.
if gh release view "$TAG" --repo "$GITHUB_REPOSITORY" >/dev/null 2>&1; then
  gh release upload "$TAG" release-assets/* --repo "$GITHUB_REPOSITORY"
else
  args=(release create "$TAG" --verify-tag --title "$TAG" --generate-notes --repo "$GITHUB_REPOSITORY")
  if [ "$CHANNEL" != stable ]; then args+=(--prerelease --latest=false); fi
  gh "${args[@]}" release-assets/*
fi
