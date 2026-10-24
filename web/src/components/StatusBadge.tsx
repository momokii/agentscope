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
}

const SPECS: Record<SessionStatus, BadgeSpec> = {
  running: { glyph: "●", label: "RUNNING", className: "badge-run" },
  idle: { glyph: "◌", label: "IDLE", className: "badge-ok" },
  "waiting-input": {
    glyph: "▲",
    label: "NEEDS INPUT",
    className: "badge-warn",
  },
  error: { glyph: "✕", label: "ERROR", className: "badge-err" },
  archived: { glyph: "▪", label: "ARCHIVED", className: "badge-mute" },
};

export function statusBadgeSpec(status: SessionStatus): BadgeSpec {
  return SPECS[status];
}

export default function StatusBadge({ status }: { status: SessionStatus }) {
  const spec = statusBadgeSpec(status);
  return (
    <span className={`badge ${spec.className}`} aria-label={spec.label}>
      <span className="badge-glyph" aria-hidden="true">
        {spec.glyph}
      </span>
      <span>{spec.label}</span>
    </span>
  );
}
