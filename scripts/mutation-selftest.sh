#!/usr/bin/env bash
#
# Prove that the mutation run reports both outcomes.
#
# A mutation score is only worth reading if the tool can say "killed" and can
# say "lived". A run where every mutant times out reports neither, and this
# repository has already seen that: gremlins' default timeout produced
# "Test efficacy: 100.00%" from one evaluated mutant out of 88, and two
# identical invocations gave 100% and 0%.
#
# So this builds two fixtures. In one, a test constrains the function and every
# mutant must die. In the other, the test calls the function and checks nothing,
# and mutants must live. If both come back as expected the tool is working.
#
# Usage: scripts/mutation-selftest.sh
# Exit: 0 both outcomes were observed, 1 they were not.

set -uo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
FAILURES=0
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

if ! command -v gremlins >/dev/null 2>&1; then
  echo "mutation-selftest: gremlins is not installed" >&2
  exit 1
fi

# Both fixtures share the same source. Only the test differs, which is the
# whole point: the source is identical, so any difference in the score is a
# difference in what the tests check.
make_fixture() {
  local d="$1" testbody="$2"
  mkdir -p "$d"
  cat > "$d/go.mod" <<'EOF'
module mutationfixture

go 1.24
EOF
  cat > "$d/calc.go" <<'EOF'
package mutationfixture

// AtLeast reports whether n reaches the threshold. The comparison is what the
// mutation operators change: >= becomes >, and the condition gets negated.
func AtLeast(n, threshold int) bool {
	if n >= threshold {
		return true
	}
	return false
}
EOF
  printf '%s' "$testbody" > "$d/calc_test.go"
  cp "$ROOT/.gremlins.yaml" "$d/.gremlins.yaml"
}

# A test that pins the boundary on both sides. Every mutant must die.
make_fixture "$WORK/constrained" 'package mutationfixture

import "testing"

func TestAtLeast(t *testing.T) {
	if !AtLeast(5, 5) {
		t.Error("5 is at least 5")
	}
	if AtLeast(4, 5) {
		t.Error("4 is not at least 5")
	}
	if !AtLeast(6, 5) {
		t.Error("6 is at least 5")
	}
}
'

# A test that runs the function and checks nothing about it. This is what the
# five decoration tests of 2026-08-28 looked like from the outside: the line is
# covered, and no mutant of it is noticed.
make_fixture "$WORK/unconstrained" 'package mutationfixture

import "testing"

func TestAtLeast(t *testing.T) {
	AtLeast(5, 5)
	AtLeast(4, 5)
	AtLeast(6, 5)
}
'

# "." and not "./...". Gremlins reports "No results to report" for ./... in a
# single-package module, and a run that finds no mutants looks from the outside
# exactly like a suite that killed them all.
score() {
  (cd "$1" && gremlins unleash . 2>&1)
}

CONSTRAINED="$(score "$WORK/constrained")"
UNCONSTRAINED="$(score "$WORK/unconstrained")"

c_lived=$(printf '%s' "$CONSTRAINED"   | sed -n 's/^Killed: [0-9]*, Lived: \([0-9]*\),.*/\1/p')
c_killed=$(printf '%s' "$CONSTRAINED"  | sed -n 's/^Killed: \([0-9]*\),.*/\1/p')
u_lived=$(printf '%s' "$UNCONSTRAINED" | sed -n 's/^Killed: [0-9]*, Lived: \([0-9]*\),.*/\1/p')

if [ "${c_killed:-0}" -gt 0 ]; then
  echo "ok    a constrained test kills mutants ($c_killed killed)"
else
  echo "FAIL  a constrained test killed nothing, so the tool cannot report a kill"
  FAILURES=$((FAILURES + 1))
fi

if [ "${c_lived:-1}" -eq 0 ]; then
  echo "ok    a constrained test leaves no survivor"
else
  echo "FAIL  a constrained test left $c_lived survivor(s), so the fixture does not pin the boundary"
  FAILURES=$((FAILURES + 1))
fi

if [ "${u_lived:-0}" -gt 0 ]; then
  echo "ok    a test that checks nothing leaves survivors ($u_lived lived)"
else
  echo "FAIL  a test that checks nothing left no survivor, so the tool cannot report a survivor"
  FAILURES=$((FAILURES + 1))
fi

echo
if [ "$FAILURES" = "0" ]; then
  echo "mutation-selftest: the tool reports both outcomes"
  exit 0
fi
echo "mutation-selftest: $FAILURES check(s) did not behave"
exit 1
