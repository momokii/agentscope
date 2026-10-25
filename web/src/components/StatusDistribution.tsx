import { statusBadgeSpec } from "./StatusBadge";
import type { SessionStatus } from "../lib/sse";

const SEGMENT_ORDER: readonly SessionStatus[] = [
  "running",
  "waiting-input",
  "idle",
  "error",
  "archived",
];

const SEGMENT_CLASS: Record<SessionStatus, string> = {
  running: "dist-seg-run",
  "waiting-input": "dist-seg-warn",
  idle: "dist-seg-ok",
  error: "dist-seg-err",
  archived: "dist-seg-mute",
};

export interface StatusDistributionProps {
  /** Per-status counts over the FULL snapshot (precomputed once per tick). */
  counts: Record<SessionStatus, number>;
  total: number;
}

export default function StatusDistribution({
  counts,
  total,
}: StatusDistributionProps) {
  const summary =
    total === 0
      ? "no sessions"
      : SEGMENT_ORDER.map(
          (status) =>
            `${statusBadgeSpec(status).label} ${counts[status]}`,
        ).join(", ");
  return (
    <section className="dist" aria-label="Session status distribution">
      {total === 0 ? (
        <div className="dist-empty">No sessions in this fleet.</div>
      ) : (
        <>
          <div
            className="dist-track"
            role="img"
            aria-label={`Status distribution across ${total} sessions: ${summary}`}
          >
            {SEGMENT_ORDER.map((status) => {
              const count = counts[status];
              const pct = total === 0 ? 0 : (count / total) * 100;
              return (
                <div
                  key={status}
                  className={`dist-seg ${SEGMENT_CLASS[status]}`}
                  style={{ width: `${pct}%` }}
                  title={`${statusBadgeSpec(status).label}: ${count}`}
                  aria-hidden="true"
                />
              );
            })}
          </div>
          <ul className="dist-legend">
            {SEGMENT_ORDER.map((status) => {
              const spec = statusBadgeSpec(status);
              return (
                <li key={status} className="dist-legend-item" data-status={status}>
                  <span className="dist-glyph" aria-hidden="true">{spec.glyph}</span>
                  <span>{spec.label}</span>
                  <span className="count mono">{counts[status]}</span>
                </li>
              );
            })}
          </ul>
        </>
      )}
    </section>
  );
}
