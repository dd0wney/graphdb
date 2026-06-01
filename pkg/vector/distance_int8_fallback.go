//go:build !(amd64 && goexperiment.simd)

package vector

// dotInt8 dispatches to the portable scalar kernel on every build that does
// not compile the amd64 archsimd path. See distance_int8_amd64.go for the
// SIMD implementation.
func dotInt8(a, b []int8) int32 { return dotInt8Scalar(a, b) }
