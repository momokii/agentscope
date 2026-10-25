package ports

import (
	"testing"
)

// Given: no BIND_ADDR in the environment.
// When: config loads.
// Then: the bind stays loopback-only (zero behavior change).
func Test_LoadConfigFromEnv_DefaultBindIsLoopback(t *testing.T) {
	t.Setenv("BIND_ADDR", "")
	cfg := LoadConfigFromEnv()
	if cfg.BindAddr != "127.0.0.1" {
		t.Fatalf("default BindAddr = %q, want %q", cfg.BindAddr, "127.0.0.1")
	}
	if got := cfg.Addr(); got != "127.0.0.1:"+cfg.Port {
		t.Fatalf("default Addr = %q", got)
	}
}

// Given: BIND_ADDR set (opt-in, e.g. tailnet 0.0.0.0).
// When: config loads.
// Then: the value flows through to Addr() unchanged.
func Test_LoadConfigFromEnv_BindAddrOverride(t *testing.T) {
	t.Setenv("BIND_ADDR", "0.0.0.0")
	cfg := LoadConfigFromEnv()
	if cfg.BindAddr != "0.0.0.0" {
		t.Fatalf("BindAddr = %q, want %q", cfg.BindAddr, "0.0.0.0")
	}
	if got := cfg.Addr(); got != "0.0.0.0:"+cfg.Port {
		t.Fatalf("Addr = %q, want %q", got, "0.0.0.0:"+cfg.Port)
	}
}
