package licensing

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// Store handles license persistence
type Store struct {
	dataDir  string
	licenses map[string]*License // key: license ID
	byKey    map[string]string   // license key -> license ID
	mu       sync.RWMutex
}

// NewStore creates a new license store
func NewStore(dataDir string) (*Store, error) {
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, err
	}

	s := &Store{
		dataDir:  dataDir,
		licenses: make(map[string]*License),
		byKey:    make(map[string]string),
	}

	// Load existing licenses
	if err := s.load(); err != nil {
		return nil, err
	}

	return s, nil
}

// CreateLicense stores a new license
func (s *Store) CreateLicense(license *License) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.licenses[license.ID] = license
	s.byKey[license.Key] = license.ID

	return s.save()
}

// GetLicense retrieves a license by ID
func (s *Store) GetLicense(id string) (*License, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	license, ok := s.licenses[id]
	if !ok {
		return nil, fmt.Errorf("license not found: %s", id)
	}

	return license, nil
}

// GetLicenseByKey retrieves a license by its key
func (s *Store) GetLicenseByKey(key string) (*License, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	id, ok := s.byKey[key]
	if !ok {
		return nil, fmt.Errorf("license key not found")
	}

	return s.licenses[id], nil
}

// GetLicenseByCustomer retrieves a license by Stripe customer ID
func (s *Store) GetLicenseByCustomer(customerID string) (*License, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, license := range s.licenses {
		if license.CustomerID == customerID {
			return license, nil
		}
	}

	return nil, fmt.Errorf("no license found for customer: %s", customerID)
}

// UpdateLicense updates an existing license
func (s *Store) UpdateLicense(license *License) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.licenses[license.ID]; !ok {
		return fmt.Errorf("license not found: %s", license.ID)
	}

	s.licenses[license.ID] = license
	return s.save()
}

// ListLicenses returns all licenses
func (s *Store) ListLicenses() []*License {
	s.mu.RLock()
	defer s.mu.RUnlock()

	licenses := make([]*License, 0, len(s.licenses))
	for _, license := range s.licenses {
		licenses = append(licenses, license)
	}

	return licenses
}

// save persists licenses to disk
func (s *Store) save() error {
	data, err := json.MarshalIndent(s.licenses, "", "  ")
	if err != nil {
		return err
	}

	path := filepath.Join(s.dataDir, "licenses.json")
	return os.WriteFile(path, data, 0600)
}

// load reads licenses from disk
func (s *Store) load() error {
	path := filepath.Join(s.dataDir, "licenses.json")

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // No licenses yet
		}
		return err
	}

	if err := json.Unmarshal(data, &s.licenses); err != nil {
		return err
	}

	// Rebuild byKey index
	for id, license := range s.licenses {
		s.byKey[license.Key] = id
	}

	return nil
}

// Ping checks if store is accessible (always succeeds for file-based store)
func (s *Store) Ping() error {
	return nil
}

// Close is a no-op for file-based store
func (s *Store) Close() error {
	return nil
}
