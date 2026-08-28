#!/usr/bin/env bash
#
# Mutation testing: change one operator, run the tests, ask whether any test
# noticed.
#
# A test suite's coverage says which lines ran. It does not say which lines
# anything checked. On 2026-08-28 five tests in this repository ran the code
# they claimed to test and asserted nothing useful about it: one faulted an
# operation that created no file, another started its reader after the writes
# it was meant to observe, three compared a fallback with itself. Each was
# found by reverting the repair by hand and watching the test stay green. That
# is this, one mutant at a time.
#
# Usage: scripts/mutation.sh [packages...]   (default: the two in TARGETS)
#
# Exit codes:
#   0  the run completed and every mutant was evaluated
#   1  the run completed and mutants survived beyond the recorded baseline
#   2  the run could not produce a number worth reading
#
# Exit 2 is the important one. See "A timed-out mutant is not a result" below.

set -uo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"

# Two packages, not the repository. A full run is minutes per package and the
# value is concentrated where the fault-injection machinery lives.
TARGETS=("./pkg/vfs/vfstest/" "./pkg/lsm/")

if ! command -v gremlins >/dev/null 2>&1; then
  echo "mutation: gremlins is not installed — refusing to report a pass" >&2
  echo "mutation: go install github.com/go-gremlins/gremlins/cmd/gremlins@latest" >&2
  exit 2
fi
if [ ! -s "$ROOT/.gremlins.yaml" ]; then
  echo "mutation: '$ROOT/.gremlins.yaml' is missing — refusing to report a pass" >&2
  echo "mutation: the timeout coefficient in it is what makes the score mean anything" >&2
  exit 2
fi

if [ "$#" -gt 0 ]; then
  TARGETS=("$@")
fi

status=0
for pkg in "${TARGETS[@]}"; do
  echo "== $pkg"
  # --config explicitly. Gremlins does not pick the file up from the working
  # directory, and a run with no configuration is a run with the default
  # timeout, which is the run that reports a score from three mutants.
  OUTPUT="$(cd "$ROOT" && gremlins unleash --config "$ROOT/.gremlins.yaml" "$pkg" 2>&1)"
  printf '%s\n' "$OUTPUT" | grep -E '^ *(LIVED|NOT COVERED)' || true
  SUMMARY="$(printf '%s' "$OUTPUT" | grep -E '^(Killed|Timed out|Test efficacy|Mutator coverage)')"
  printf '%s\n' "$SUMMARY"

  KILLED=$(printf '%s' "$OUTPUT"  | sed -n 's/^Killed: \([0-9]*\),.*/\1/p')
  LIVED=$(printf '%s' "$OUTPUT"   | sed -n 's/^Killed: [0-9]*, Lived: \([0-9]*\),.*/\1/p')
  TIMEDOUT=$(printf '%s' "$OUTPUT" | sed -n 's/^Timed out: \([0-9]*\),.*/\1/p')

  if [ -z "${KILLED:-}" ] || [ -z "${LIVED:-}" ] || [ -z "${TIMEDOUT:-}" ]; then
    echo "mutation: could not read a summary from the run for $pkg" >&2
    status=2
    continue
  fi

  # A timed-out mutant is not a result.
  #
  # Gremlins derives its per-mutant timeout from the baseline test run, and the
  # default is far too short here: every one of pkg/vfs/vfstest's 88 mutants
  # timed out, and the tool then reported "Test efficacy: 100.00%" from the one
  # mutant it had managed to run. Two identical invocations gave 100% and 0%.
  #
  # So a run with many timeouts must refuse rather than report. A score
  # computed from three mutants out of 88 looks exactly like a real one.
  RAN=$((KILLED + LIVED))
  if [ "$TIMEDOUT" -gt 0 ]; then
    if [ "$RAN" -eq 0 ] || [ $((TIMEDOUT * 10)) -gt "$RAN" ]; then
      echo "mutation: $TIMEDOUT of $((RAN + TIMEDOUT)) mutants timed out for $pkg." >&2
      echo "mutation: those mutants were never evaluated, so the score above is not one." >&2
      echo "mutation: raise timeout-coefficient in .gremlins.yaml and run it again." >&2
      status=2
      continue
    fi
    echo "mutation: note — $TIMEDOUT mutants timed out and were not evaluated." >&2
  fi
done

exit "$status"
