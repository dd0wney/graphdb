package btree

import (
	"errors"
	"fmt"
	"os"
	"sync"
)

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
	file     *os.File
	mu       sync.RWMutex
	maxPage  uint64
	cache    map[uint64]*Page
	cacheSize int
}

// NewPager creates a new pager for the given file path
func NewPager(path string) (*Pager, error) {
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open pager file: %w", err)
	}

	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("failed to stat pager file: %w", err)
	}

	maxPage := uint64(info.Size() / PageSize)

	return &Pager{
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
