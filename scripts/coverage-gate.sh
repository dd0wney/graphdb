#!/usr/bin/env bash
#
# Fail if statement coverage in a Go coverage profile is below a floor.
#
# The coverage number was informational until now: CI produced it, uploaded it
# to Codecov with fail_ci_if_error:false, and nothing ever read it. A number
# nothing reads is not a gate.
#
# Usage: scripts/coverage-gate.sh <profile> <min-percent>
#
# Exit codes:
#   0  coverage meets or exceeds the floor
#   1  coverage is below the floor
#   2  the profile is missing, empty, or has no total line
#
# Exit 2 matters as much as exit 1. A gate that cannot find its input must say
# so, not pass by default.

set -euo pipefail

PROFILE="${1:-}"
MIN="${2:-}"

if [ -z "$PROFILE" ] || [ -z "$MIN" ]; then
  echo "usage: $0 <coverage-profile> <min-percent>" >&2
  exit 2
fi

if [ ! -s "$PROFILE" ]; then
  echo "coverage-gate: '$PROFILE' is missing or empty — refusing to report a pass" >&2
  exit 2
fi

TOTAL="$(go tool cover -func="$PROFILE" | awk '/^total:/ { gsub(/%/, "", $NF); print $NF }')"

if [ -z "$TOTAL" ]; then
  echo "coverage-gate: no total line in '$PROFILE' — refusing to report a pass" >&2
  exit 2
fi

awk -v total="$TOTAL" -v min="$MIN" 'BEGIN {
  if (total + 0 < min + 0) {
    printf("coverage-gate: FAIL — %.1f%% is below the %.1f%% floor\n", total, min)
    exit 1
  }
  printf("coverage-gate: ok — %.1f%% meets the %.1f%% floor\n", total, min)
}'
