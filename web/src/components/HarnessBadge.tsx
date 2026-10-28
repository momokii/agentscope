/**
 * Harness badge (Wave 5): which agent runtime owns the session.
 * [glyph] LABEL on a tonal wash — glyph + label always accompany color,
 * per DESIGN section 6 (never color alone).
 *
 * - opencode: "OC" glyph on the neutral mute wash (the fleet default).
 * - pi: "π" glyph on the accent wash (the second harness, visually distinct
 *   but quiet — the glyph + label carry the meaning, not the hue).
 */

import type { Harness } from "../lib/sse";

interface HarnessSpec {
  glyph: string;
  label: string;
  className: string;
  /** How to read this owner (hover tooltip). */
  title: string;
}

const SPECS: Record<Harness, HarnessSpec> = {
  opencode: {
    glyph: "OC",
    label: "OPENCODE",
    className: "badge-harness-oc",
    title: "Owned by opencode (tracked in opencode.db)",
  },
  pi: {
    glyph: "π",
    label: "PI",
    className: "badge-harness-pi",
    title: "Owned by pi (tracked in JSONL session files)",
  },
};

export function harnessBadgeSpec(harness: Harness): HarnessSpec {
  return SPECS[harness];
}

export default function HarnessBadge({ harness }: { harness: Harness }) {
  const spec = harnessBadgeSpec(harness);
  return (
    <span
      className={`badge ${spec.className}`}
      aria-label={`harness ${spec.label}`}
      title={spec.title}
    >
      <span className="badge-glyph" aria-hidden="true">
        {spec.glyph}
      </span>
      <span>{spec.label}</span>
    </span>
  );
}
