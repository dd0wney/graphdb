package licensing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// CreateLicense stores a new license
func (s *PGStore) CreateLicense(ctx context.Context, license *License) error {
	metadataJSON, err := json.Marshal(license.Metadata)
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	query := `
		INSERT INTO licenses (id, key, type, email, customer_id, subscription_id, status, created_at, expires_at, metadata)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`

	_, err = s.pool.Exec(ctx, query,
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
func (s *PGStore) GetLicense(ctx context.Context, id string) (*License, error) {
	query := `
		SELECT id, key, type, email, customer_id, subscription_id, status, created_at, expires_at, metadata
		FROM licenses
		WHERE id = $1
	`

	license := &License{}
	var metadataJSON []byte

	err := s.pool.QueryRow(ctx, query, id).Scan(
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

	if errors.Is(err, pgx.ErrNoRows) {
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
func (s *PGStore) GetLicenseByKey(ctx context.Context, key string) (*License, error) {
	query := `
		SELECT id, key, type, email, customer_id, subscription_id, status, created_at, expires_at, metadata
		FROM licenses
		WHERE key = $1
	`

	license := &License{}
	var metadataJSON []byte

	err := s.pool.QueryRow(ctx, query, key).Scan(
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

	if errors.Is(err, pgx.ErrNoRows) {
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
func (s *PGStore) GetLicenseByCustomer(ctx context.Context, customerID string) (*License, error) {
	query := `
		SELECT id, key, type, email, customer_id, subscription_id, status, created_at, expires_at, metadata
		FROM licenses
		WHERE customer_id = $1
	`

	license := &License{}
	var metadataJSON []byte

	err := s.pool.QueryRow(ctx, query, customerID).Scan(
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

	if errors.Is(err, pgx.ErrNoRows) {
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
func (s *PGStore) UpdateLicense(ctx context.Context, license *License) error {
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

	result, err := s.pool.Exec(ctx, query,
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

	if result.RowsAffected() == 0 {
		return fmt.Errorf("license not found: %s", license.ID)
	}

	return nil
}

// ListLicenses returns all licenses
func (s *PGStore) ListLicenses(ctx context.Context) ([]*License, error) {
	query := `
		SELECT id, key, type, email, customer_id, subscription_id, status, created_at, expires_at, metadata
		FROM licenses
		ORDER BY created_at DESC
	`

	rows, err := s.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to list licenses: %w", err)
	}
	defer rows.Close()

	var licenses []*License
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
			return nil, fmt.Errorf("failed to scan license: %w", err)
		}

		if len(metadataJSON) > 0 {
			json.Unmarshal(metadataJSON, &license.Metadata)
		}

		licenses = append(licenses, license)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating licenses: %w", err)
	}

	return licenses, nil
}
