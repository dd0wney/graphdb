package lsm

// MergeIterator merges multiple sorted iterators
type MergeIterator struct {
	iterators []*SSTableIterator
}

// SSTableIterator iterates over an SSTable
type SSTableIterator struct {
	sst     *SSTable
	entries []*Entry
	index   int
}

// NewSSTableIterator creates an iterator for an SSTable
func NewSSTableIterator(sst *SSTable) (*SSTableIterator, error) {
	entries, err := sst.Iterator()
	if err != nil {
		return nil, err
	}

	return &SSTableIterator{
		sst:     sst,
		entries: entries,
		index:   0,
	}, nil
}

// Next advances the iterator
func (it *SSTableIterator) Next() (*Entry, bool) {
	if it.index >= len(it.entries) {
		return nil, false
	}

	entry := it.entries[it.index]
	it.index++
	return entry, true
}

// Peek returns current entry without advancing
func (it *SSTableIterator) Peek() (*Entry, bool) {
	if it.index >= len(it.entries) {
		return nil, false
	}
	return it.entries[it.index], true
}

// NewMergeIterator creates an iterator that merges multiple SSTables
func NewMergeIterator(sstables []*SSTable) (*MergeIterator, error) {
	iterators := make([]*SSTableIterator, 0, len(sstables))

	for _, sst := range sstables {
		it, err := NewSSTableIterator(sst)
		if err != nil {
			return nil, err
		}
		iterators = append(iterators, it)
	}

	return &MergeIterator{
		iterators: iterators,
	}, nil
}

// Next returns the next entry in sorted order across all iterators
func (mi *MergeIterator) Next() (*Entry, bool) {
	var minEntry *Entry
	var minIdx int = -1

	// Find minimum key across all iterators
	for i, it := range mi.iterators {
		entry, ok := it.Peek()
		if !ok {
			continue
		}

		if minEntry == nil || EntryCompare(entry, minEntry) < 0 {
			minEntry = entry
			minIdx = i
		}
	}

	if minIdx == -1 {
		return nil, false
	}

	// Advance the iterator with minimum key
	mi.iterators[minIdx].Next()
	return minEntry, true
}
