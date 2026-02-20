package replication

import "testing"

func TestExtractHostFromAddr(t *testing.T) {
	tests := []struct {
		name     string
		addr     string
		expected string
	}{
		{"with_port", "localhost:9090", "localhost"},
		{"ip_with_port", "192.168.1.1:9090", "192.168.1.1"},
		{"no_port", "localhost", "localhost"},
		{"empty", "", ""},
		{"ipv6_with_port", "[::1]:9090", "[::1]"},
		{"hostname_with_port", "primary.example.com:9090", "primary.example.com"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractHostFromAddr(tt.addr)
			if got != tt.expected {
				t.Errorf("extractHostFromAddr(%q) = %q, want %q", tt.addr, got, tt.expected)
			}
		})
	}
}
