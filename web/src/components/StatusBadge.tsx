/**
 * Compound fleet status badge (DESIGN section 6):
 * [glyph] LABEL on a tonal wash of the state color — color + glyph + label,
 * always all three, never color alone. waiting-input (▲) is shape-distinct
 * from idle (◌) at grayscale.
 */

import type { SessionStatus } from "../lib/sse";

interface BadgeSpec {
  glyph: string;
  label: string;
  className: string;
  /** How to read this state (hover tooltip). */
  title: string;
}

const SPECS: Record<SessionStatus, BadgeSpec> = {
  running: {
    glyph: "●",
    label: "RUNNING",
    className: "badge-run",
    title: "Generating now: a recent assistant turn is still in flight",
  },
  idle: {
    glyph: "◌",
    label: "IDLE",
    className: "badge-ok",
    title: "Last turn completed, nothing in flight",
  },
  "waiting-input": {
    glyph: "▲",
    label: "NEEDS INPUT",
    className: "badge-warn",
    title:
      "Heuristic: user spoke last with nothing in flight — may need you (no pending flag exists, so this is an estimate)",
  },
  error: {
    glyph: "✕",
    label: "ERROR",
    className: "badge-err",
    title: "The newest assistant turn failed (older errors don't stick)",
  },
  archived: {
    glyph: "▪",
    label: "ARCHIVED",
    className: "badge-mute",
    title: "Session archived — terminal state, wins over everything",
  },
};

export function statusBadgeSpec(status: SessionStatus): BadgeSpec {
  return SPECS[status];
}

export default function StatusBadge({ status }: { status: SessionStatus }) {
  const spec = statusBadgeSpec(status);
  return (
    <span
      className={`badge ${spec.className}`}
      aria-label={spec.label}
      title={spec.title}
    >
      <span className="badge-glyph" aria-hidden="true">
        {spec.glyph}
      </span>
      <span>{spec.label}</span>
    </span>
  );
}
