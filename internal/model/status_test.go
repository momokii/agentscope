package model

import "testing"

// Test_DeriveStatus_* — table-driven contract tests for the pure
// status-derivation function. One scenario per row; the run proves the
// precedence chain (archived > error > running > waiting-input > idle).

// Test_DeriveStatus_precedence verifies each status wins at its precedence
// level, including combined-signal conflicts.
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
			signals: StatusSignals{LatestRole: "assistant"},
			want:    SessionIdle,
		},
		{
			name:    "running when assistant turn in flight",
			signals: StatusSignals{InFlightAssistant: true, LatestRole: "assistant"},
			want:    SessionRunning,
		},
		{
			name:    "running when tool part running",
			signals: StatusSignals{RunningToolParts: 2, LatestRole: "assistant"},
			want:    SessionRunning,
		},
		{
			name:    "running when fresh events observed",
			signals: StatusSignals{HasFreshEvents: true, LatestRole: "assistant"},
			want:    SessionRunning,
		},
		{
			name:    "waiting-input heuristic when user spoke last",
			signals: StatusSignals{LatestRole: "user"},
			want:    SessionWaitingInput,
		},
		{
			name:    "waiting-input heuristic when compacting",
			signals: StatusSignals{Compacting: true, LatestRole: "assistant"},
			want:    SessionWaitingInput,
		},
		{
			name:    "error when terminal error observed",
			signals: StatusSignals{HasError: true, LatestRole: "assistant"},
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
			name: "error beats running",
			signals: StatusSignals{
				HasError: true, InFlightAssistant: true, RunningToolParts: 1,
			},
			want: SessionError,
		},
		{
			name: "running beats waiting-input heuristic",
			signals: StatusSignals{
				HasFreshEvents: true, LatestRole: "user",
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
