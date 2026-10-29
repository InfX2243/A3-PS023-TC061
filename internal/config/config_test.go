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
