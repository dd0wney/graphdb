#!/usr/bin/env bash
# Prove scripts/snare-scan.sh can report each of its outcomes, not only a pass:
# a finding (1), a clean tree (0), a finding only in a _test.go file (0), and a
# scan that did not run (2). Needs a snare binary; without one this selftest
# cannot run and exits 2, which is not a pass.
set -euo pipefail

here=$(cd "$(dirname "$0")" && pwd)
repo=$(cd "$here/.." && pwd)
scan="$here/snare-scan.sh"
SNARE="${SNARE:-snare}"

if ! command -v "$SNARE" >/dev/null 2>&1; then
  echo "snare-scan-selftest: snare binary not found (set SNARE=/path/to/snare); the selftest did not run" >&2
  exit 2
fi

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

fixture() { # fixture <dir> <file> <body of f>
  mkdir -p "$work/$1"
  cat >"$work/$1/$2" <<EOF
package fixture

import "sync"

var mu sync.Mutex

func g() {}

func f() {
$3
}
EOF
}

fixture finding a.go $'\tmu.Lock()\n\tg()\n\tmu.Unlock()'
fixture clean a.go $'\tmu.Lock()\n\tdefer mu.Unlock()\n\tg()'
fixture nocall a.go $'\tmu.Lock()\n\tx := 1\n\t_ = x\n\tmu.Unlock()'
fixture testonly a_test.go $'\tmu.Lock()\n\tg()\n\tmu.Unlock()'

fail=0
expect() { # expect <want-exit> <label> <command...>
  local want=$1 label=$2
  shift 2
  set +e
  "$@" >/dev/null 2>&1
  local got=$?
  set -e
  if [ "$got" -eq "$want" ]; then
    echo "ok   $label (exit $got)"
  else
    echo "FAIL $label: exit $got, want $want"
    fail=1
  fi
}

cd "$repo"
expect 1 "a call between Lock and Unlock is a finding" env SNARE="$SNARE" "$scan" "$work/finding"
expect 0 "a deferred unlock is clean" env SNARE="$SNARE" "$scan" "$work/clean"
expect 0 "a section with no call is clean" env SNARE="$SNARE" "$scan" "$work/nocall"
expect 0 "a finding only in a _test.go file is left out" env SNARE="$SNARE" "$scan" "$work/testonly"
expect 2 "a missing snare binary is not a pass" env SNARE="$work/no-such-snare" "$scan" "$work/finding"
expect 2 "a snare runtime error is not a pass" env SNARE="$SNARE" SNARE_RULES="$work/no-such-rules" "$scan" "$work/finding"

exit "$fail"
