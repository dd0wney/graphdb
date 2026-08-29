#!/usr/bin/env bash
#
# Re-record the statement-count floors in docs/internals/design/couplings.tsv
# from a coverage profile.
#
# The floor exists to catch a coupling being extracted out from under its
# registered symbol (see the header of dccc.sh). Run this when a coupling
# legitimately changes size, never to make a HOLLOWED report go away without
# looking at where the boundary went.
#
# A statement count is a property of the code, not of the machine that ran the
# tests, so unlike the coverage floors this may be recorded locally.
#
# Usage: scripts/dccc-update.sh [coverage-profile]

set -uo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
REGISTRY="$ROOT/docs/internals/design/couplings.tsv"
PROFILE="${1:-$ROOT/coverage/coverage.out}"

COUNTS="$(bash "$ROOT/scripts/dccc.sh" --update "$PROFILE")" || {
  echo "dccc-update: the measure could not run; the registry is unchanged" >&2
  exit 2
}
[ -n "$COUNTS" ] || { echo "dccc-update: no counts produced; registry unchanged" >&2; exit 2; }

TMP="$(mktemp)"
printf '%s\n' "$COUNTS" > "$TMP.counts"
awk -F'\t' -v counts="$TMP.counts" '
BEGIN {
  while ((getline line < counts) > 0) {
    split(line, a, "\t")
    if (a[3] + 0 > 0) seen[a[1] "." a[2]] = a[3] + 0
  }
}
/^#/ || NF == 0 { print; next }
{
  key = $3 "." $4
  if (key in seen) { $5 = seen[key]; changed++ }
  # A site the profile did not match keeps its previous floor rather than
  # being zeroed: an unmeasured coupling is not a shrunken one.
  printf "%s", $1
  for (i = 2; i <= NF; i++) printf "\t%s", $i
  printf "\n"
}
END { printf "dccc-update: rewrote %d floor(s)\n", changed > "/dev/stderr" }
' "$REGISTRY" > "$TMP"

mv "$TMP" "$REGISTRY"
rm -f "$TMP.counts"
