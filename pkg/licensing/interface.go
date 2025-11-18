package licensing

// LicenseStore defines the interface for license persistence
type LicenseStore interface {
	CreateLicense(license *License) error
	GetLicense(id string) (*License, error)
	GetLicenseByKey(key string) (*License, error)
	GetLicenseByCustomer(customerID string) (*License, error)
	UpdateLicense(license *License) error
	ListLicenses() []*License
	Ping() error
	Close() error
}
