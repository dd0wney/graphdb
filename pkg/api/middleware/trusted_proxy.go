package middleware

import (
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
)

// TrustedProxyConfig configures trusted proxy handling
type TrustedProxyConfig struct {
	// TrustedNetworks holds the list of trusted proxy CIDR ranges.
	// Only requests from these IPs will have X-Forwarded-For/X-Real-IP headers trusted.
	TrustedNetworks []*net.IPNet
}

var (
	globalTrustedProxies     []*net.IPNet
	globalTrustedProxiesOnce sync.Once
)

// InitTrustedProxiesFromEnv initializes trusted proxies from TRUSTED_PROXIES environment variable.
// Format: comma-separated CIDR ranges, e.g., "10.0.0.0/8,172.16.0.0/12,192.168.0.0/16"
// This is called automatically on first use, but can be called explicitly during server init.
func InitTrustedProxiesFromEnv() {
	globalTrustedProxiesOnce.Do(func() {
		proxiesEnv := os.Getenv("TRUSTED_PROXIES")
		if proxiesEnv == "" {
			log.Printf("TRUSTED_PROXIES not set - X-Forwarded-For/X-Real-IP headers will NOT be trusted")
			return
		}

		globalTrustedProxies = ParseTrustedProxies(proxiesEnv)

		if len(globalTrustedProxies) > 0 {
			log.Printf("Trusted proxies configured: %d networks", len(globalTrustedProxies))
		}
	})
}

// ParseTrustedProxies parses a comma-separated list of CIDR ranges or IP addresses.
func ParseTrustedProxies(proxiesStr string) []*net.IPNet {
	var networks []*net.IPNet

	cidrs := strings.Split(proxiesStr, ",")
	for _, cidr := range cidrs {
		cidr = strings.TrimSpace(cidr)
		if cidr == "" {
			continue
		}

		// Handle single IPs by appending /32 or /128
		if !strings.Contains(cidr, "/") {
			ip := net.ParseIP(cidr)
			if ip == nil {
				log.Printf("Invalid trusted proxy IP: %s", cidr)
				continue
			}
			if ip.To4() != nil {
				cidr = cidr + "/32"
			} else {
				cidr = cidr + "/128"
			}
		}

		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			log.Printf("Invalid trusted proxy CIDR: %s: %v", cidr, err)
			continue
		}
		networks = append(networks, network)
	}

	return networks
}

// IsTrustedProxy checks if the given remote address is from a trusted proxy.
// Uses the global trusted proxies list initialized from environment.
func IsTrustedProxy(remoteAddr string) bool {
	InitTrustedProxiesFromEnv() // Ensure initialized
	return IsTrustedProxyIn(remoteAddr, globalTrustedProxies)
}

// IsTrustedProxyIn checks if the given remote address is in the provided networks.
func IsTrustedProxyIn(remoteAddr string, trustedNetworks []*net.IPNet) bool {
	if len(trustedNetworks) == 0 {
		return false
	}

	// Extract IP from host:port format
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		// Maybe it's just an IP without port
		host = remoteAddr
	}

	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}

	for _, network := range trustedNetworks {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

// GetClientIP extracts the client IP address from a request.
// SECURITY: Only trusts X-Forwarded-For and X-Real-IP headers when the request
// comes from a configured trusted proxy. This prevents IP spoofing attacks.
func GetClientIP(r *http.Request) string {
	return GetClientIPWithProxies(r, globalTrustedProxies)
}

// GetClientIPWithProxies extracts the client IP with custom trusted proxy list.
func GetClientIPWithProxies(r *http.Request, trustedNetworks []*net.IPNet) string {
	// Only trust forwarding headers if request is from a trusted proxy
	if IsTrustedProxyIn(r.RemoteAddr, trustedNetworks) {
		// Try X-Real-IP first (typically set by nginx)
		if ip := r.Header.Get("X-Real-IP"); ip != "" {
			if parsedIP := net.ParseIP(strings.TrimSpace(ip)); parsedIP != nil {
				return parsedIP.String()
			}
		}

		// Try X-Forwarded-For (may contain multiple IPs: client, proxy1, proxy2...)
		// The leftmost IP is the original client
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			parts := strings.Split(xff, ",")
			if len(parts) > 0 {
				clientIP := strings.TrimSpace(parts[0])
				if parsedIP := net.ParseIP(clientIP); parsedIP != nil {
					return parsedIP.String()
				}
			}
		}
	}

	// Fall back to direct connection IP (always trusted)
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		// RemoteAddr might be just an IP without port in some cases
		return r.RemoteAddr
	}
	return host
}
