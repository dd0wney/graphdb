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

# --- Working-tree guard (scripts/mutation.sh) --------------------------------
#
# gremlins mutates *.go files in place. The guard added to mutation.sh must
# refuse to run when a tracked *.go file is already dirty and name the
# offending path, stay out of the way when only a non-Go file is dirty, and
# its restore trap must actually put back a file gremlins left mutated. None
# of these three need the real gremlins binary — a stub in front of PATH
# stands in, so this block runs whether or not gremlins is installed.

guard_commit() {
  local d="$1"
  git -C "$d" init -q
  git -C "$d" config user.email "mutation-selftest@example.com"
  git -C "$d" config user.name "mutation-selftest"
  git -C "$d" add -A
  git -C "$d" commit -qm "guard fixture" >/dev/null
}

# 1. A dirty tracked .go file must refuse, exit 2, and name the path. No
#    gremlins or .gremlins.yaml is needed here — the guard runs before either
#    is checked.
D_DIRTY="$WORK/guard-dirty-go"
mkdir -p "$D_DIRTY/scripts" "$D_DIRTY/pkg"
cp "$ROOT/scripts/mutation.sh" "$D_DIRTY/scripts/mutation.sh"
printf 'package guardfixture\n' > "$D_DIRTY/pkg/foo.go"
echo "unrelated" > "$D_DIRTY/README.md"
guard_commit "$D_DIRTY"
echo "// dirty" >> "$D_DIRTY/pkg/foo.go"

DIRTY_OUT="$(bash "$D_DIRTY/scripts/mutation.sh" 2>&1)"
DIRTY_STATUS=$?
if [ "$DIRTY_STATUS" -eq 2 ] && printf '%s' "$DIRTY_OUT" | grep -q 'pkg/foo.go'; then
  echo "ok    a dirty tracked .go file refuses and names the path (exit $DIRTY_STATUS)"
else
  echo "FAIL  a dirty tracked .go file: exit $DIRTY_STATUS, wanted 2 naming pkg/foo.go"
  printf '%s\n' "$DIRTY_OUT" | sed 's/^/      /'
  FAILURES=$((FAILURES + 1))
fi

# 2. A dirty NON-Go file must NOT refuse. This is the case almost nobody
#    writes: proof the guard does not fire where it must not. A sibling
#    project's first guard keyed on the whole tree and refused its own
#    selftest's fixture workflow for exactly this reason. A stub `gremlins`
#    stands in so this case needs neither the real tool nor a buildable
#    package — only the guard's own scoping is under test.
D_NONGO="$WORK/guard-dirty-nongo"
mkdir -p "$D_NONGO/scripts" "$D_NONGO/bin"
cp "$ROOT/scripts/mutation.sh" "$D_NONGO/scripts/mutation.sh"
cp "$ROOT/.gremlins.yaml" "$D_NONGO/.gremlins.yaml"
printf 'package guardfixture\n' > "$D_NONGO/tracked.go"
echo "unrelated" > "$D_NONGO/README.md"
guard_commit "$D_NONGO"
cat > "$D_NONGO/bin/gremlins" <<'STUB'
#!/usr/bin/env bash
cat <<'SUMMARY'
Killed: 1, Lived: 0, Not covered: 0
Timed out: 0, Not viable: 0, Skipped: 0
Test efficacy: 100.00%
Mutator coverage: 100.00%
SUMMARY
exit 0
STUB
chmod +x "$D_NONGO/bin/gremlins"
echo "modified" >> "$D_NONGO/README.md"

NONGO_OUT="$(PATH="$D_NONGO/bin:$PATH" bash "$D_NONGO/scripts/mutation.sh" . 2>&1)"
NONGO_STATUS=$?
if [ "$NONGO_STATUS" -eq 0 ] && ! printf '%s' "$NONGO_OUT" | grep -qi 'dirty'; then
  echo "ok    a dirty non-Go file does not trigger the guard (exit $NONGO_STATUS)"
else
  echo "FAIL  a dirty non-Go file changed the outcome: exit $NONGO_STATUS"
  printf '%s\n' "$NONGO_OUT" | sed 's/^/      /'
  FAILURES=$((FAILURES + 1))
fi

# 3. The restore trap must actually restore. A stub `gremlins` mutates the
#    tracked .go file and sleeps, standing in for the window during a real run
#    where the file sits mutated on disk. The harness waits for that mutation
#    to land, sends SIGTERM, and checks the file matches the committed content
#    afterward — proof the trap fired, not proof the process merely exited.
D_RESTORE="$WORK/guard-restore"
mkdir -p "$D_RESTORE/scripts" "$D_RESTORE/bin"
cp "$ROOT/scripts/mutation.sh" "$D_RESTORE/scripts/mutation.sh"
cp "$ROOT/.gremlins.yaml" "$D_RESTORE/.gremlins.yaml"
printf 'package guardfixture\n' > "$D_RESTORE/calc.go"
guard_commit "$D_RESTORE"
cat > "$D_RESTORE/bin/gremlins" <<STUB
#!/usr/bin/env bash
echo "// mutated by stub" >> "$D_RESTORE/calc.go"
sleep 10
cat <<'SUMMARY'
Killed: 1, Lived: 0, Not covered: 0
Timed out: 0, Not viable: 0, Skipped: 0
Test efficacy: 100.00%
Mutator coverage: 100.00%
SUMMARY
exit 0
STUB
chmod +x "$D_RESTORE/bin/gremlins"

ORIGINAL_CALC="$(cat "$D_RESTORE/calc.go")"
PATH="$D_RESTORE/bin:$PATH" bash "$D_RESTORE/scripts/mutation.sh" . >"$D_RESTORE/stdout.log" 2>"$D_RESTORE/stderr.log" &
RPID=$!

RWAITED=0
while [ "$RWAITED" -lt 50 ]; do
  git -C "$D_RESTORE" diff --quiet -- '*.go' || break
  sleep 0.1
  RWAITED=$((RWAITED + 1))
done

if git -C "$D_RESTORE" diff --quiet -- '*.go'; then
  echo "FAIL  restore trap: the stub never mutated calc.go, so this case tested nothing"
  FAILURES=$((FAILURES + 1))
  kill -9 "$RPID" >/dev/null 2>&1 || true
  wait "$RPID" 2>/dev/null || true
else
  kill -TERM "$RPID"
  wait "$RPID" 2>/dev/null
  RSTATUS=$?
  if [ "$RSTATUS" -eq 143 ] && [ "$(cat "$D_RESTORE/calc.go")" = "$ORIGINAL_CALC" ]; then
    echo "ok    the restore trap put calc.go back after SIGTERM (exit $RSTATUS)"
  else
    echo "FAIL  calc.go was not restored after SIGTERM (exit $RSTATUS)"
    sed 's/^/      /' "$D_RESTORE/stderr.log"
    FAILURES=$((FAILURES + 1))
  fi
fi

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
