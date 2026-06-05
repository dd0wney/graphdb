// Package cluster contains graphdb's cluster-coordination substrate: Raft-style
// leader election, membership tracking, and node discovery.
//
// Key types are [ClusterMembership] (nodes and their Primary/Replica/Candidate
// roles, heartbeats, quorum detection), [ElectionManager] (election timeouts,
// term tracking, vote collection), and seed-based node discovery.
//
// # Status: substrate only — not wired to the live write path
//
// This code is real and tested in isolation, but it is NOT connected to the
// graph store. There is no replication log or append path; EnableAutoFailover
// and EnableQuorumWrites default to false; and nothing outside this package's
// own tests imports it. graphdb runs single-node — the write path assumes one
// node. Treat this package as a foundation for future clustering work, not a
// shipping feature.
package cluster
