#!/usr/bin/env bash
#
# Prove that scripts/lib/netns.sh isolates, and that this selftest can report
# the negative.
#
# Style: scripts/contract-guard-selftest.sh. That script proves contract-guard
# can fail by feeding it a fixture built to be broken and checking the exit
# code. This script does the same for the isolation checks below: the "only
# lo" check also runs once against a deliberately unisolated stand-in and
# must report FAIL there. A check that always passes is not proving anything
# about the real wrapper (CLAUDE.md, "Red-first: a test that has never failed
# is not evidence").
#
# Usage: scripts/netns-selftest.sh
# Exit: 0 every case behaved (including the documented host-unsupported
#       fallback), 1 a case did not.

set -uo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
LIB="$ROOT/scripts/lib/netns.sh"
FAILURES=0

# expect NAME WANT_EXIT CMD... — run CMD, compare its exit status to WANT_EXIT.
expect() {
  local name="$1" want="$2"; shift 2
  "$@" >/dev/null 2>&1
  local got=$?
  if [ "$got" = "$want" ]; then
    echo "ok    $name (exit $got)"
  else
    echo "FAIL  $name: exit $got, wanted $want"
    FAILURES=$((FAILURES + 1))
  fi
}

# shellcheck source=scripts/lib/netns.sh
source "$LIB"

if ! netns_available; then
  echo "SKIP isolation cases: this host cannot make a private network namespace"
  echo "     (unshare/user namespaces unavailable, or this host's Portmaster"
  echo "     situation does not apply); the netns path is unverified here."
  echo
  # The wrapper must still say so honestly rather than silently running the
  # command on the host: in_private_netns must return 125, never 0.
  expect "in_private_netns reports 125 when unsupported" 125 in_private_netns true
  echo
  if [ "$FAILURES" = "0" ]; then
    echo "netns-selftest: fallback path behaved, isolation unverified on this host"
    exit 0
  fi
  echo "netns-selftest: $FAILURES case(s) did not behave"
  exit 1
fi

# broken_in_private_netns is a stand-in that runs CMD directly on the host,
# in no namespace at all. Every isolation check below must FAIL against it,
# or the check is not testing isolation — it would pass just as happily if
# scripts/lib/netns.sh regressed to this.
broken_in_private_netns() { "$@"; }

# only_lo RUNNER checks that, through RUNNER, "ip link show" names exactly
# one interface: lo. ip reads the kernel's live namespace state over netlink,
# unlike /sys/class/net, which reflects the namespace that mounted sysfs and
# so cannot tell two network namespaces apart without a mount-namespace
# remount as well.
only_lo() {
  local runner="$1" ifaces
  ifaces=$("$runner" sh -c 'ip link set lo up >/dev/null 2>&1; ip -o link show' 2>/dev/null \
    | awk -F': ' '{print $2}')
  [ "$ifaces" = "lo" ]
}

# 1. Real wrapper: inside the namespace, only lo exists.
if only_lo in_private_netns; then
  echo "ok    only lo exists inside the namespace"
else
  echo "FAIL  more than lo visible inside the namespace"
  FAILURES=$((FAILURES + 1))
fi

# 2. Negative proof: the same check against the unisolated stand-in must
#    fail, or "only lo" cannot be trusted to catch a real regression. Skipped
#    only if this host itself somehow exposes just lo, since then the check
#    cannot tell the two apart and a pass here would prove nothing.
host_ifaces=$(ip -o link show 2>/dev/null | awk -F': ' '{print $2}')
if [ "$host_ifaces" = "lo" ]; then
  echo "skip  host itself has only lo; the only-lo check cannot prove a negative here"
else
  if only_lo broken_in_private_netns; then
    echo "FAIL  only-lo check passed against the unisolated stand-in"
    FAILURES=$((FAILURES + 1))
  else
    echo "ok    only-lo check fails against the unisolated stand-in, as it must"
  fi
fi

# 3. Real wrapper: an outbound connect to a non-loopback address is refused
#    at once (no route out of the namespace), not left to hang to the
#    connect timeout. 198.51.100.1 is TEST-NET-2 (RFC 5737): reserved,
#    globally unroutable, so a real network cannot answer it either way and
#    the timing difference is entirely about whether a route exists.
start_ns=$(date +%s%N)
in_private_netns curl -s --connect-timeout 3 -o /dev/null http://198.51.100.1/
st=$?
end_ns=$(date +%s%N)
ms=$(( (end_ns - start_ns) / 1000000 ))
if [ "$st" -ne 0 ] && [ "$ms" -lt 1000 ]; then
  echo "ok    outbound connect to a non-loopback address is refused at once (${ms} ms)"
else
  echo "FAIL  outbound connect inside the namespace: exit $st after ${ms} ms, wanted a fast refusal"
  FAILURES=$((FAILURES + 1))
fi

# 4. Loopback listen-and-connect inside the namespace succeeds.
if in_private_netns bash -c '
  python3 -m http.server 18099 --bind 127.0.0.1 >/dev/null 2>&1 &
  pid=$!
  ok=1
  for _ in $(seq 20); do
    if curl -s --connect-timeout 1 -o /dev/null http://127.0.0.1:18099/; then
      ok=0
      break
    fi
    sleep 0.1
  done
  kill "$pid" 2>/dev/null
  exit $ok
'; then
  echo "ok    loopback listen-and-connect succeeds inside the namespace"
else
  echo "FAIL  loopback listen-and-connect failed inside the namespace"
  FAILURES=$((FAILURES + 1))
fi

echo
if [ "$FAILURES" = "0" ]; then
  echo "netns-selftest: all cases behaved"
  exit 0
fi
echo "netns-selftest: $FAILURES case(s) did not behave"
exit 1
