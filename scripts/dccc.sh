#!/usr/bin/env bash
#
# Data- and control-coupling coverage.
#
# Nine of the thirteen defects found on 2026-08-28 were coupling defects, and
# not one would have been caught by raising statement or branch coverage inside
# a package. DO-178C separates this from structural coverage for that reason:
# 6.4.4.d asks for coverage of the interfaces between components, because unit
# coverage verifies behaviour inside one and cannot verify that two interact
# correctly once integrated.
#
# The mechanically useful part is narrow: define the interfaces well enough to
# write test cases against them, then measure statement coverage restricted to
# the statements that cross a boundary. docs/internals/design/couplings.tsv is
# that definition. This measures it.
#
# Usage: scripts/dccc.sh [--root DIR] [coverage-profile]
#        (default root is the repository, default profile coverage/coverage.out)
#
# Exit codes:
#   0  a number was produced
#   1  a coupling has no covered statement at all
#   2  the measure could not run
#
# There is no threshold. A floor set before the number is understood is how the
# coverage floor was got wrong in #469.

set -uo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
if [ "${1:-}" = "--root" ]; then
  ROOT="$(cd "$2" && pwd)"
  shift 2
fi
REGISTRY="$ROOT/docs/internals/design/couplings.tsv"
PROFILE="${1:-$ROOT/coverage/coverage.out}"

if [ ! -s "$REGISTRY" ]; then
  echo "dccc: '$REGISTRY' is missing or empty — refusing to report a number" >&2
  exit 2
fi
if [ ! -s "$PROFILE" ]; then
  echo "dccc: '$PROFILE' is missing or empty — refusing to report a number" >&2
  echo "dccc: generate it with 'make test-cover'" >&2
  exit 2
fi

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

# Resolve each symbol to a file and a line range. gofmt puts the closing brace
# of a top-level func at column 0, so the body runs from "func ... Name(" to
# the next line that is exactly "}".
UNRESOLVED=0
: > "$TMP/sites"
while IFS=$'\t' read -r id kind pkg sym rest; do
  case "$id" in ''|'#'*) continue ;; esac
  [ -z "${sym:-}" ] && continue

  found=""
  for f in "$ROOT/$pkg"/*.go; do
    case "$f" in *_test.go) continue ;; esac
    [ -e "$f" ] || continue
    start="$(awk -v s="$sym" '$0 ~ "^func .*[ .*)]" s "\\(" || $0 ~ "^func " s "\\(" { print NR; exit }' "$f")"
    [ -z "$start" ] && continue
    end="$(awk -v st="$start" 'NR>=st && $0=="}" { print NR; exit }' "$f")"
    [ -z "$end" ] && continue
    found="$f"
    printf '%s\t%s\t%s\t%s\t%s\t%s\t%s\n' "$id" "$kind" "$pkg" "$sym" "$start" "$end" "$(basename "$f")" >> "$TMP/sites"
    break
  done

  if [ -z "$found" ]; then
    echo "dccc: $id names $pkg.$sym, which no file in that package defines" >&2
    UNRESOLVED=1
  fi
done < "$REGISTRY"

if [ "$UNRESOLVED" = "1" ]; then
  echo "dccc: the registry and the code disagree — refusing to report a number" >&2
  exit 2
fi
if [ ! -s "$TMP/sites" ]; then
  echo "dccc: no sites resolved from '$REGISTRY' — refusing to report a number" >&2
  exit 2
fi

# Sum the profile's statement blocks that fall inside each resolved range.
awk -v profile="$PROFILE" '
BEGIN { FS="\t" }
{
  id[NR]=$1; kind[NR]=$2; pkg[NR]=$3; sym[NR]=$4; lo[NR]=$5; hi[NR]=$6; base[NR]=$7; n=NR
}
END {
  while ((getline line < profile) > 0) {
    if (line ~ /^mode:/) continue
    split(line, a, " ")
    stmts = a[2] + 0; count = a[3] + 0
    split(a[1], b, ":")
    path = b[1]
    split(b[2], c, ",")
    split(c[1], d, ".")
    startline = d[1] + 0
    for (i = 1; i <= n; i++) {
      # The path AND the file must both match. Matching the package alone
      # counted every block at the same line numbers in a different file of
      # the package, which inflated a 20-statement function to 634.
      if (index(path, pkg[i] "/" base[i]) == 0) continue
      if (startline < lo[i] || startline > hi[i]) continue
      total[i] += stmts
      if (count > 0) covered[i] += stmts
    }
  }

  print "  covered/total  coupling"
  gt = 0; gc = 0; empty = 0; unmatched = 0
  for (i = 1; i <= n; i++) {
    t = total[i] + 0; cv = covered[i] + 0
    gt += t; gc += cv
    # A site with no statements at all is not a coverage result. Every real
    # function has statements, so zero means nothing matched: the profile does
    # not cover that package, or the resolver found the wrong file. Reporting
    # it as "n/a" alongside real percentages is how a half-written profile got
    # read as a finished one.
    if (t == 0) {
      printf "  %4s/%-5s %s  %-4s %s %s.%s\n", "-", "-", " UNMATCHED", id[i], kind[i], pkg[i], sym[i]
      unmatched++
      continue
    }
    pct = sprintf("%5.1f%%", 100 * cv / t)
    printf "  %4d/%-5d %s  %-4s %s %s.%s\n", cv, t, pct, id[i], kind[i], pkg[i], sym[i]
    if (cv == 0) empty++
  }
  printf "\n  %d/%d statements across %d coupling sites", gc, gt, n
  if (gt > 0) printf " = %.1f%%", 100 * gc / gt
  printf "\n"
  if (unmatched > 0) {
    printf "\n  %d coupling site(s) matched no statement in the profile.\n", unmatched
    printf "  That is not a coverage result. Either the profile does not include\n"
    printf "  those packages, or it was read while it was still being written.\n"
    exit 2
  }
  if (empty > 0) {
    printf "\n  %d coupling site(s) have no covered statement at all.\n", empty
    exit 1
  }
}
' "$TMP/sites"
