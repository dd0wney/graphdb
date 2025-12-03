package api

import (
	"github.com/dd0wney/cluso-graphdb/pkg/api/middleware"
)

// Re-export types from middleware package for backward compatibility
type (
	CORSConfig      = middleware.CORSConfig
	RateLimitConfig = middleware.RateLimitConfig
	RateLimiter     = middleware.RateLimiter
)

// Re-export functions from middleware package
var (
	DefaultCORSConfig      = middleware.DefaultCORSConfig
	DefaultRateLimitConfig = middleware.DefaultRateLimitConfig
	NewRateLimiter         = middleware.NewRateLimiter
	GetRequestID           = middleware.GetRequestID
	GetClientIP            = middleware.GetClientIP
	IsTrustedProxy         = middleware.IsTrustedProxy
)
