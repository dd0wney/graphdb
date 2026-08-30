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
#   3  a coupling shrank below its recorded statement floor
#
# There is no COVERAGE threshold. A floor set before the number is understood
# is how the coverage floor was got wrong in #469.
#
# There IS a STATEMENT-COUNT floor, column 5 of the registry, and it exists
# because of a defect this measure could not see. Stage 4 of ADR 0002 extracted
# openMmapSnapshot's body into openMmapSnapshotWithFS. The registered symbol
# still existed, still resolved, and still reported coverage — of the one line
# that remained. The coupling went from 26/33 (78.8%) to 1/1 (100.0%) and the
# measure called that an improvement. A gate that rewards you for removing its
# subject is worse than no gate.
#
# Unlike a coverage percentage, a statement count is a property of the code and
# not of the machine that ran the tests, so this floor may be recorded locally.
# That is the whole difference from #469. Re-record with --update after a change
# that legitimately alters a coupling's size.

set -uo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
if [ "${1:-}" = "--root" ]; then
  ROOT="$(cd "$2" && pwd)"
  shift 2
fi
UPDATE=0
if [ "${1:-}" = "--update" ]; then
  UPDATE=1
  shift
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
#
# A symbol may be written bare ("CreateNodeWithTenant") or qualified with its
# receiver type ("GraphStorage.CreateNodeWithTenant"). Qualification exists
# because two files in the same package can declare a same-named method on
# different receivers (C4: pkg/storage/btree_storage.go's
# BTreeGraphStorage.CreateNodeWithTenant and
# pkg/storage/node_operations.go's GraphStorage.CreateNodeWithTenant). Taking
# "the first file that matches, then break" silently measured whichever file
# the glob visited first — BTreeGraphStorage's body, which holds no vector
# code, against a row whose note says node creation drives vector index
# maintenance. Every matching file is now collected before a verdict: more
# than one match is a refusal naming each file, never a silent pick.
UNRESOLVED=0
: > "$TMP/sites"
while IFS=$'\t' read -r id kind pkg sym floor rest; do
  case "$id" in ''|'#'*) continue ;; esac
  [ -z "${sym:-}" ] && continue

  recv=""
  method="$sym"
  case "$sym" in
    *.*) recv="${sym%%.*}"; method="${sym#*.}" ;;
  esac

  matches=()
  declare -A match_start=() match_end=()
  for f in "$ROOT/$pkg"/*.go; do
    case "$f" in *_test.go) continue ;; esac
    [ -e "$f" ] || continue
    if [ -n "$recv" ]; then
      # Anchored on the receiver type: "func (<name> *Recv) Method(" or the
      # value-receiver form without "*". This is what keeps "GraphStorage"
      # from also matching "BTreeGraphStorage" — the literal must follow
      # directly after the optional "*", so a longer type name that merely
      # ends in the same substring cannot satisfy it.
      start="$(awk -v r="$recv" -v m="$method" \
        '$0 ~ "^func \\([a-zA-Z_][a-zA-Z0-9_]*[ \t]+\\*?" r "\\)[ \t]+" m "\\(" { print NR; exit }' "$f")"
    else
      start="$(awk -v s="$method" '$0 ~ "^func .*[ .*)]" s "\\(" || $0 ~ "^func " s "\\(" { print NR; exit }' "$f")"
    fi
    [ -z "$start" ] && continue
    end="$(awk -v st="$start" 'NR>=st && $0=="}" { print NR; exit }' "$f")"
    [ -z "$end" ] && continue
    matches+=("$f")
    match_start["$f"]="$start"
    match_end["$f"]="$end"
  done

  if [ "${#matches[@]}" -gt 1 ]; then
    echo "dccc: $id names $pkg.$sym, which is ambiguous — declared in:" >&2
    for f in "${matches[@]}"; do
      echo "dccc:   $f" >&2
    done
    echo "dccc: qualify the registry row as Receiver.Method to pick one" >&2
    UNRESOLVED=1
    continue
  fi

  if [ "${#matches[@]}" -eq 0 ]; then
    echo "dccc: $id names $pkg.$sym, which no file in that package defines" >&2
    UNRESOLVED=1
    continue
  fi

  found="${matches[0]}"
  printf '%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n' "$id" "$kind" "$pkg" "$sym" "${match_start[$found]}" "${match_end[$found]}" "$(basename "$found")" "${floor:-0}" >> "$TMP/sites"
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
awk -v profile="$PROFILE" -v update="$UPDATE" '
BEGIN { FS="\t" }
{
  id[NR]=$1; kind[NR]=$2; pkg[NR]=$3; sym[NR]=$4; lo[NR]=$5; hi[NR]=$6; base[NR]=$7
  flo[NR]=$8 + 0; n=NR
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

  # --update emits the measured counts for the registry rewrite and nothing else.
  if (update == 1) {
    for (i = 1; i <= n; i++) printf "%s\t%s\t%d\n", pkg[i], sym[i], total[i] + 0
    exit 0
  }

  print "  covered/total  coupling"
  gt = 0; gc = 0; empty = 0; unmatched = 0; shrunk = 0
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
    flag = ""
    if (flo[i] > 0 && t < flo[i]) {
      flag = sprintf("   <- HOLLOWED: %d statements, floor %d", t, flo[i])
      shrunk++
    }
    printf "  %4d/%-5d %s  %-4s %s %s.%s%s\n", cv, t, pct, id[i], kind[i], pkg[i], sym[i], flag
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
  if (shrunk > 0) {
    printf "\n  %d coupling site(s) hold fewer statements than their recorded floor.\n", shrunk
    printf "  The symbol still resolves, so the percentage above is real — it is just\n"
    printf "  no longer about the coupling. Something was extracted out from under it.\n"
    printf "  Point the registry at where the boundary went, or re-record the floor\n"
    printf "  with make dccc-update if the coupling genuinely shrank.\n"
    exit 3
  }
  if (empty > 0) {
    printf "\n  %d coupling site(s) have no covered statement at all.\n", empty
    exit 1
  }
}
' "$TMP/sites"
