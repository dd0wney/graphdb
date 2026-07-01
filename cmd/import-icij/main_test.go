package main

import "testing"

func TestResolveMmap(t *testing.T) {
	tests := []struct {
		name    string
		flag    string
		env     string
		want    bool
		wantErr bool
	}{
		{"default (nothing set) is mmap", "", "", true, false},
		{"env json opts out", "", "json", false, false},
		{"env jsonl opts out", "", "jsonl", false, false},
		{"env mmap is mmap", "", "mmap", true, false},
		{"env unknown falls back to mmap default", "", "bogus", true, false},
		{"flag json overrides env mmap", "json", "mmap", false, false},
		{"flag mmap overrides env json", "mmap", "json", true, false},
		{"flag invalid is an error", "yaml", "", false, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveMmap(tc.flag, tc.env)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("resolveMmap(%q,%q) = %v, want error", tc.flag, tc.env, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveMmap(%q,%q) unexpected error: %v", tc.flag, tc.env, err)
			}
			if got != tc.want {
				t.Errorf("resolveMmap(%q,%q) = %v, want %v", tc.flag, tc.env, got, tc.want)
			}
		})
	}
}
