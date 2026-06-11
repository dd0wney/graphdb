package storage

import (
	"bytes"
	"encoding/binary"
	"fmt"
)

// snapshotMagic is "GSNP" — graphdb snapshot. Mirrors the LSA snapshot's
// GLSA discipline (pkg/search/lsa_persistence.go): wrong magic = "not a
// versioned snapshot" (pre-M-14 headerless file, handled via legacy
// fallback); right magic + unsupported version = "written by a newer
// binary," reported with an operator-actionable error.
var snapshotMagic = [4]byte{'G', 'S', 'N', 'P'}

const (
	// snapshotFormatVersion bumps when the envelope or payload format
	// changes incompatibly (CLAUDE.md § Snapshot format stability).
	// v1 (M-14): magic + version + flags envelope around the existing
	// JSON payload; the payload shape itself is unchanged.
	snapshotFormatVersion uint32 = 1

	// snapshotFlagEncrypted marks the payload as ciphertext from the
	// configured encryption engine. Replaces the pre-M-14 first-byte
	// `data[0] != '{'` heuristic, which a BOM or leading whitespace
	// could misclassify (AUDIT_security_2026-06-10 M-14).
	snapshotFlagEncrypted byte = 1 << 0

	snapshotHeaderSize = 4 + 4 + 1 // magic + version + flags
)

// encodeSnapshotEnvelope prefixes payload with the versioned header.
func encodeSnapshotEnvelope(payload []byte, encrypted bool) []byte {
	out := make([]byte, snapshotHeaderSize+len(payload))
	copy(out, snapshotMagic[:])
	binary.BigEndian.PutUint32(out[4:8], snapshotFormatVersion)
	if encrypted {
		out[8] |= snapshotFlagEncrypted
	}
	copy(out[snapshotHeaderSize:], payload)
	return out
}

// decodeSnapshotEnvelope splits a snapshot file into payload + flags.
// legacy=true means the file predates the envelope (headerless); the
// caller falls back to the first-byte heuristic for those. A legacy
// encrypted payload could in principle begin with the magic bytes
// (~2^-32 per file) — accepted odds, documented here.
func decodeSnapshotEnvelope(data []byte) (payload []byte, encrypted, legacy bool, err error) {
	if len(data) < snapshotHeaderSize || !bytes.Equal(data[:4], snapshotMagic[:]) {
		return data, false, true, nil
	}
	version := binary.BigEndian.Uint32(data[4:8])
	if version > snapshotFormatVersion {
		return nil, false, false, fmt.Errorf(
			"snapshot format version %d is newer than this binary supports (max %d); upgrade graphdb to read it",
			version, snapshotFormatVersion)
	}
	return data[snapshotHeaderSize:], data[8]&snapshotFlagEncrypted != 0, false, nil
}
