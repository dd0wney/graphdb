package updater

import (
	"testing"
)

func TestIsVersionNewer(t *testing.T) {
	tests := []struct {
		current string
		latest  string
		want    bool
	}{
		{"v1.0.0", "v1.1.0", true},
		{"1.0.0", "1.1.0", true},
		{"v1.2.3", "v1.2.4", true},
		{"v2.0.0", "v1.9.9", false},
		{"v1.1.0", "v1.1.0", false},
		{"dev", "v1.0.0", true},
		{"", "v0.1.0", true},
	}

	for _, tt := range tests {
		t.Run(tt.current+"_vs_"+tt.latest, func(t *testing.T) {
			if got := isVersionNewer(tt.current, tt.latest); got != tt.want {
				t.Errorf("isVersionNewer(%q, %q) = %v, want %v", tt.current, tt.latest, got, tt.want)
			}
		})
	}
}
