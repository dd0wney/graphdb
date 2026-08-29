#!/usr/bin/env bash
#
# Prove that the coupling-coverage measure reports what it claims.
#
# A coverage number is easy to produce and hard to trust. The first working
# version of dccc.sh matched profile blocks by package and line range and not
# by file, so a 20-statement function was reported as 634 statements: every
# block at the same line numbers in every other file of the package counted
# towards it. The number looked like a number.
#
# Usage: scripts/dccc-selftest.sh
# Exit: 0 every case behaved, 1 a case did not.
#
# The five original cases plus the statement-count floor, added in #507,
# which exists because a resolvable symbol is not the same thing as a symbol
# that still contains the coupling.

set -uo pipefail

DCCC="$(cd "$(dirname "$0")" && pwd)/dccc.sh"
FAILURES=0
# Counted, not asserted. This line read "all 6 cases behaved" while seven were
# passing, because the number was a literal. A self-test that miscounts itself
# is the same class of defect as the one it exists to catch.
CASES=0
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

# fixture DIR builds a tree with two files whose functions sit at the SAME line
# numbers. One is covered by the profile and the other is not. That is the
# shape the file-matching defect got wrong.
fixture() {
  local d="$1"
  mkdir -p "$d/docs/internals/design" "$d/pkg/fixture"

  cat > "$d/pkg/fixture/covered.go" <<'EOF'
package fixture

// Covered occupies lines 5 to 9.
func Covered(n int) int {
	if n > 0 {
		return n
	}
	return 0
}
EOF
  cat > "$d/pkg/fixture/uncovered.go" <<'EOF'
package fixture

// Uncovered occupies lines 5 to 9, the same lines as Covered.
func Uncovered(n int) int {
	if n > 0 {
		return n
	}
	return 0
}
EOF

  # A profile in which only covered.go has any executed statement.
  cat > "$d/profile.out" <<'EOF'
mode: set
example.com/m/pkg/fixture/covered.go:5.24,6.13 1 1
example.com/m/pkg/fixture/covered.go:6.13,8.3 1 1
example.com/m/pkg/fixture/covered.go:9.2,9.10 1 1
example.com/m/pkg/fixture/uncovered.go:5.26,6.13 1 0
example.com/m/pkg/fixture/uncovered.go:6.13,8.3 1 0
example.com/m/pkg/fixture/uncovered.go:9.2,9.10 1 0
EOF
}

registry() {
  printf '%s\n' "$2" > "$1/docs/internals/design/couplings.tsv"
}

# expect NAME EXPECTED_EXIT DIR [PATTERN]
expect() {
  local name="$1" want="$2" dir="$3" pattern="${4:-}"
  local out status
  out="$(bash "$DCCC" --root "$dir" "$dir/profile.out" 2>&1)"
  status=$?
  if [ "$status" != "$want" ]; then
    echo "FAIL  $name: exit $status, wanted $want"
    printf '%s\n' "$out" | sed 's/^/        /'
    FAILURES=$((FAILURES + 1))
    return
  fi
  if [ -n "$pattern" ] && ! printf '%s' "$out" | grep -q "$pattern"; then
    echo "FAIL  $name: exit $status as wanted, but the output does not match '$pattern'"
    printf '%s\n' "$out" | sed 's/^/        /'
    FAILURES=$((FAILURES + 1))
    return
  fi
  CASES=$((CASES + 1))
  echo "ok    $name (exit $status)"
}

# 0. A covered coupling reports full coverage. Without this every case below
#    could be passing for the wrong reason.
fixture "$WORK/covered"
registry "$WORK/covered" 'X1	control	pkg/fixture	Covered	0	a covered coupling'
expect "a covered coupling reports coverage" 0 "$WORK/covered" "3/3"

# 1. An uncovered coupling reports zero, and the run fails. A coupling with no
#    covered statement is the finding this measure exists to make.
fixture "$WORK/uncovered"
registry "$WORK/uncovered" 'X2	control	pkg/fixture	Uncovered	0	an uncovered coupling'
expect "an uncovered coupling reports zero and fails" 1 "$WORK/uncovered" "0/3"

# 2. THE REGRESSION. Uncovered sits at the same line numbers as Covered. If the
#    measure matches by package and line range without the file, it reports
#    Covered's statements against Uncovered and calls it 100%.
fixture "$WORK/samelines"
registry "$WORK/samelines" 'X3	control	pkg/fixture	Uncovered	0	same lines as a covered function in another file'
expect "a function at the same lines in another file is not counted" 1 "$WORK/samelines" "0/3"

# 3. A symbol the registry names and the code does not have is a refusal. A
#    measure that silently drops a renamed function reports a number for fewer
#    couplings than it says it covers.
fixture "$WORK/missing"
registry "$WORK/missing" 'X4	control	pkg/fixture	Renamed	0	a symbol that is not there'
expect "a symbol that does not exist refuses" 2 "$WORK/missing" "which no file in that package defines"

# 4. A site that matches nothing in the profile is a refusal, not an "n/a".
#    Every real function has statements, so zero total means the profile does
#    not cover that package or was read while still being written. That
#    happened for real: a half-written profile reported one coupling as 0/0
#    beside thirteen real percentages, and it read as a result.
fixture "$WORK/unmatched"
registry "$WORK/unmatched" 'X6	control	pkg/fixture	Covered	0	a coupling absent from the profile'
grep -v 'covered.go' "$WORK/unmatched/profile.out" > "$WORK/unmatched/p2" && mv "$WORK/unmatched/p2" "$WORK/unmatched/profile.out"
expect "a site absent from the profile refuses" 2 "$WORK/unmatched" "matched no statement"

# 5. A symbol that still resolves but no longer holds the coupling. This is the
#    case ADR 0002 stage 4 walked through undetected: openMmapSnapshot kept its
#    name, kept resolving, and reported 1/1 = 100% after its body moved to
#    openMmapSnapshotWithFS. Coverage went UP because the measure lost its
#    subject, so nothing in the number could reveal it. The floor is the only
#    thing that can.
fixture "$WORK/hollowed"
registry "$WORK/hollowed" 'X1	control	pkg/fixture	Covered	10	a coupling recorded at 10 statements'
expect "a symbol that shrank below its floor refuses" 3 "$WORK/hollowed" "HOLLOWED"

# 5. No profile is a refusal, not a zero.
fixture "$WORK/noprofile"
registry "$WORK/noprofile" 'X5	control	pkg/fixture	Covered	0	a covered coupling'
rm "$WORK/noprofile/profile.out"
out="$(bash "$DCCC" --root "$WORK/noprofile" "$WORK/noprofile/profile.out" 2>&1)"
if [ $? = 2 ] && printf '%s' "$out" | grep -q "refusing to report a number"; then
  CASES=$((CASES + 1))
  echo "ok    a missing profile refuses (exit 2)"
else
  echo "FAIL  a missing profile did not refuse"
  FAILURES=$((FAILURES + 1))
fi

echo
if [ "$FAILURES" = "0" ]; then
  echo "dccc-selftest: all $CASES cases behaved"
  exit 0
fi
echo "dccc-selftest: $FAILURES case(s) did not behave"
exit 1
