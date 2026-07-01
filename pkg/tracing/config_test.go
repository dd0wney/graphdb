package tracing

import "testing"

func TestConfigFromEnv(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want Config
	}{
		{
			name: "defaults when nothing set",
			env:  map[string]string{},
			want: Config{Enabled: false, Exporter: "otlp", Endpoint: "", ServiceName: "graphdb"},
		},
		{
			name: "master switch enables",
			env:  map[string]string{"GRAPHDB_TRACING_ENABLED": "true"},
			want: Config{Enabled: true, Exporter: "otlp", Endpoint: "", ServiceName: "graphdb"},
		},
		{
			name: "console exporter selected",
			env:  map[string]string{"GRAPHDB_TRACING_ENABLED": "true", "OTEL_TRACES_EXPORTER": "console"},
			want: Config{Enabled: true, Exporter: "console", Endpoint: "", ServiceName: "graphdb"},
		},
		{
			name: "otlp endpoint + service name",
			env: map[string]string{
				"GRAPHDB_TRACING_ENABLED":     "true",
				"OTEL_EXPORTER_OTLP_ENDPOINT": "localhost:4317",
				"OTEL_SERVICE_NAME":           "graphdb-prod",
			},
			want: Config{Enabled: true, Exporter: "otlp", Endpoint: "localhost:4317", ServiceName: "graphdb-prod"},
		},
		{
			name: "enabled=1 also true; unknown exporter falls back to otlp",
			env:  map[string]string{"GRAPHDB_TRACING_ENABLED": "1", "OTEL_TRACES_EXPORTER": "bogus"},
			want: Config{Enabled: true, Exporter: "otlp", Endpoint: "", ServiceName: "graphdb"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			got := ConfigFromEnv()
			if got != tc.want {
				t.Errorf("ConfigFromEnv() = %+v, want %+v", got, tc.want)
			}
		})
	}
}
