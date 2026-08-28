#!/usr/bin/env bash
#
# Prove that the per-package floor gate can report a failure.
#
# A gate that passes everything and a gate that works look identical from the
# outside. This builds a synthetic profile for each outcome and checks the exit
# code, so "every package is above its floor" means something.
#
# Usage: scripts/coverage-floors-selftest.sh
# Exit: 0 every case behaved, 1 a case did not.

set -uo pipefail

GATE="$(cd "$(dirname "$0")" && pwd)/coverage-floors.sh"
FAILURES=0
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

# fixture DIR COVERED_COUNT builds a profile for one package with four
# statements, COVERED_COUNT of them executed, and a floor of 60%.
fixture() {
  local d="$1" covered="$2"
  mkdir -p "$d/docs/internals/design"
  printf '# Columns: package\tfloor\tmeasured-in-CI\npkg/fixture\t60.0\t75.0\n' \
    > "$d/docs/internals/design/coverage-floors.tsv"
  {
    echo "mode: set"
    for i in 1 2 3 4; do
      if [ "$i" -le "$covered" ]; then c=1; else c=0; fi
      echo "example.com/m/pkg/fixture/f.go:$i.1,$i.9 1 $c"
    done
  } > "$d/profile.out"
}

# expect NAME EXPECTED_EXIT DIR [PATTERN]
expect() {
  local name="$1" want="$2" dir="$3" pattern="${4:-}"
  local out status
  out="$(bash "$GATE" --root "$dir" "$dir/profile.out" 2>&1)"
  status=$?
  if [ "$status" != "$want" ]; then
    echo "FAIL  $name: exit $status, wanted $want"
    printf '%s\n' "$out" | sed 's/^/        /'
    FAILURES=$((FAILURES + 1))
    return
  fi
  if [ -n "$pattern" ] && ! printf '%s' "$out" | grep -q "$pattern"; then
    echo "FAIL  $name: exit $status as wanted, but the output does not match '$pattern'"
    FAILURES=$((FAILURES + 1))
    return
  fi
  echo "ok    $name (exit $status)"
}

# 0. Above the floor passes. Without this every case below could pass wrongly.
fixture "$WORK/above" 3   # 75%
expect "a package above its floor passes" 0 "$WORK/above" "ok"

# 1. Below the floor fails. This is the whole point of the gate.
fixture "$WORK/below" 2   # 50%, floor is 60
expect "a package below its floor fails" 1 "$WORK/below" "below the 60.0% floor"

# 2. Exactly at the floor passes, so the comparison is not off by one side.
fixture "$WORK/exact" 3
sed -i.bak 's/\t60.0\t/\t75.0\t/' "$WORK/exact/docs/internals/design/coverage-floors.tsv"
expect "a package exactly at its floor passes" 0 "$WORK/exact"

# 3. A package in the floors file and absent from the profile is a refusal.
#    Skipping it would drop a package out of the gate and read as a pass, which
#    is the same failure the coupling measure had.
fixture "$WORK/absent" 3
printf 'pkg/gone\t50.0\t60.0\n' >> "$WORK/absent/docs/internals/design/coverage-floors.tsv"
expect "a package missing from the profile refuses" 2 "$WORK/absent" "drops out of the gate"

# 4. No profile is a refusal, not a pass.
fixture "$WORK/noprofile" 3
rm "$WORK/noprofile/profile.out"
expect "a missing profile refuses" 2 "$WORK/noprofile" "refusing to report a pass"

echo
if [ "$FAILURES" = "0" ]; then
  echo "coverage-floors-selftest: all 5 cases behaved"
  exit 0
fi
echo "coverage-floors-selftest: $FAILURES case(s) did not behave"
exit 1
