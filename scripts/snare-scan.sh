#!/usr/bin/env bash
# Run graphdb's own snare rules (rules/snare/) over a Go tree.
#
# Exit 0: no finding outside _test.go files. Exit 1: findings, printed as
# file:line: rule: message. Exit 2: the scan did not run or did not finish,
# which is not a result about the code and must never read as a pass.
#
# Usage: SNARE=/path/to/snare scripts/snare-scan.sh [dir]   (dir defaults to pkg)
set -euo pipefail

SNARE="${SNARE:-snare}"
target="${1:-pkg}"
rules="${SNARE_RULES:-rules/snare}"

if ! command -v "$SNARE" >/dev/null 2>&1; then
  echo "snare-scan: snare binary not found (set SNARE=/path/to/snare); the scan did not run" >&2
  exit 2
fi
if ! command -v jq >/dev/null 2>&1; then
  echo "snare-scan: jq not found; the scan did not run" >&2
  exit 2
fi

out=$(mktemp)
trap 'rm -f "$out"' EXIT

set +e
"$SNARE" scan "$target" --config "$rules" --format json --no-cache >"$out"
rc=$?
set -e
# snare exits 0 when clean, 1 on findings, 2 on a usage or runtime error.
if [ "$rc" -ne 0 ] && [ "$rc" -ne 1 ]; then
  echo "snare-scan: snare exited $rc; the scan did not finish" >&2
  exit 2
fi

# _test.go files are left out: a lock a test holds through a panic fails that
# test, so the hazard the rules describe does not reach a running server.
# Test-support packages that are not _test.go files (pkg/vfs/vfstest) stay in.
if ! findings=$(jq -r --arg dir "$target" '
    (. // [])
    | map(select(.Source.File | endswith("_test.go") | not))
    | .[]
    | "\($dir)/\(.Source.File):\(.Source.StartLine): \(.RuleID): \(.Message)"' "$out"); then
  echo "snare-scan: cannot parse the snare output; the scan did not finish" >&2
  exit 2
fi

if [ -z "$findings" ]; then
  echo "snare-scan: no findings in $target (rules: $rules)"
  exit 0
fi
printf '%s\n' "$findings"
echo "snare-scan: $(printf '%s\n' "$findings" | wc -l) finding(s) in $target (rules: $rules)" >&2
exit 1
