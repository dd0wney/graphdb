#!/usr/bin/env bash
#
# Prove that consumer-drive.sh turns a SKIP into a failure under CI, and
# stays a warning without it.
#
# A gate is only worth running if it can report the negative. This points
# consumer-drive.sh at two fixture directories that never satisfy either
# consumer's presence check (no coi-screen/cmd/coi, no
# understand-graphdb/package.json), so every run below drives the SKIP path
# on purpose. It then runs the script once with CI unset and once with
# CI=true, and checks the exit code and the log line for each.
#
# This selftest never builds graphdb and never runs a real consumer: it
# always sets CONSUMER_DRIVE_SKIP_BUILD=1 for the script under test.
#
# Usage: scripts/consumer-drive-selftest.sh
#   CONSUMER_DRIVE_SCRIPT=<path>  run against a different consumer-drive.sh
#                                 instead of the one beside this selftest.
#                                 Used as a negative control: point this at
#                                 a checkout of the script from before the
#                                 CI-skip-policy change and confirm this
#                                 selftest then reports a failure.
# Exit: 0 every case behaved, 1 a case did not.

set -uo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$HERE/.." && pwd)"
SCRIPT="${CONSUMER_DRIVE_SCRIPT:-$HERE/consumer-drive.sh}"
FAILURES=0

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

# Fixture consumer directories exist but never satisfy the presence check
# (coi-screen needs $COI/cmd/coi ; understand-graphdb needs $UG/package.json).
mkdir -p "$WORK/coi-screen" "$WORK/understand-graphdb"

# run CI_VALUE — invokes the script under test and sets OUT / GOT.
run() {
  local ci_value="$1"
  if [ -n "$ci_value" ]; then
    OUT="$(cd "$REPO_ROOT" && CI="$ci_value" CONSUMER_DRIVE_SKIP_BUILD=1 \
      COI_SCREEN_REPO="$WORK/coi-screen" \
      UNDERSTAND_GRAPHDB_REPO="$WORK/understand-graphdb" \
      bash "$SCRIPT" 2>&1)"
  else
    OUT="$(cd "$REPO_ROOT" && env -u CI CONSUMER_DRIVE_SKIP_BUILD=1 \
      COI_SCREEN_REPO="$WORK/coi-screen" \
      UNDERSTAND_GRAPHDB_REPO="$WORK/understand-graphdb" \
      bash "$SCRIPT" 2>&1)"
  fi
  GOT=$?
}

# expect NAME WANT_EXIT CI_VALUE GREP_FOR
expect() {
  local name="$1" want="$2" ci_value="$3" grep_for="$4"
  run "$ci_value"
  local ok=1
  if [ "$GOT" != "$want" ]; then
    ok=0
  fi
  if ! echo "$OUT" | grep -qF "$grep_for"; then
    ok=0
  fi
  if [ "$ok" = 1 ]; then
    echo "ok    $name (exit $GOT)"
  else
    echo "FAIL  $name: exit $GOT, wanted $want (looking for: $grep_for)"
    echo "$OUT" | sed 's/^/      | /'
    FAILURES=$((FAILURES + 1))
  fi
}

# a. A consumer is missing and CI is unset: the run stays a warning.
expect "SKIP with CI unset exits 0" 0 "" "SKIP coi-screen"

# b. A consumer is missing and CI is set: the run fails, loudly.
expect "SKIP with CI=true exits 1" 1 "true" "is a failure under CI"

echo
if [ "$FAILURES" = "0" ]; then
  echo "consumer-drive-selftest: all cases behaved"
  exit 0
fi
echo "consumer-drive-selftest: $FAILURES case(s) did not behave"
exit 1
