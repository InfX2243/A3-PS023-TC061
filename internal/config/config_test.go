package config

import "testing"

func TestDefaultsValidate(t *testing.T) {
	cfg := Defaults()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("default configuration should validate: %v", err)
	}
}

func TestRejectsUnsafeConfiguration(t *testing.T) {
	tests := []struct {
		name string
		edit func(*Config)
	}{
		{"empty listen address", func(c *Config) { c.ListenAddr = " " }},
		{"zero vcpus", func(c *Config) { c.MaxVCPUs = 0 }},
		{"reserve consumes all memory", func(c *Config) { c.MemorySafetyReserveMiB = c.MaxMemoryMiB }},
		{"path-like image", func(c *Config) { c.AllowedImages = []string{"../etc/passwd"} }},
		{"empty images", func(c *Config) { c.AllowedImages = nil }},
		{"invalid shutdown timeout", func(c *Config) { c.ShutdownTimeoutSeconds = 0 }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Defaults()
			tt.edit(&cfg)
			if err := cfg.Validate(); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestWorkerEndpointConfigurationValidation(t *testing.T) {
	cfg := Defaults()
	cfg.Workers = []WorkerEndpoint{{ID: "worker-a", Name: "Worker A", Endpoint: "http://10.0.0.2:9090", MemoryLimitMiB: 7168}}
	if err := cfg.Validate(); err != nil { t.Fatalf("valid worker config rejected: %v", err) }

	tests := []struct {
		name string
		edit func(*WorkerEndpoint)
	}{
		{"unsafe ID", func(w *WorkerEndpoint) { w.ID = "../worker" }},
		{"URL with credentials", func(w *WorkerEndpoint) { w.Endpoint = "http://user:pass@worker:9090" }},
		{"non-http URL", func(w *WorkerEndpoint) { w.Endpoint = "file:///tmp/worker" }},
		{"zero memory cap", func(w *WorkerEndpoint) { w.MemoryLimitMiB = 0 }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := Defaults()
			worker := WorkerEndpoint{ID: "worker-a", Name: "Worker A", Endpoint: "http://worker:9090", MemoryLimitMiB: 7168}
			tc.edit(&worker)
			cfg.Workers = []WorkerEndpoint{worker}
			if err := cfg.Validate(); err == nil { t.Fatal("expected invalid worker config to be rejected") }
		})
	}
}
