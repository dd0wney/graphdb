package licensing

import "context"

// migrate creates the necessary database tables
func (s *PGStore) migrate(ctx context.Context) error {
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

	_, err := s.pool.Exec(ctx, schema)
	return err
}
