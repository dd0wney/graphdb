package logging

import (
	"time"
)

// Common field constructors
func String(key, value string) Field {
	return Field{Key: key, Value: value}
}

func Int(key string, value int) Field {
	return Field{Key: key, Value: value}
}

func Int64(key string, value int64) Field {
	return Field{Key: key, Value: value}
}

func Uint64(key string, value uint64) Field {
	return Field{Key: key, Value: value}
}

func Float64(key string, value float64) Field {
	return Field{Key: key, Value: value}
}

func Bool(key string, value bool) Field {
	return Field{Key: key, Value: value}
}

func Duration(key string, value time.Duration) Field {
	return Field{Key: key, Value: value.String()}
}

func Error(err error) Field {
	if err == nil {
		return Field{Key: "error", Value: nil}
	}
	return Field{Key: "error", Value: err.Error()}
}

func Any(key string, value any) Field {
	return Field{Key: key, Value: value}
}

// Component field helpers for common component names
func Component(name string) Field {
	return String("component", name)
}

func NodeID(id uint64) Field {
	return Uint64("node_id", id)
}

func EdgeID(id uint64) Field {
	return Uint64("edge_id", id)
}

func Operation(op string) Field {
	return String("operation", op)
}

func Latency(d time.Duration) Field {
	return Duration("latency", d)
}

func Count(n int) Field {
	return Int("count", n)
}

func Path(p string) Field {
	return String("path", p)
}
