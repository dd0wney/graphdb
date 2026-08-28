#!/usr/bin/env bash
#
# Per-package statement-coverage floors.
#
# `make coverage-gate` enforces one number over every package at once. That
# lets a well-covered package hide a poor one: pkg/query at 63.0% and
# pkg/parallel at 93.4% sit under the same gate, and the total clears it.
#
# Usage:
#   scripts/coverage-floors.sh [--root DIR] [--update] [profile]
#
#     --update   rewrite the floors file from this profile
#
# Exit codes:
#   0  every package meets its floor
#   1  a package is below its floor
#   2  the check could not run
#
# Exit 2 covers the case that matters most here: a package named in the floors
# file and absent from the profile. Skipping it silently would drop a package
# out of the gate and read as a pass.

set -uo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
UPDATE=0
while [ $# -gt 0 ]; do
  case "$1" in
    --root) ROOT="$(cd "$2" && pwd)"; shift 2 ;;
    --update) UPDATE=1; shift ;;
    *) break ;;
  esac
done

FLOORS="$ROOT/docs/internals/design/coverage-floors.tsv"
PROFILE="${1:-$ROOT/coverage/coverage.out}"

if [ ! -s "$PROFILE" ]; then
  echo "coverage-floors: '$PROFILE' is missing or empty — refusing to report a pass" >&2
  exit 2
fi
if [ "$UPDATE" = "0" ] && [ ! -s "$FLOORS" ]; then
  echo "coverage-floors: '$FLOORS' is missing or empty — refusing to report a pass" >&2
  exit 2
fi

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

# Per-package statement coverage, straight from the profile. A profile line is
#   <import path>/<file>.go:<range> <statements> <count>
# so the package is the path with the file name removed.
awk '
  /^mode:/ { next }
  {
    split($1, a, ":")
    n = split(a[1], parts, "/")
    pkg = parts[1]
    for (i = 2; i < n; i++) pkg = pkg "/" parts[i]
    total[pkg] += $2
    if ($3 + 0 > 0) covered[pkg] += $2
  }
  END {
    for (p in total) {
      # The full import path is kept. Matching is by suffix below, so the
      # module prefix does not have to be known — stripping a hard-coded
      # github.com/<owner>/<repo> made every lookup miss for any other module.
      printf "%s\t%.1f\n", p, (total[p] > 0 ? 100 * covered[p] / total[p] : 0)
    }
  }
' "$PROFILE" | sort > "$TMP/measured"

if [ ! -s "$TMP/measured" ]; then
  echo "coverage-floors: no packages found in '$PROFILE' — refusing to report a pass" >&2
  exit 2
fi

if [ "$UPDATE" = "1" ]; then
  {
    sed -n '1,/^# Columns:/p' "$FLOORS" 2>/dev/null || true
    while IFS=$'\t' read -r pkg pct; do
      floor=$(awk -v p="$pct" 'BEGIN { f = p - 2.0; if (f < 0) f = 0; printf "%.1f", f }')
      printf '%s\t%s\t%s\n' "$pkg" "$floor" "$pct"
    done < "$TMP/measured"
  } > "$TMP/newfloors"
  mv "$TMP/newfloors" "$FLOORS"
  echo "coverage-floors: rewrote '$FLOORS' from '$PROFILE'"
  echo "coverage-floors: IF THAT PROFILE CAME FROM A DEVELOPER MACHINE, DO NOT COMMIT IT." >&2
  exit 0
fi

FAIL=0
MISSING=0
while IFS=$'\t' read -r pkg floor measured; do
  case "$pkg" in ''|'#'*) continue ;; esac
  [ -z "${floor:-}" ] && continue

  # Suffix match, so "pkg/wal" does not also match "pkg/wal/apply".
  got="$(awk -F'\t' -v p="$pkg" '
    $1 == p { print $2; exit }
    substr($1, length($1) - length(p)) == "/" p { print $2; exit }
  ' "$TMP/measured")"
  if [ -z "$got" ]; then
    echo "coverage-floors: $pkg is in the floors file and not in the profile" >&2
    MISSING=1
    continue
  fi
  if awk -v g="$got" -v f="$floor" 'BEGIN { exit !(g + 0 < f + 0) }'; then
    printf '  FAIL  %-20s %5s%%  below the %s%% floor (CI measured %s%%)\n' "$pkg" "$got" "$floor" "$measured"
    FAIL=1
  else
    printf '  ok    %-20s %5s%%  floor %s%%\n' "$pkg" "$got" "$floor"
  fi
done < "$FLOORS"

if [ "$MISSING" = "1" ]; then
  echo "coverage-floors: at least one package in the floors file was not measured." >&2
  echo "coverage-floors: a package that drops out of the profile drops out of the gate." >&2
  exit 2
fi
exit "$FAIL"
