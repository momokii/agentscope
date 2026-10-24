/**
 * Compare view (W3-3): 2–4 sessions side by side over Snapshot data only.
 *
 * - Session picker REUSES `filterCmdSessions` from CmdBar (imported, not
 *   duplicated); the picker itself is local because CmdBar is an overlay
 *   jump bar while this is an inline multi-toggle capped at 4.
 * - KPI cards per session: tokens, cost, last active, subagents. Tool-call
 *   counts and per-turn durations are NOT in the Snapshot shape — they
 *   come from W3-2 detail views, never from these aggregates.
 * - Overlaid cost + token lines share one time base (see `overlaySeries`):
 *   two stacked SVG panels, hand-rolled (no chart lib — spark histories
 *   are tiny, SVG keeps the bundle small and animation-free). Session hues
 *   map to fixed tokens in order: --accent, --ink-100, --ok, --warn.
 * - Click a chart point → `onPointSelect(id, t)`: Wave-3-drilldown HOOK
 *   stub (currently logs/selects via the session hash route; the full
 *   span inspector is W3-2's right-rail, not this view).
 */

import { useMemo, useState } from "react";

import type { SessionRow } from "../lib/sse";
import { filterCmdSessions } from "../components/CmdBar";
import { formatCompact, formatUSD } from "../lib/format";
import { overlaySeries } from "../lib/compare";
import RelativeTime from "../components/RelativeTime";
import StatusBadge from "../components/StatusBadge";

export interface CompareProps {
  sessions: readonly SessionRow[];
  /** Selected ids (hash-serializable; App owns the hash write). */
  ids: string[];
  onChangeIds: (ids: string[]) => void;
  /** Card/lane click → session hash route (drill-down contract). */
  onSelect: (id: string) => void;
  /** Chart-point hook stub: Wave-3-drilldown entry, not full drill-down. */
  onPointSelect?: (id: string, t: number) => void;
  nowMs?: number;
}

export const MAX_COMPARE = 4;

/**
 * Fixed session-hue mapping (documented, max 4, all DESIGN tokens —
 * no legends-as-rainbows). Index = position in the selected list.
 */
export const SESSION_HUES = [
  "var(--accent)",
  "var(--ink-100)",
  "var(--ok)",
  "var(--warn)",
] as const;

const CHART_W = 560;
const CHART_H = 96;

function linePath(
  values: number[],
  max: number,
  width: number,
  height: number,
): string {
  if (values.length === 0) return "";
  if (values.length === 1) {
    const y = height - (values[0]! / Math.max(1, max)) * (height - 8) - 4;
    return `M 0,${y.toFixed(1)} L ${width},${y.toFixed(1)}`;
  }
  return values
    .map((v, i) => {
      const x = (i / (values.length - 1)) * width;
      const y = height - 4 - (v / Math.max(1e-9, max)) * (height - 8);
      return `${i === 0 ? "M" : "L"} ${x.toFixed(1)},${y.toFixed(1)}`;
    })
    .join(" ");
}

export default function Compare({
  sessions,
  ids,
  onChangeIds,
  onSelect,
  onPointSelect,
  nowMs,
}: CompareProps) {
  const [query, setQuery] = useState("");
  const byId = useMemo(() => new Map(sessions.map((r) => [r.id, r])), [sessions]);
  const selected = useMemo(
    () => ids.map((id) => byId.get(id)).filter((r): r is SessionRow => r !== undefined),
    [ids, byId],
  );
  const lines = useMemo(() => overlaySeries(selected), [selected]);
  const hits = useMemo(
    () => filterCmdSessions(sessions, query),
    [sessions, query],
  );
  const maxCost = Math.max(1e-9, ...lines.flatMap((l) => l.points.map((p) => p.cost)));
  const maxTokens = Math.max(1e-9, ...lines.flatMap((l) => l.points.map((p) => p.tokens)));

  const toggle = (id: string) => {
    if (ids.includes(id)) onChangeIds(ids.filter((x) => x !== id));
    else if (ids.length < MAX_COMPARE) onChangeIds([...ids, id]);
  };

  const capped = ids.length >= MAX_COMPARE;

  return (
    <div className="compare">
      <div className="compare-head">
        <span className="compare-title">
          Compare <span className="count">({selected.length}/{MAX_COMPARE})</span>
        </span>
      </div>

      {sessions.length === 0 ? (
        <div className="empty-state">No sessions in this fleet to compare.</div>
      ) : (
        <>
          <div className="compare-picker">
            <input
              type="search"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder="Pick sessions — id, title, cwd, model…"
              aria-label="Pick sessions to compare"
            />
            <div className="compare-picks mono">
              {selected.length === 0 && (
                <span className="compare-hint">
                  Select at least 2 sessions to compare (up to {MAX_COMPARE}).
                </span>
              )}
              {selected.map((row, i) => (
                <button
                  key={row.id}
                  type="button"
                  className="compare-chip"
                  aria-label={`Remove ${row.id} from comparison`}
                  onClick={() => toggle(row.id)}
                >
                  <span
                    className="compare-swatch"
                    aria-hidden="true"
                    style={{ background: SESSION_HUES[i % SESSION_HUES.length] }}
                  />
                  <span>{row.id}</span>
                  <span aria-hidden="true">×</span>
                </button>
              ))}
            </div>
            <ul className="compare-hits" aria-label="Sessions">
              {hits.map((row) => {
                const on = ids.includes(row.id);
                const disabled = !on && capped;
                return (
                  <li key={row.id}>
                    <button
                      type="button"
                      className={on ? "compare-hit compare-hit-on" : "compare-hit"}
                      aria-pressed={on}
                      disabled={disabled}
                      onClick={() => toggle(row.id)}
                    >
                      <StatusBadge status={row.status} />
                      <span className="mono">{row.id}</span>
                      <span className="compare-hit-title">{row.title}</span>
                    </button>
                  </li>
                );
              })}
            </ul>
            {capped && (
              <div className="compare-hint mono">
                Comparison capped at {MAX_COMPARE} — remove one to add another.
              </div>
            )}
          </div>

          {selected.length < 2 ? (
            <div className="empty-state">
              {selected.length === 0
                ? "Select at least 2 sessions to compare — not a broken chart."
                : "One session selected — add one more to draw the overlay."}
            </div>
          ) : (
            <>
              <div className="compare-cards">
                {selected.map((row, i) => (
                  <button
                    key={row.id}
                    type="button"
                    className="compare-card"
                    onClick={() => onSelect(row.id)}
                    aria-label={`Open session ${row.title}`}
                  >
                    <span
                      className="compare-card-strip"
                      aria-hidden="true"
                      style={{ background: SESSION_HUES[i % SESSION_HUES.length] }}
                    />
                    <span className="compare-card-title">{row.title}</span>
                    <span className="compare-card-id mono">{row.id}</span>
                    <dl className="compare-kpis mono">
                      <div>
                        <dt>tokens</dt>
                        <dd>{formatCompact(row.tokens.total)}</dd>
                      </div>
                      <div>
                        <dt>cost</dt>
                        <dd>{formatUSD(row.cost)}</dd>
                      </div>
                      <div>
                        <dt>active</dt>
                        <dd>
                          <RelativeTime
                            updatedRelativeMs={row.updated_relative_ms}
                            nowMs={nowMs}
                          />
                        </dd>
                      </div>
                      <div>
                        <dt>subagents</dt>
                        <dd>{row.subagent_count}</dd>
                      </div>
                    </dl>
                  </button>
                ))}
              </div>

              <div className="compare-charts">
                {(
                  [
                    { key: "cost", label: "Cumulative cost", format: formatUSD, max: maxCost, pick: (t: number, li: number) => lines[li]!.points[t]!.cost },
                    { key: "tokens", label: "Cumulative tokens", format: (v: number) => formatCompact(v), max: maxTokens, pick: (t: number, li: number) => lines[li]!.points[t]!.tokens },
                  ] as const
                ).map((panel) => (
                  <section key={panel.key} className="compare-panel" aria-label={panel.label}>
                    <div className="compare-panel-head mono">
                      <span>{panel.label}</span>
                      <span className="compare-panel-max">{panel.format(panel.max)}</span>
                    </div>
                    <svg
                      className="compare-chart"
                      viewBox={`0 0 ${CHART_W} ${CHART_H}`}
                      role="img"
                      aria-label={`${panel.label} overlay, ${lines.length} sessions`}
                    >
                      {lines.map((line, li) => {
                        const values = line.points.map((p) =>
                          panel.key === "cost" ? p.cost : p.tokens,
                        );
                        return (
                          <g key={line.id}>
                            <polyline
                              points={linePath(values, panel.max, CHART_W, CHART_H)
                                .replaceAll("M ", "")
                                .replaceAll("L ", "")
                                .trim()}
                              style={{
                                fill: "none",
                                stroke: SESSION_HUES[li % SESSION_HUES.length],
                                strokeWidth: 1.5,
                              }}
                            />
                            {line.points.map((pt, t) => {
                              const n = line.points.length;
                              const cx = n === 1 ? CHART_W / 2 : (t / (n - 1)) * CHART_W;
                              const cy =
                                CHART_H - 4 - (panel.pick(t, li) / panel.max) * (CHART_H - 8);
                              return (
                                <circle
                                  key={t}
                                  cx={cx}
                                  cy={cy}
                                  r={4}
                                  style={{
                                    fill: "transparent",
                                    stroke: "none",
                                    cursor: onPointSelect ? "pointer" : "default",
                                  }}
                                  onClick={
                                    onPointSelect
                                      ? (e) => {
                                          e.stopPropagation();
                                          onPointSelect(line.id, pt.t);
                                        }
                                      : undefined
                                  }
                                >
                                  <title>{`${line.id} · bucket ${pt.t}`}</title>
                                </circle>
                              );
                            })}
                          </g>
                        );
                      })}
                    </svg>
                    <div className="compare-legend mono">
                      {lines.map((line, li) => (
                        <span key={line.id} className="compare-legend-item">
                          <span
                            className="compare-swatch"
                            aria-hidden="true"
                            style={{ background: SESSION_HUES[li % SESSION_HUES.length] }}
                          />
                          <span>{line.id}</span>
                        </span>
                      ))}
                      <span className="compare-axis">oldest → latest tick</span>
                    </div>
                  </section>
                ))}
              </div>
            </>
          )}
        </>
      )}
    </div>
  );
}
