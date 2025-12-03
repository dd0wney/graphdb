// Package pools provides object pooling for reducing GC pressure.
//
// This package contains various pool implementations for commonly
// allocated types in the graph database:
//
//   - BytePool: Size-class based byte slice pooling
//   - Uint64Pool: Pooling for uint64 slices (edge lists, IDs)
//   - StringMapPool: Pooling for property maps
//   - BufferBuilder: Efficient buffer construction with pooling
package pools
