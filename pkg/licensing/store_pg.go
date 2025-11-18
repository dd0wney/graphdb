package licensing

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	_ "github.com/lib/pq"
)

// PGStore handles license persistence using PostgreSQL
type PGStore struct {
	db *sql.DB
}

// NewPGStore creates a new PostgreSQL-backed license store
func NewPGStore(databaseURL string) (*PGStore, error) {
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Connection pooling (Railway best practice)
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)

	// Verify connection
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("database unreachable: %w", err)
	}

	s := &PGStore{db: db}

	// Create tables if they don't exist
	if err := s.migrate(); err != nil {
		return nil, fmt.Errorf("migration failed: %w", err)
	}

	return s, nil
}

// migrate creates the necessary database tables
func (s *PGStore) migrate() error {
	schema := `
	CREATE TABLE IF NOT EXISTS licenses (
		id TEXT PRIMARY KEY,
		key TEXT UNIQUE NOT NULL,
		type TEXT NOT NULL,
		email TEXT NOT NULL,
		customer_id TEXT,
		subscription_id TEXT,
		status TEXT NOT NULL,
		created_at TIMESTAMP NOT NULL,
		expires_at TIMESTAMP,
		metadata JSONB
	);

	CREATE INDEX IF NOT EXISTS idx_licenses_key ON licenses(key);
	CREATE INDEX IF NOT EXISTS idx_licenses_customer_id ON licenses(customer_id);
	CREATE INDEX IF NOT EXISTS idx_licenses_email ON licenses(email);
	CREATE INDEX IF NOT EXISTS idx_licenses_status ON licenses(status);
	`

	_, err := s.db.Exec(schema)
	return err
}

// CreateLicense stores a new license
func (s *PGStore) CreateLicense(license *License) error {
	metadataJSON, err := json.Marshal(license.Metadata)
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	query := `
		INSERT INTO licenses (id, key, type, email, customer_id, subscription_id, status, created_at, expires_at, metadata)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`

	_, err = s.db.Exec(query,
		license.ID,
		license.Key,
		license.Type,
		license.Email,
		license.CustomerID,
		license.SubscriptionID,
		license.Status,
		license.CreatedAt,
		license.ExpiresAt,
		metadataJSON,
	)

	if err != nil {
		return fmt.Errorf("failed to create license: %w", err)
	}

	return nil
}

// GetLicense retrieves a license by ID
func (s *PGStore) GetLicense(id string) (*License, error) {
	query := `
		SELECT id, key, type, email, customer_id, subscription_id, status, created_at, expires_at, metadata
		FROM licenses
		WHERE id = $1
	`

	license := &License{}
	var metadataJSON []byte

	err := s.db.QueryRow(query, id).Scan(
		&license.ID,
		&license.Key,
		&license.Type,
		&license.Email,
		&license.CustomerID,
		&license.SubscriptionID,
		&license.Status,
		&license.CreatedAt,
		&license.ExpiresAt,
		&metadataJSON,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("license not found: %s", id)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get license: %w", err)
	}

	if len(metadataJSON) > 0 {
		if err := json.Unmarshal(metadataJSON, &license.Metadata); err != nil {
			return nil, fmt.Errorf("failed to unmarshal metadata: %w", err)
		}
	}

	return license, nil
}

// GetLicenseByKey retrieves a license by its key
func (s *PGStore) GetLicenseByKey(key string) (*License, error) {
	query := `
		SELECT id, key, type, email, customer_id, subscription_id, status, created_at, expires_at, metadata
		FROM licenses
		WHERE key = $1
	`

	license := &License{}
	var metadataJSON []byte

	err := s.db.QueryRow(query, key).Scan(
		&license.ID,
		&license.Key,
		&license.Type,
		&license.Email,
		&license.CustomerID,
		&license.SubscriptionID,
		&license.Status,
		&license.CreatedAt,
		&license.ExpiresAt,
		&metadataJSON,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("license key not found")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get license: %w", err)
	}

	if len(metadataJSON) > 0 {
		if err := json.Unmarshal(metadataJSON, &license.Metadata); err != nil {
			return nil, fmt.Errorf("failed to unmarshal metadata: %w", err)
		}
	}

	return license, nil
}

// GetLicenseByCustomer retrieves a license by Stripe customer ID
func (s *PGStore) GetLicenseByCustomer(customerID string) (*License, error) {
	query := `
		SELECT id, key, type, email, customer_id, subscription_id, status, created_at, expires_at, metadata
		FROM licenses
		WHERE customer_id = $1
	`

	license := &License{}
	var metadataJSON []byte

	err := s.db.QueryRow(query, customerID).Scan(
		&license.ID,
		&license.Key,
		&license.Type,
		&license.Email,
		&license.CustomerID,
		&license.SubscriptionID,
		&license.Status,
		&license.CreatedAt,
		&license.ExpiresAt,
		&metadataJSON,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("no license found for customer: %s", customerID)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get license: %w", err)
	}

	if len(metadataJSON) > 0 {
		if err := json.Unmarshal(metadataJSON, &license.Metadata); err != nil {
			return nil, fmt.Errorf("failed to unmarshal metadata: %w", err)
		}
	}

	return license, nil
}

// UpdateLicense updates an existing license
func (s *PGStore) UpdateLicense(license *License) error {
	metadataJSON, err := json.Marshal(license.Metadata)
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	query := `
		UPDATE licenses
		SET key = $2, type = $3, email = $4, customer_id = $5, subscription_id = $6,
		    status = $7, expires_at = $8, metadata = $9
		WHERE id = $1
	`

	result, err := s.db.Exec(query,
		license.ID,
		license.Key,
		license.Type,
		license.Email,
		license.CustomerID,
		license.SubscriptionID,
		license.Status,
		license.ExpiresAt,
		metadataJSON,
	)

	if err != nil {
		return fmt.Errorf("failed to update license: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return fmt.Errorf("license not found: %s", license.ID)
	}

	return nil
}

// ListLicenses returns all licenses
func (s *PGStore) ListLicenses() []*License {
	query := `
		SELECT id, key, type, email, customer_id, subscription_id, status, created_at, expires_at, metadata
		FROM licenses
		ORDER BY created_at DESC
	`

	rows, err := s.db.Query(query)
	if err != nil {
		return []*License{}
	}
	defer rows.Close()

	licenses := []*License{}
	for rows.Next() {
		license := &License{}
		var metadataJSON []byte

		err := rows.Scan(
			&license.ID,
			&license.Key,
			&license.Type,
			&license.Email,
			&license.CustomerID,
			&license.SubscriptionID,
			&license.Status,
			&license.CreatedAt,
			&license.ExpiresAt,
			&metadataJSON,
		)

		if err != nil {
			continue
		}

		if len(metadataJSON) > 0 {
			json.Unmarshal(metadataJSON, &license.Metadata)
		}

		licenses = append(licenses, license)
	}

	return licenses
}

// Ping checks database connectivity
func (s *PGStore) Ping() error {
	return s.db.Ping()
}

// Close closes the database connection
func (s *PGStore) Close() error {
	return s.db.Close()
}
