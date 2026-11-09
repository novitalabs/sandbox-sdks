#!/usr/bin/env bash
set -euo pipefail
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="${1:-$(git -C "$HERE" rev-parse --show-toplevel)}"
exec python3 "$HERE/hygiene_scan.py" "$ROOT"
