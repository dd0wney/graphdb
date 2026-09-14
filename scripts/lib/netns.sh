#!/usr/bin/env bash
# netns.sh — run a command on a private loopback.
#
# Copied verbatim from dd0wney/graphdb-coord, scripts/lib/netns.sh, at commit
# 936a2ad (graphdb-coord PR #45). Nothing below is project-specific, so this
# is a straight copy with only this header added. If graphdb-coord fixes a
# defect in its copy, diff that file against 936a2ad and carry the fix here.
#
# Why: Portmaster (the Safing application firewall) queues every new
# connection on this host to userspace before it gives a verdict. A SYN to
# a closed loopback port has no owning process, so it is dropped instead of
# refused, and under load the verdict outlasts a client's dial timeout
# (graphdb-coord docs/OPERATIONS.md, "Host fact"). A new network namespace has
# its own loopback and its own netfilter tables, so nothing Portmaster
# installed sees the traffic. Measured 2026-09-13 (graphdb-coord): a closed
# port is refused in 15 ms inside the namespace and hangs for the full
# connect timeout outside it.
#
# The outer unshare maps the caller to root so it may bring lo up; the inner
# unshare maps that root back to the caller's uid with an empty capability
# set, so file permissions and the process identity are the same as on the
# host. A test that expects a mode 000 file to be unreadable passes either
# way.
#
# Source this file; do not execute it.

# netns_available returns 0 when this host can make a private network
# namespace with its loopback up (unshare present, user namespaces on).
netns_available() {
  unshare -rn sh -c 'ip link set lo up' >/dev/null 2>&1
}

# in_private_netns CMD [ARG...] runs CMD in a new network namespace with lo
# up, as the caller's uid and gid, with no capabilities. The exit status is
# CMD's. Exit 125 means the namespace could not be made.
in_private_netns() {
  unshare -rn bash -c '
    ip link set lo up || exit 125
    uid=$1; gid=$2; shift 2
    exec unshare -U --map-user="$uid" --map-group="$gid" -- "$@"
  ' _ "$(id -u)" "$(id -g)" "$@"
}
