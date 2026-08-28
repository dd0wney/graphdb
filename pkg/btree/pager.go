package btree

import (
	"errors"
	"fmt"
	"os"
	"sync"

	"github.com/dd0wney/graphdb/pkg/vfs"
)

// TODO: tombstone compaction. Tree.Delete writes zero-length values
// as tombstones rather than removing keys; over time, pages
// accumulate dead slots that the pager cannot reclaim without a
// passive scan. A compaction pass would rewrite leaves whose
// tombstone fraction exceeds a threshold. Out of scope for the
// btree primitive; expected to live in a higher layer that owns
// scheduling (e.g. the storage backend that consumes Tree).

const (
	// PageSize is the standard size for a B+Tree page (4KB)
	PageSize = 4096
)

var (
	ErrPageNotFound = errors.New("page not found")
)

// Page represents a fixed-size block of data on disk
type Page struct {
	ID   uint64
	Data []byte
}

// Pager manages reading and writing pages to a file
type Pager struct {
	// fs is the filesystem driver. vfs.Default() unless a caller chose another
	// through NewPagerWithFS, which is how an I/O fault or a simulated power
	// cut reaches this code by the path production takes.
	fs        vfs.FileSystem
	file      vfs.File
	mu        sync.RWMutex
	maxPage   uint64
	cache     map[uint64]*Page
	cacheSize int
}

// NewPager creates a new pager for the given file path, on the default
// filesystem driver.
func NewPager(path string) (*Pager, error) {
	return NewPagerWithFS(path, vfs.Default())
}

// NewPagerWithFS creates a pager on a caller-supplied filesystem driver.
// Intended for testing; see pkg/vfs and docs/adr/0002.
func NewPagerWithFS(path string, fs vfs.FileSystem) (*Pager, error) {
	if fs == nil {
		fs = vfs.Default()
	}
	file, err := fs.Open(path, os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open pager file: %w", err)
	}

	info, err := fs.Stat(path)
	if err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("failed to stat pager file: %w", err)
	}

	maxPage := uint64(info.Size() / PageSize)

	return &Pager{
		fs:        fs,
		file:      file,
		maxPage:   maxPage,
		cache:     make(map[uint64]*Page),
		cacheSize: 1000, // Default cache size
	}, nil
}

// ReadPage reads a page from disk or cache
func (p *Pager) ReadPage(pageID uint64) (*Page, error) {
	p.mu.RLock()
	if page, ok := p.cache[pageID]; ok {
		p.mu.RUnlock()
		return page, nil
	}
	p.mu.RUnlock()

	if pageID >= p.maxPage {
		return nil, ErrPageNotFound
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	// Double check cache after acquiring lock
	if page, ok := p.cache[pageID]; ok {
		return page, nil
	}

	data := make([]byte, PageSize)
	offset := int64(pageID * PageSize)
	_, err := p.file.ReadAt(data, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to read page %d: %w", pageID, err)
	}

	page := &Page{ID: pageID, Data: data}
	p.addToCache(page)

	return page, nil
}

// WritePage writes a page to disk and cache
func (p *Pager) WritePage(page *Page) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	offset := int64(page.ID * PageSize)
	_, err := p.file.WriteAt(page.Data, offset)
	if err != nil {
		return fmt.Errorf("failed to write page %d: %w", page.ID, err)
	}

	if page.ID >= p.maxPage {
		p.maxPage = page.ID + 1
	}

	p.addToCache(page)
	return nil
}

// AllocatePage creates a new page at the end of the file
func (p *Pager) AllocatePage() (*Page, error) {
	p.mu.Lock()
	pageID := p.maxPage
	p.maxPage++
	p.mu.Unlock()

	page := &Page{
		ID:   pageID,
		Data: make([]byte, PageSize),
	}

	// We don't write it yet, just return it for initialization
	return page, nil
}

// Close closes the pager file
func (p *Pager) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.file.Close()
}

// Flush writes all cached pages to disk and flushes the file
func (p *Pager) Flush() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, page := range p.cache {
		offset := int64(page.ID * PageSize)
		_, err := p.file.WriteAt(page.Data, offset)
		if err != nil {
			return fmt.Errorf("failed to flush page %d: %w", page.ID, err)
		}
	}

	return p.file.Sync()
}

func (p *Pager) addToCache(page *Page) {
	if len(p.cache) >= p.cacheSize {
		// Simple random eviction for now
		for k := range p.cache {
			delete(p.cache, k)
			break
		}
	}
	p.cache[page.ID] = page
}
