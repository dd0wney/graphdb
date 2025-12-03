package licensing

import "context"

// LicenseStore defines the interface for license persistence
type LicenseStore interface {
	CreateLicense(ctx context.Context, license *License) error
	GetLicense(ctx context.Context, id string) (*License, error)
	GetLicenseByKey(ctx context.Context, key string) (*License, error)
	GetLicenseByCustomer(ctx context.Context, customerID string) (*License, error)
	UpdateLicense(ctx context.Context, license *License) error
	ListLicenses(ctx context.Context) ([]*License, error)
	Ping(ctx context.Context) error
	Close() error
}
