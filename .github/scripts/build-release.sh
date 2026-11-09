#!/usr/bin/env bash
set -euo pipefail
ROOT="$(git rev-parse --show-toplevel)"
cd "$ROOT"
mkdir -p release-assets
case "${COMPONENT:?component required}" in
  go)
    (cd sdk-go && go build ./... && go vet ./... && go test ./...)
    tar -czf "release-assets/novita-sandbox-go-$VERSION.tar.gz" -C sdk-go .
    ;;
  java)
    (cd sdk-java && mvn -B --no-transfer-progress -Prelease -Dgpg.skip=true -Dproject.build.outputTimestamp="$SOURCE_DATE_EPOCH" verify)
    cp sdk-java/target/novita-sandbox-"$VERSION"*.jar release-assets/
    ;;
  *) echo "Unknown component" >&2; exit 1 ;;
esac
python3 "$ROOT/.github/scripts/release.py" artifacts "$TAG" --directory "$ROOT/release-assets"
python3 "$ROOT/.github/scripts/hygiene_scan.py" "$ROOT/release-assets"
