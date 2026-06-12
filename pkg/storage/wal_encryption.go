package storage

import (
	"bytes"
	"fmt"
)

// walEncMagic prefixes encrypted WAL payloads ("GWE1" — graphdb WAL
// encrypted, v1). H-3: SetEncryption used to cover only snapshot.json,
// leaving every WAL entry (everything since the last snapshot) as raw
// JSON — silent partial coverage. Marker presence is a deterministic
// discriminator, not a heuristic: a legacy plaintext payload is JSON and
// starts with '{'. Bump the digit if the sealed format ever changes.
var walEncMagic = [4]byte{'G', 'W', 'E', '1'}

// sealWALPayload encrypts a marshaled WAL payload through the same
// engine that protects the snapshot, prefixing walEncMagic; a no-op
// pass-through when encryption is disabled. Called by every WAL append
// dispatcher (writeToWALWithError, enqueueWAL, appendWALBatch,
// appendToWAL) so no write path can leak plaintext.
func (gs *GraphStorage) sealWALPayload(data []byte) ([]byte, error) {
	engine := gs.encryptionEngine
	if engine == nil {
		return data, nil
	}
	encrypted, err := engine.Encrypt(data)
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt WAL payload: %w", err)
	}
	out := make([]byte, len(walEncMagic)+len(encrypted))
	copy(out, walEncMagic[:])
	copy(out[len(walEncMagic):], encrypted)
	return out, nil
}

// openWALPayload reverses sealWALPayload during replay. Returns the
// plaintext payload and whether the entry was sealed; a legacy
// (pre-toggle) plaintext entry passes through unchanged so enabling
// encryption mid-life keeps old data replayable — the constructor then
// purges those entries via CompactWAL (see NewGraphStorageWithConfig).
// A sealed entry with no engine configured fails loud: replaying
// ciphertext as JSON would be silent corruption.
func (gs *GraphStorage) openWALPayload(data []byte) (payload []byte, sealed bool, err error) {
	if !bytes.HasPrefix(data, walEncMagic[:]) {
		return data, false, nil
	}
	if gs.encryptionEngine == nil {
		return nil, true, fmt.Errorf("WAL entry is encrypted but encryption is not enabled (set ENCRYPTION_ENABLED=true)")
	}
	decrypted, err := gs.encryptionEngine.Decrypt(data[len(walEncMagic):])
	if err != nil {
		return nil, true, fmt.Errorf("failed to decrypt WAL entry: %w", err)
	}
	return decrypted, true, nil
}
