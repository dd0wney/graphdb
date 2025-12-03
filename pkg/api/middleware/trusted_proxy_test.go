package middleware

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestParseTrustedProxies(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected int // Number of networks parsed
	}{
		{
			name:     "empty string",
			input:    "",
			expected: 0,
		},
		{
			name:     "single CIDR",
			input:    "10.0.0.0/8",
			expected: 1,
		},
		{
			name:     "multiple CIDRs",
			input:    "10.0.0.0/8,172.16.0.0/12,192.168.0.0/16",
			expected: 3,
		},
		{
			name:     "single IPv4 address",
			input:    "10.0.0.1",
			expected: 1,
		},
		{
			name:     "single IPv6 address",
			input:    "::1",
			expected: 1,
		},
		{
			name:     "mixed IPs and CIDRs",
			input:    "10.0.0.1,192.168.0.0/16,::1",
			expected: 3,
		},
		{
			name:     "with whitespace",
			input:    "  10.0.0.0/8 , 172.16.0.0/12  ",
			expected: 2,
		},
		{
			name:     "invalid CIDR ignored",
			input:    "10.0.0.0/8,invalid,192.168.0.0/16",
			expected: 2,
		},
		{
			name:     "invalid IP ignored",
			input:    "10.0.0.0/8,not-an-ip,192.168.0.0/16",
			expected: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ParseTrustedProxies(tt.input)
			if len(result) != tt.expected {
				t.Errorf("ParseTrustedProxies(%q) returned %d networks, expected %d",
					tt.input, len(result), tt.expected)
			}
		})
	}
}

func TestIsTrustedProxyIn(t *testing.T) {
	// Configure test networks
	_, network1, _ := net.ParseCIDR("10.0.0.0/8")
	_, network2, _ := net.ParseCIDR("172.16.0.0/12")
	_, network3, _ := net.ParseCIDR("::1/128")
	trustedNetworks := []*net.IPNet{network1, network2, network3}

	tests := []struct {
		name       string
		remoteAddr string
		expected   bool
	}{
		{"10.x.x.x is trusted", "10.0.0.1:8080", true},
		{"10.255.255.255 is trusted", "10.255.255.255:443", true},
		{"172.16.x.x is trusted", "172.16.0.1:8080", true},
		{"172.31.255.255 is trusted", "172.31.255.255:80", true},
		{"192.168.x.x is NOT trusted", "192.168.1.1:8080", false},
		{"public IP is NOT trusted", "203.0.113.50:8080", false},
		{"IPv6 localhost is trusted", "[::1]:8080", true},
		{"invalid address", "not-an-ip:8080", false},
		{"empty address", "", false},
		{"IP without port", "10.0.0.1", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsTrustedProxyIn(tt.remoteAddr, trustedNetworks)
			if result != tt.expected {
				t.Errorf("IsTrustedProxyIn(%q) = %v, expected %v",
					tt.remoteAddr, result, tt.expected)
			}
		})
	}

	// Test with nil/empty networks
	t.Run("nil networks returns false", func(t *testing.T) {
		if IsTrustedProxyIn("10.0.0.1:8080", nil) {
			t.Error("Expected false with nil networks")
		}
	})

	t.Run("empty networks returns false", func(t *testing.T) {
		if IsTrustedProxyIn("10.0.0.1:8080", []*net.IPNet{}) {
			t.Error("Expected false with empty networks")
		}
	})
}

func TestGetClientIPWithProxies(t *testing.T) {
	// Configure 10.0.0.0/8 as trusted proxy network
	_, network, _ := net.ParseCIDR("10.0.0.0/8")
	trustedNetworks := []*net.IPNet{network}

	tests := []struct {
		name       string
		remoteAddr string
		xRealIP    string
		xForwarded string
		expected   string
	}{
		{
			name:       "X-Real-IP trusted from trusted proxy",
			remoteAddr: "10.0.0.1:8080",
			xRealIP:    "203.0.113.50",
			expected:   "203.0.113.50",
		},
		{
			name:       "X-Forwarded-For trusted from trusted proxy",
			remoteAddr: "10.0.0.1:8080",
			xForwarded: "203.0.113.51",
			expected:   "203.0.113.51",
		},
		{
			name:       "X-Real-IP takes precedence over X-Forwarded-For",
			remoteAddr: "10.0.0.1:8080",
			xRealIP:    "203.0.113.50",
			xForwarded: "203.0.113.51",
			expected:   "203.0.113.50",
		},
		{
			name:       "X-Forwarded-For with multiple IPs uses first",
			remoteAddr: "10.0.0.1:8080",
			xForwarded: "203.0.113.51, 10.0.0.2, 10.0.0.3",
			expected:   "203.0.113.51",
		},
		{
			name:       "headers ignored from untrusted source",
			remoteAddr: "192.168.1.1:8080",
			xRealIP:    "203.0.113.50",
			expected:   "192.168.1.1",
		},
		{
			name:       "no headers from trusted proxy uses RemoteAddr",
			remoteAddr: "10.0.0.1:8080",
			expected:   "10.0.0.1",
		},
		{
			name:       "invalid X-Real-IP ignored",
			remoteAddr: "10.0.0.1:8080",
			xRealIP:    "not-an-ip",
			expected:   "10.0.0.1",
		},
		{
			name:       "invalid X-Forwarded-For ignored",
			remoteAddr: "10.0.0.1:8080",
			xForwarded: "not-an-ip, garbage",
			expected:   "10.0.0.1",
		},
		{
			name:       "direct connection without port",
			remoteAddr: "192.168.1.1",
			expected:   "192.168.1.1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			req.RemoteAddr = tt.remoteAddr

			if tt.xRealIP != "" {
				req.Header.Set("X-Real-IP", tt.xRealIP)
			}
			if tt.xForwarded != "" {
				req.Header.Set("X-Forwarded-For", tt.xForwarded)
			}

			result := GetClientIPWithProxies(req, trustedNetworks)
			if result != tt.expected {
				t.Errorf("GetClientIPWithProxies() = %q, expected %q", result, tt.expected)
			}
		})
	}
}

func TestGetClientIPWithProxies_NoTrustedProxies(t *testing.T) {
	// With no trusted proxies, headers should always be ignored
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.RemoteAddr = "192.168.1.1:8080"
	req.Header.Set("X-Real-IP", "203.0.113.50")
	req.Header.Set("X-Forwarded-For", "203.0.113.51")

	result := GetClientIPWithProxies(req, nil)
	if result != "192.168.1.1" {
		t.Errorf("Expected RemoteAddr IP, got %q", result)
	}
}

func BenchmarkIsTrustedProxyIn(b *testing.B) {
	// Setup networks
	networks := ParseTrustedProxies("10.0.0.0/8,172.16.0.0/12,192.168.0.0/16")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		IsTrustedProxyIn("10.0.0.1:8080", networks)
	}
}

func BenchmarkGetClientIPWithProxies(b *testing.B) {
	networks := ParseTrustedProxies("10.0.0.0/8")
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.RemoteAddr = "10.0.0.1:8080"
	req.Header.Set("X-Forwarded-For", "203.0.113.50, 10.0.0.2")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		GetClientIPWithProxies(req, networks)
	}
}
