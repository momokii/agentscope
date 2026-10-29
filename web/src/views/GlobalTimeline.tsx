/**
 * GlobalTimeline view (W3-3): all sessions as swimlanes on one time axis.
 *
 * - One lane per session (label + StatusBadge glyph, never color alone);
 *   message dots + tool-call bars derive from the row spark buckets
 *   (width ∝ bucket share of the session max — Snapshot has no per-tool
 *   durations, see `swimlanes`), with a hand-rolled SVG token-rate area
 *   underlay (no chart lib: same justification as Compare — tiny data,
 *   zero animation, full token control).
 * - Subagent lanes nest under parents as count-derived placeholders.
 * - Click a lane or dot → `onSelect(id)` → existing `#/sessions/:id`
 *   hash route (drill-down contract; full span inspector is W3-2).
 * - Drag-zoom: NO-GO (fixed full-range). Without real per-message
 *   timestamps a viewBox zoom would only magnify bucket indices — real
 *   zoom wants W3-2's detail timestamps. Recorded as follow-up.
 */

import { useMemo } from "react";

import type { SessionRow } from "../lib/sse";
import { swimlanes } from "../lib/compare";
import type { SwimLane } from "../lib/compare";
import RelativeTime from "../components/RelativeTime";
import DataGuide from "../components/DataGuide";
import StatusBadge from "../components/StatusBadge";
import { statusBadgeSpec } from "../components/StatusBadge";

export interface GlobalTimelineProps {
  sessions: readonly SessionRow[];
  selectedId: string | null;
  /** Lane/dot click → session hash route (existing drill-down). */
  onSelect: (id: string) => void;
  nowMs?: number;
}

const LANE_W = 480;
const LANE_H = 28;

function ratePath(rate: number[], width: number, height: number): string {
  if (rate.length === 0) return "";
  const max = Math.max(...rate, 1e-9);
  const pts = rate.map((v, i) => {
    const x =
      rate.length === 1 ? width / 2 : (i / (rate.length - 1)) * width;
    const y = height - 2 - (v / max) * (height - 6);
    return `${x.toFixed(1)},${y.toFixed(1)}`;
  });
  return `M 0,${height} L ${pts.join(" L ")} L ${width},${height} Z`;
}

function LaneTrack({ lane, width }: { lane: SwimLane; width: number }) {
  const n = Math.max(1, lane.tokenRate.length);
  const x = (t: number) => (n === 1 ? width / 2 : (t / (n - 1)) * width);
  return (
    <svg
      className="timeline-track"
      viewBox={`0 0 ${width} ${LANE_H}`}
      preserveAspectRatio="none"
      role="img"
      aria-label={`Activity for ${lane.id}`}
    >
      {lane.tokenRate.length > 0 && (
        <path
          d={ratePath(lane.tokenRate, width, LANE_H)}
          style={{ fill: "var(--ink-700)", opacity: 0.6 }}
        />
      )}
      {lane.bars.map((bar) => (
        <rect
          key={`b${bar.t}`}
          x={x(bar.t) - 1}
          y={LANE_H - 12 - bar.width * 10}
          width={2 + bar.width * 8}
          height={4 + bar.width * 10}
          style={{ fill: "var(--accent)", opacity: 0.55 }}
        />
      ))}
      {lane.dots.map((dot) => (
        <circle
          key={`d${dot.t}`}
          cx={x(dot.t)}
          cy={6}
          r={2.5}
          style={{ fill: "var(--ink-100)" }}
        />
      ))}
    </svg>
  );
}

export default function GlobalTimeline({
  sessions,
  selectedId,
  onSelect,
  nowMs,
}: GlobalTimelineProps) {
  const lanes = useMemo(() => swimlanes(sessions), [sessions]);
  const width = Math.max(
    1,
    ...sessions.map((r) => r.spark.length),
    1,
  );
  const viewW = Math.max(LANE_W, width * 12);

  if (sessions.length === 0) {
    return (
      <div className="timeline">
        <div className="timeline-head">
          <span className="timeline-title">Global timeline</span>
        </div>
        <div className="empty-state">No sessions in this fleet yet.</div>
      </div>
    );
  }

  return (
    <div className="timeline">
      <div className="timeline-head">
        <span className="timeline-title">
          Global timeline <span className="count">({lanes.length})</span>
        </span>
        <span className="timeline-note mono">fixed full-range · zoom follows in W3-2</span>
        <DataGuide
          blurb="Every session as a swimlane on one shared time axis — spot who burned tokens when, then click a lane to drill in."
          entries={[
            {
              term: "Lanes",
              definition:
                "One lane per session in fleet order (running first, exited last, most-recent first). Subagent lanes nest under parents as count-derived placeholders labeled subagent 1..N.",
            },
            {
              term: "Bars & dots",
              definition:
                "Drawn from each row's token-rate spark (tokens/min per tick): dots mark nonzero ticks, bar width is that tick's share of the session max — a duration proxy, since snapshots carry no per-tool durations.",
            },
            {
              term: "Area",
              definition:
                "The gray underlay is the raw token-rate spark itself.",
            },
            {
              term: "Click",
              definition:
                "Lane or dot click navigates to that session's hash route. Range is fixed full-history; zoom follows in a later wave.",
            },
          ]}
        />
      </div>
      <ol className="timeline-lanes">
        {lanes.map((lane) => {
          const spec = statusBadgeSpec(lane.status);
          const isSelected = selectedId === lane.id;
          return (
            <li
              key={lane.id}
              className={isSelected ? "timeline-lane timeline-lane-selected" : "timeline-lane"}
            >
              <button
                type="button"
                className="timeline-lane-main"
                onClick={() => onSelect(lane.id)}
                aria-label={`Open session ${lane.title}`}
                aria-current={isSelected ? "true" : undefined}
              >
                <span className="timeline-label">
                  <span className="badge-glyph" aria-hidden="true">
                    {spec.glyph}
                  </span>
                  <span className="timeline-label-title">{lane.title}</span>
                  <span className="timeline-label-id mono">{lane.id}</span>
                  {lane.exited && (
                    <span className="timeline-exited mono">exited</span>
                  )}
                </span>
                <span className="timeline-badges">
                  <StatusBadge status={lane.status} />
                  <RelativeTime
                    updatedRelativeMs={
                      sessions.find((r) => r.id === lane.id)?.updated_relative_ms ?? 0
                    }
                    nowMs={nowMs}
                  />
                </span>
                {lane.tokenRate.length === 0 ? (
                  <span className="timeline-quiet mono">no activity in window</span>
                ) : (
                  <LaneTrack lane={lane} width={viewW} />
                )}
              </button>
              {lane.children.length > 0 && (
                <ol className="timeline-sub">
                  {lane.children.map((child) => (
                    <li key={child.id} className="timeline-sublane">
                      <span aria-hidden="true">↳</span>
                      <span className="mono">{child.label}</span>
                    </li>
                  ))}
                </ol>
              )}
            </li>
          );
        })}
      </ol>
    </div>
  );
}
