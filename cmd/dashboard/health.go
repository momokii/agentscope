package main

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"opencode-dashboard/internal/ports"
)

// healthTimeout bounds the -healthcheck probe (container HEALTHCHECK).
const healthTimeout = 5 * time.Second

// runHealthcheck probes the local /api/healthz and maps it to a process
// exit code for the container HEALTHCHECK / compose healthcheck (exec form,
// no shell needed in the distroless runtime). It dials 127.0.0.1
// explicitly — never 0.0.0.0 — on the configured PORT.
func runHealthcheck() int {
	cfg := ports.LoadConfigFromEnv()
	url := "http://127.0.0.1:" + cfg.Port + "/api/healthz"
	ctx, cancel := context.WithTimeout(context.Background(), healthTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 1
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 1
	}
	defer func() { _ = resp.Body.Close() }()
	var body struct {
		Healthy bool `json:"healthy"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return 1
	}
	if resp.StatusCode != http.StatusOK || !body.Healthy {
		return 1
	}
	return 0
}
