package storage

import "fmt"

// guardEphemeralDirEmpty refuses an Ephemeral open unless the data directory
// is empty, apart from the LOCK file this open has just taken.
//
// An Ephemeral store writes nothing back, so pointed at a durable store it
// would load nothing (no WAL replay), serve an empty graph, and leave the
// real data on disk untouched but invisible. Refusing loudly is the only safe
// answer.
//
// It checks for emptiness rather than for a list of known artefacts
// (snapshot.json, snapshot.mmap, wal/, edgestore/, rules.json) because such a
// list is wrong the day someone adds a new file kind, and silently so.
//
// Constructor-only; no locking. Must run after lockDataDir and before the
// first write into the directory.
//
// Returns an error that wraps ErrEphemeralStore and names the directory and
// the first entry it found.
func (gs *GraphStorage) guardEphemeralDirEmpty() error {
	entries, err := gs.fs.ReadDir(gs.dataDir)
	if err != nil {
		// A guard against pointing at a durable store cannot treat "could
		// not look" as "nothing there".
		return fmt.Errorf("%w: list data directory %q to check it is empty: %w",
			ErrEphemeralStore, gs.dataDir, err)
	}
	for _, e := range entries {
		if e.Name() != dataDirLockFile {
			return fmt.Errorf("%w: data directory %q is not empty (found %q). "+
				"An Ephemeral store needs a fresh directory, such as one from os.MkdirTemp",
				ErrEphemeralStore, gs.dataDir, e.Name())
		}
	}
	return nil
}
