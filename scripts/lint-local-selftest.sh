#!/usr/bin/env bash
#
# Prove that lint-local.sh can report a finding.
#
# The linter it wraps is one that fails on this machine, and a wrapper around a
# broken tool reports "0 issues" exactly as a working one does. So this builds
# a small module with known violations, runs the wrapper against it, and checks
# that each violation comes back.
#
# Usage: scripts/lint-local-selftest.sh
# Exit: 0 every case behaved, 1 a case did not.

set -uo pipefail

LINT="$(cd "$(dirname "$0")" && pwd)/lint-local.sh"
FAILURES=0
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

fixture() {
  local d="$1"
  mkdir -p "$d"
  cat > "$d/go.mod" <<'EOF'
module lintfixture

go 1.24
EOF
}

# expect NAME EXPECTED_EXIT DIR [PATTERN]
expect() {
  local name="$1" want="$2" dir="$3" pattern="${4:-}"
  local out status
  out="$(cd "$dir" && bash "$LINT" ./... 2>&1)"
  status=$?
  if [ "$status" != "$want" ]; then
    echo "FAIL  $name: exit $status, wanted $want"
    FAILURES=$((FAILURES + 1))
    return
  fi
  if [ -n "$pattern" ] && ! printf '%s' "$out" | grep -q "$pattern"; then
    echo "FAIL  $name: exit $status as wanted, but the output does not mention '$pattern'"
    FAILURES=$((FAILURES + 1))
    return
  fi
  echo "ok    $name (exit $status)"
}

# 0. A clean module passes. Without this every case below could be passing for
#    the wrong reason.
fixture "$WORK/clean"
cat > "$WORK/clean/ok.go" <<'EOF'
package lintfixture

// Receive does nothing, correctly and with the word spelled right.
func Receive() int { return 1 }
EOF
expect "a clean module passes" 0 "$WORK/clean"

# 1. errcheck, and specifically the blank assignment. This is the exact
#    construct that sent PR #494 to CI to find out: .golangci.yml sets
#    check-blank, so `_ = f()` is a finding.
#
#    The function has to be one of this module's own. os.Remove is NOT
#    reported, because the std-error-handling exclusion preset covers the
#    standard library — two earlier versions of this case used it and reported
#    nothing while looking correct. The call CI rejected was on vfs.FileSystem,
#    a project interface, which no preset covers.
fixture "$WORK/errcheck"
cat > "$WORK/errcheck/bad.go" <<'EOF'
package lintfixture

// remove stands in for a project interface method. The standard library is
// excluded from errcheck by preset; this is not.
func remove(name string) error { return nil }

// Unchecked assigns an error to the blank identifier, which check-blank
// reports.
func Unchecked() {
	_ = remove("/tmp/lint-local-selftest")
}
EOF
expect "an unchecked error is reported" 1 "$WORK/errcheck" "errcheck"

# 2. misspell, which needs no type information and so covers the other half of
#    the linter set.
fixture "$WORK/misspell"
cat > "$WORK/misspell/bad.go" <<'EOF'
package lintfixture

// Recieve is spelled wrongly on purpose.
func Recieve() int { return 1 }
EOF
expect "a misspelling is reported" 1 "$WORK/misspell" "misspell"

# 3. The wrapper must refuse rather than pass when it cannot find its config.
mkdir -p "$WORK/noconfig/scripts"
cp "$LINT" "$WORK/noconfig/scripts/lint-local.sh"
fixture "$WORK/noconfig"
out="$(cd "$WORK/noconfig" && bash scripts/lint-local.sh ./... 2>&1)"
if [ $? = 2 ] && printf '%s' "$out" | grep -q "refusing to report a pass"; then
  echo "ok    a missing config refuses (exit 2)"
else
  echo "FAIL  a missing config did not refuse"
  FAILURES=$((FAILURES + 1))
fi

# 4. A package that does not load is not a finding about the code. The
#    analysers never ran on it, so reporting "the lint failed" is as wrong as
#    reporting a pass. This is exit 2, the same as any other cannot-run.
fixture "$WORK/typecheck"
cat > "$WORK/typecheck/bad.go" <<'EOF'
package lintfixture

// Broken refers to a symbol that does not exist, so the package cannot be
// loaded and no analyser can say anything about it.
func Broken() int { return undefinedSymbol }
EOF
expect "a package that cannot load refuses" 2 "$WORK/typecheck" "did not run"

echo
if [ "$FAILURES" = "0" ]; then
  echo "lint-local-selftest: all 5 cases behaved"
  exit 0
fi
echo "lint-local-selftest: $FAILURES case(s) did not behave"
exit 1
