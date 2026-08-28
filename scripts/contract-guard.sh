#!/usr/bin/env bash
#
# Keep the consumer-contract registry and the tests that enforce it in step,
# and make any change to an enforcing test visible in the diff.
#
# docs/CONSUMER_CONTRACTS.md is the registry. Each row names an invariant, the
# downstream consumer that depends on it, and the test that guards it. The
# growth rule says a contract test must fail against the pre-fix code. Nothing
# checked that the registry and the tests still describe each other, and nothing
# made a weakened assertion visible.
#
# That matters more with an agent in the loop than without one. An agent that
# cannot make a test pass can make it pass by editing the test, and the edit
# reads like any other diff. This script does not prevent that. It makes it
# loud: the digest of every guarding test is checked in, so weakening one
# changes a tracked file and the change has to be explained.
#
# Usage:
#   scripts/contract-guard.sh [--root DIR] [--update]
#
#     --root DIR   check this tree instead of the working directory
#     --update     rewrite the lock file from the current tests
#
# Exit codes:
#   0  the registry, the tags and the lock file agree
#   1  they do not agree
#   2  the check could not run
#
# Exit 2 matters as much as exit 1. A registry that cannot be found, or a tree
# with no contract tags at all, means the check read nothing — and a check that
# reads nothing must say so rather than pass.

set -uo pipefail

# Byte order, not dictionary order. The lock file is sorted, and sort's
# collation follows the locale: under a UTF-8 locale punctuation is weighted
# below letters, so "CC10-unique" sorts before "CC1-rest", while under C the
# hyphen (0x2D) is below "0" (0x30) and the order reverses. The digests were
# identical and the gate still failed, because a lock file written on a
# developer machine did not match the one CI computed from the same tests.
# A gate that depends on the environment is not a gate.
export LC_ALL=C

ROOT="."
UPDATE=0
while [ $# -gt 0 ]; do
  case "$1" in
    --root) ROOT="${2:-}"; shift 2 ;;
    --update) UPDATE=1; shift ;;
    -h|--help) sed -n '2,32p' "$0" | sed 's|^# \{0,1\}||'; exit 0 ;;
    *) echo "contract-guard: unknown argument '$1'" >&2; exit 2 ;;
  esac
done

REGISTRY="$ROOT/docs/CONSUMER_CONTRACTS.md"
LOCK="$ROOT/docs/consumer-contracts.lock"
SRC="$ROOT/pkg"

if [ ! -s "$REGISTRY" ]; then
  echo "contract-guard: '$REGISTRY' is missing or empty — refusing to report a pass" >&2
  exit 2
fi
if [ ! -d "$SRC" ]; then
  echo "contract-guard: '$SRC' is not a directory — refusing to report a pass" >&2
  exit 2
fi

sha() {
  if command -v sha256sum >/dev/null 2>&1; then sha256sum | cut -d' ' -f1
  else shasum -a 256 | cut -d' ' -f1; fi
}

# --- the registry: id -> guarding test names ---------------------------------
# A row is: | <id> | <invariant> | <consumers> | <guards> | <origin> |
# The guards column names each test in backticks, next to its package.
REG_IDS="$(awk -F'|' '/^\| *CC[0-9]+-/ { gsub(/^ +| +$/, "", $2); print $2 }' "$REGISTRY" | sort -u)"

if [ -z "$REG_IDS" ]; then
  echo "contract-guard: no contract rows in '$REGISTRY' — refusing to report a pass" >&2
  exit 2
fi

# --- the tags in the tests ---------------------------------------------------
TAG_LINES="$(grep -rn "CONSUMER CONTRACT:" "$SRC" --include='*.go' 2>/dev/null)"

# No tags at all has two very different causes, and they need different exits.
# If there are no test files either, the check is looking at the wrong tree or
# the search itself is broken, and it has read nothing: that is exit 2. If test
# files are there and no tag is, the tags were removed, and every registry row
# is unenforced: that is a violation, and it is reported per row below.
if [ -z "$TAG_LINES" ]; then
  if [ -z "$(find "$SRC" -name '*_test.go' -print -quit 2>/dev/null)" ]; then
    echo "contract-guard: no CONSUMER CONTRACT tags and no test files under '$SRC'." >&2
    echo "contract-guard: the search read nothing, so it cannot report a pass." >&2
    exit 2
  fi
fi

FAIL=0
report() { echo "contract-guard: $*" >&2; FAIL=1; }

# 1. Every tag carries a registry id. A contract comment with no id cannot be
#    traced to a consumer, and it is invisible to every other check here.
while IFS= read -r line; do
  [ -z "$line" ] && continue
  if ! printf '%s' "$line" | grep -qE 'CONSUMER CONTRACT: *CC[0-9]+-[a-z0-9-]+'; then
    loc="${line%%:*}"; rest="${line#*:}"; lno="${rest%%:*}"
    report "$loc:$lno carries a CONSUMER CONTRACT comment with no CC id, so it is in no registry row"
  fi
done <<< "$TAG_LINES"

TAG_IDS="$(printf '%s\n' "$TAG_LINES" | grep -oE 'CC[0-9]+-[a-z0-9-]+' | sort -u)"

# 2. Every registry row is enforced by at least one tagged test.
for id in $REG_IDS; do
  if ! printf '%s\n' "$TAG_IDS" | grep -qx "$id"; then
    report "$id is in the registry but no test is tagged with it"
  fi
done

# 3. Every tagged test is in the registry.
for id in $TAG_IDS; do
  if ! printf '%s\n' "$REG_IDS" | grep -qx "$id"; then
    report "$id is tagged in a test but has no row in the registry"
  fi
done

# --- 4. Every named guarding test exists, and its body is pinned --------------
NEW_LOCK="$(mktemp)"
trap 'rm -f "$NEW_LOCK"' EXIT

# Emit "<id> <pkg> <TestName>" for each guard the registry names.
GUARDS="$(awk -F'|' '
  /^\| *CC[0-9]+-/ {
    id = $2; gsub(/^ +| +$/, "", id)
    n = split($5, parts, "`")
    pkg = ""
    for (i = 2; i <= n; i += 2) {
      tok = parts[i]
      if (tok ~ /^pkg\//) { pkg = tok }
      else if (tok ~ /^Test/) { print id " " (pkg == "" ? "?" : pkg) " " tok }
    }
  }' "$REGISTRY")"

if [ -z "$GUARDS" ]; then
  echo "contract-guard: no guarding test names in '$REGISTRY' — refusing to report a pass" >&2
  exit 2
fi

while read -r id pkg fn; do
  [ -z "${fn:-}" ] && continue
  dir="$ROOT/$pkg"
  if [ ! -d "$dir" ]; then
    report "$id names package '$pkg', which does not exist"
    continue
  fi
  # gofmt puts the closing brace of a top-level func at column 0, so the body
  # runs from "func <name>(" to the next line that is exactly "}".
  body="$(awk -v fn="$fn" '
    $0 ~ "^func " fn "\\(" { inside = 1 }
    inside { print }
    inside && $0 == "}" { exit }
  ' "$dir"/*_test.go 2>/dev/null)"
  if [ -z "$body" ]; then
    report "$id names $pkg.$fn, which no test file in that package defines"
    continue
  fi
  printf '%s %s %s %s\n' "$id" "$pkg" "$fn" "$(printf '%s' "$body" | sha)" >> "$NEW_LOCK"
done <<< "$GUARDS"

sort -o "$NEW_LOCK" "$NEW_LOCK"

if [ "$UPDATE" = "1" ]; then
  { echo "# Digests of the tests that enforce docs/CONSUMER_CONTRACTS.md."
    echo "# Regenerate with: make contract-guard-update"
    echo "#"
    echo "# A change here means a contract test changed. That is allowed, and it must"
    echo "# be explained in the pull request that makes it. The point is that it cannot"
    echo "# happen quietly."
    cat "$NEW_LOCK"
  } > "$LOCK"
  echo "contract-guard: wrote $(wc -l < "$NEW_LOCK" | tr -d ' ') digests to '$LOCK'"
  exit $FAIL
fi

if [ ! -s "$LOCK" ]; then
  report "'$LOCK' is missing. Create it with: make contract-guard-update"
elif ! diff -u <(grep -v '^#' "$LOCK" | grep -v '^$') "$NEW_LOCK" > /tmp/contract-guard-diff.$$ 2>&1; then
  echo "contract-guard: a contract test changed since the lock file was written." >&2
  echo "contract-guard: this is allowed. Run 'make contract-guard-update', and say in the" >&2
  echo "contract-guard: pull request body what changed and why the contract still holds." >&2
  sed 's/^/contract-guard:   /' /tmp/contract-guard-diff.$$ >&2
  rm -f /tmp/contract-guard-diff.$$
  FAIL=1
else
  rm -f /tmp/contract-guard-diff.$$
fi

if [ "$FAIL" = "0" ]; then
  echo "contract-guard: OK — $(printf '%s\n' "$REG_IDS" | wc -l | tr -d ' ') contracts, $(wc -l < "$NEW_LOCK" | tr -d ' ') guarding tests, all pinned"
fi
exit $FAIL
