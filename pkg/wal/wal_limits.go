package wal

import "errors"

const (
	// walFilePerm and walDirPerm keep WAL files and directories
	// owner-only (security audit H-2). WAL entries contain the full
	// serialized JSON of every node and edge written since the last
	// snapshot — including all customer properties — so a world-readable
	// 0644 file exposed every tenant's data to any local OS user.
	walFilePerm = 0o600
	walDirPerm  = 0o700

	// maxWALRecordSize bounds the data length read from a single WAL
	// record before allocation (security audit H-4). DataLen is encoded
	// as a uint32, so a corrupted or crafted record can claim up to 4 GiB;
	// allocating that before the CRC is even checked OOM-kills the server
	// on every restart (a persistent DoS). A record larger than this cap
	// is treated as corruption — replay stops at the last valid record,
	// the same recovery path as a CRC mismatch.
	maxWALRecordSize = 64 << 20 // 64 MiB
)

// errWALRecordTooLarge is returned by the read path when a record's
// declared data length exceeds maxWALRecordSize.
var errWALRecordTooLarge = errors.New("wal: record data length exceeds maximum")
