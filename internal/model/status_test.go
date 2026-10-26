package model

import "testing"

// Test_DeriveStatus_* — table-driven contract tests for the pure
// status-derivation function. One scenario per row; the run proves the
// precedence chain (archived > running > error > waiting-input > idle).

// Test_DeriveStatus_precedence verifies each status wins at its precedence
// level, including combined-signal conflicts. Every active case sets
// RecentActivity: staleness gating lives in Test_DeriveStatus_recency_gate.
func Test_DeriveStatus_precedence(t *testing.T) {
	tests := []struct {
		name    string
		signals StatusSignals
		want    SessionStatus
	}{
		{
			name:    "idle when no signals",
			signals: StatusSignals{},
			want:    SessionIdle,
		},
		{
			name:    "idle when latest role assistant and turn completed",
			signals: StatusSignals{LatestRole: "assistant", RecentActivity: true},
			want:    SessionIdle,
		},
		{
			name:    "running when assistant turn in flight",
			signals: StatusSignals{InFlightAssistant: true, LatestRole: "assistant", RecentActivity: true},
			want:    SessionRunning,
		},
		{
			name:    "running when tool part running",
			signals: StatusSignals{RunningToolParts: 2, LatestRole: "assistant", RecentActivity: true},
			want:    SessionRunning,
		},
		{
			name:    "running when fresh events observed",
			signals: StatusSignals{HasFreshEvents: true, LatestRole: "assistant", RecentActivity: true},
			want:    SessionRunning,
		},
		{
			name:    "waiting-input heuristic when user spoke last",
			signals: StatusSignals{LatestRole: "user", RecentActivity: true},
			want:    SessionWaitingInput,
		},
		{
			name:    "waiting-input heuristic when compacting",
			signals: StatusSignals{Compacting: true, LatestRole: "assistant", RecentActivity: true},
			want:    SessionWaitingInput,
		},
		{
			name:    "error when terminal error observed",
			signals: StatusSignals{HasError: true, LatestRole: "assistant", RecentActivity: true},
			want:    SessionError,
		},
		{
			name:    "archived when time_archived set",
			signals: StatusSignals{Archived: true, LatestRole: "assistant"},
			want:    SessionArchived,
		},
		{
			name: "archived beats error",
			signals: StatusSignals{
				Archived: true, HasError: true, InFlightAssistant: true,
			},
			want: SessionArchived,
		},
		{
			name: "running beats error when turn in flight",
			signals: StatusSignals{
				HasError: true, InFlightAssistant: true, LatestRole: "assistant", RecentActivity: true,
			},
			want: SessionRunning,
		},
		{
			name: "running beats error when tool part running",
			signals: StatusSignals{
				HasError: true, RunningToolParts: 1, LatestRole: "assistant", RecentActivity: true,
			},
			want: SessionRunning,
		},
		{
			name: "running beats error on fresh events",
			signals: StatusSignals{
				HasError: true, HasFreshEvents: true, LatestRole: "assistant", RecentActivity: true,
			},
			want: SessionRunning,
		},
		{
			name: "error when newest assistant errored and nothing in flight",
			signals: StatusSignals{
				HasError: true, LatestRole: "assistant", RecentActivity: true,
			},
			want: SessionError,
		},
		{
			name: "waiting-input when newest is user even with prior error cleared",
			signals: StatusSignals{
				LatestRole: "user", RecentActivity: true,
			},
			want: SessionWaitingInput,
		},
		{
			name: "running beats waiting-input heuristic",
			signals: StatusSignals{
				HasFreshEvents: true, LatestRole: "user", RecentActivity: true,
			},
			want: SessionRunning,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// When
			got := DeriveStatus(tt.signals)

			// Then
			if got != tt.want {
				t.Errorf("DeriveStatus(%+v) = %q, want %q", tt.signals, got, tt.want)
			}
			if !got.IsValid() {
				t.Errorf("DeriveStatus(%+v) = %q, not a valid contract status", tt.signals, got)
			}
		})
	}
}

// Test_DeriveStatus_recency_gate pins the status-truth contract: without
// recent session activity every live signal degrades to idle; archived still
// wins. RecentActivity is the caller's now-minus-time_updated verdict.
func Test_DeriveStatus_recency_gate(t *testing.T) {
	tests := []struct {
		name    string
		signals StatusSignals
		want    SessionStatus
	}{
		{
			name:    "recent in-flight assistant runs",
			signals: StatusSignals{InFlightAssistant: true, LatestRole: "assistant", RecentActivity: true},
			want:    SessionRunning,
		},
		{
			name:    "ancient in-flight assistant idles",
			signals: StatusSignals{InFlightAssistant: true, LatestRole: "assistant"},
			want:    SessionIdle,
		},
		{
			name:    "recent running tool runs",
			signals: StatusSignals{RunningToolParts: 1, LatestRole: "assistant", RecentActivity: true},
			want:    SessionRunning,
		},
		{
			name:    "ancient running tool idles",
			signals: StatusSignals{RunningToolParts: 1, LatestRole: "assistant"},
			want:    SessionIdle,
		},
		{
			name:    "recent fresh events run",
			signals: StatusSignals{HasFreshEvents: true, LatestRole: "assistant", RecentActivity: true},
			want:    SessionRunning,
		},
		{
			name:    "ancient fresh tail idles",
			signals: StatusSignals{HasFreshEvents: true, LatestRole: "assistant"},
			want:    SessionIdle,
		},
		{
			name:    "recent user-last waits for input",
			signals: StatusSignals{LatestRole: "user", RecentActivity: true},
			want:    SessionWaitingInput,
		},
		{
			name:    "ancient user-last idles",
			signals: StatusSignals{LatestRole: "user"},
			want:    SessionIdle,
		},
		{
			name:    "recent error errors",
			signals: StatusSignals{HasError: true, LatestRole: "assistant", RecentActivity: true},
			want:    SessionError,
		},
		{
			name:    "ancient error idles",
			signals: StatusSignals{HasError: true, LatestRole: "assistant"},
			want:    SessionIdle,
		},
		{
			name:    "ancient compacting idles",
			signals: StatusSignals{Compacting: true, LatestRole: "assistant"},
			want:    SessionIdle,
		},
		{
			name:    "archived wins over recency",
			signals: StatusSignals{Archived: true, HasError: true, InFlightAssistant: true, RecentActivity: true},
			want:    SessionArchived,
		},
		{
			name:    "archived wins over staleness",
			signals: StatusSignals{Archived: true, InFlightAssistant: true},
			want:    SessionArchived,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// When
			got := DeriveStatus(tt.signals)

			// Then
			if got != tt.want {
				t.Errorf("DeriveStatus(%+v) = %q, want %q", tt.signals, got, tt.want)
			}
			if !got.IsValid() {
				t.Errorf("DeriveStatus(%+v) = %q, not a valid contract status", tt.signals, got)
			}
		})
	}
}
// Test_SessionStatus_IsValid_rejects_unknown verifies the contract enum is
// closed: only the five frozen statuses validate.
func Test_SessionStatus_IsValid_rejects_unknown(t *testing.T) {
	// Given
	valid := []SessionStatus{
		SessionRunning, SessionIdle, SessionWaitingInput, SessionError, SessionArchived,
	}

	// When / Then
	for _, s := range valid {
		if !s.IsValid() {
			t.Errorf("status %q should be valid", s)
		}
	}
	if SessionStatus("busy").IsValid() {
		t.Errorf("status %q must not validate: opencode in-memory states are not contract states", "busy")
	}
	if SessionStatus("").IsValid() {
		t.Errorf("empty status must not validate")
	}
}
