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

// Given: no DB_TIMEOUT in the environment.
// When: config loads.
// Then: the per-source DB budget defaults to 10s (headroom past the old 5s
// constant that starved every tick on the 8.2GB live db).
func Test_LoadConfigFromEnv_DefaultDBTimeout(t *testing.T) {
	t.Setenv("DB_TIMEOUT", "")
	cfg := LoadConfigFromEnv()
	if cfg.DBTimeout != 10_000_000_000 {
		t.Fatalf("default DBTimeout = %v, want 10s", cfg.DBTimeout)
	}
}

// Given: DB_TIMEOUT set to a valid duration.
// When: config loads.
// Then: the value flows through unchanged.
func Test_LoadConfigFromEnv_DBTimeoutOverride(t *testing.T) {
	t.Setenv("DB_TIMEOUT", "25s")
	cfg := LoadConfigFromEnv()
	if cfg.DBTimeout != 25_000_000_000 {
		t.Fatalf("DBTimeout = %v, want 25s", cfg.DBTimeout)
	}
}

// Given: DB_TIMEOUT malformed or non-positive.
// When: config loads.
// Then: the 10s default holds (fail-safe, never a zero budget).
func Test_LoadConfigFromEnv_DBTimeoutMalformedKeepsDefault(t *testing.T) {
	for _, raw := range []string{"banana", "0s", "-5s"} {
		t.Setenv("DB_TIMEOUT", raw)
		cfg := LoadConfigFromEnv()
		if cfg.DBTimeout != 10_000_000_000 {
			t.Fatalf("DBTimeout=%q -> %v, want 10s default", raw, cfg.DBTimeout)
		}
	}
}
