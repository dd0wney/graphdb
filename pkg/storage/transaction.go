// Transaction support for GraphDB storage.
//
// This file is the package documentation for transaction support.
// The implementation is split across:
//   - transaction_types.go: Transaction struct and error definitions
//   - transaction_ops.go: CRUD operations within a transaction
//   - transaction_commit.go: Commit and rollback logic
package storage
