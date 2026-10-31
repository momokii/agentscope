/**
 * History view (`#/history`): the fleet over time, from the dashboard-owned
 * store (`GET /api/history`). Two range presets (7d/30d), a cost line and a
 * token line with real time axes, plus a per-project table with sparklines.
 * Everything shown is a recorded sample — the view never interpolates.
 */

import { useEffect, useMemo, useState } from "react";

import type { HistoryProject, HistoryRange, HistoryResponse } from "../lib/history";
import {
  HISTORY_RANGES,
  HistoryUnavailable,
  fetchHistory,
  formatDay,
} from "../lib/history";
import { formatCompact, formatUSD } from "../lib/format";
import DataGuide from "../components/DataGuide";
import Sparkline from "../components/Sparkline";

const RANGE_LABELS: Record<HistoryRange, string> = { "7d": "7d", "30d": "30d" };

/** Decimated SVG line with first/mid/last time labels. */
function TimeChart({
  points,
  pick,
  format,
  label,
  empty,
}: {
  points: readonly { ts: number }[];
  pick: (index: number) => number;
  format: (value: number) => string;
  label: string;
  empty: string;
}) {
  const W = 640;
  const H = 96;
  const PAD = 4;
  const values = useMemo(() => points.map((_, i) => pick(i)), [points, pick]);
  const peak = useMemo(() => values.reduce((m, v) => Math.max(m, v), 0), [values]);
  if (points.length === 0 || peak <= 0) {
    return (
      <div className="empty-state" role="status">
        {empty}
      </div>
    );
  }
  const x = (i: number): number =>
    points.length <= 1 ? W / 2 : PAD + (i / (points.length - 1)) * (W - 2 * PAD);
  const y = (v: number): number => H - 6 - (v / peak) * (H - 14);
  const d = values.map((v, i) => `${i === 0 ? "M" : "L"}${x(i).toFixed(1)},${y(v).toFixed(1)}`).join(" ");
  const first = points[0];
  const last = points[points.length - 1];
  if (first === undefined || last === undefined) return null;
  const mid = points[Math.floor(points.length / 2)];
  const caption = mid === undefined ? "" : `${formatDay(first.ts)} → ${formatDay(last.ts)}`;
  return (
    <figure className="history-chart" role="img" aria-label={`${label}, ${caption}, peak ${format(peak)}`}>
      <figcaption className="history-caption mono">
        <span>{label}</span>
        <span className="history-peak">peak {format(peak)}</span>
      </figcaption>
      <svg className="rate-svg" viewBox={`0 0 ${W} ${H}`} preserveAspectRatio="none" aria-hidden="true">
        <path className="rate-area" d={`${d} L${W},${H} L0,${H} Z`} />
        <path className="rate-line" d={d} fill="none" />
        {values.map((v, i) => (
          <circle key={i} className="rate-dot" cx={x(i)} cy={y(v)} r={2}>
            <title>{`${formatDay(points[i]?.ts ?? 0)}: ${format(v)}`}</title>
          </circle>
        ))}
      </svg>
      <div className="rate-axis mono" aria-hidden="true">
        <span>{formatDay(first.ts)}</span>
        {mid !== undefined && mid !== first && mid !== last && <span>{formatDay(mid.ts)}</span>}
        <span>latest</span>
      </div>
    </figure>
  );
}

export default function HistoryView() {
  const [range, setRange] = useState<HistoryRange>("7d");
  const [data, setData] = useState<HistoryResponse | null>(null);
  const [phase, setPhase] = useState<"loading" | "ready" | "unavailable" | "error">("loading");

  useEffect(() => {
    let active = true;
    setPhase("loading");
    setData(null);
    void (async () => {
      try {
        const next = await fetchHistory(range);
        if (!active) return;
        setData(next);
        setPhase("ready");
      } catch (error) {
        if (!active) return;
        setPhase(error instanceof HistoryUnavailable ? "unavailable" : "error");
      }
    })();
    return () => {
      active = false;
    };
  }, [range]);

  return (
    <div className="history">
      <div className="fleet-toolbar">
        <span className="fleet-title">
          History <span className="count">trailing windows, recorded hourly</span>
        </span>
        <div className="filter-bar" role="group" aria-label="History range">
          {HISTORY_RANGES.map((option) => {
            const selected = range === option;
            return (
              <button
                key={option}
                type="button"
                className={selected ? "filter-chip filter-chip-on" : "filter-chip"}
                aria-pressed={selected}
                onClick={() => setRange(option)}
              >
                {selected && <span aria-hidden="true">✓</span>}
                <span>{RANGE_LABELS[option]}</span>
              </button>
            );
          })}
        </div>
        <DataGuide
          blurb="What the fleet spent and burned, over time — recorded by the dashboard itself, not queried live."
          entries={[
            {
              term: "Source",
              definition:
                "The collector writes one fleet + per-directory sample per metrics refresh into a dashboard-owned SQLite file (never agent data). Charts render recorded samples only; gaps mean no samples, never interpolation.",
            },
            {
              term: "Lines",
              definition:
                "Trailing-24h cost and tokens re-measured each sample: a rising line means the last day got more expensive, not that today did. Peak labels name the maximum.",
            },
            {
              term: "Projects",
              definition:
                "Top directories by trailing-24h cost in range, with per-directory 24h/7d/30d totals and a cost sparkline. Sessions/running are the latest snapshot values.",
            },
          ]}
        />
      </div>

      {phase === "loading" && <div className="empty-state">Loading history…</div>}
      {phase === "unavailable" && (
        <div className="empty-state">
          History is unavailable (the store is disabled on this host). Live fleet data above is unaffected.
        </div>
      )}
      {phase === "error" && (
        <div className="empty-state">History failed to load. The next poll retries automatically.</div>
      )}
      {phase === "ready" && data !== null && (
        <>
          <div className="history-charts">
            <TimeChart
              points={data.points}
              pick={(i) => data.points[i]?.cost_24h ?? 0}
              format={formatUSD}
              label="Fleet cost (trailing 24h)"
              empty="No cost samples in range yet — the collector records one per metrics refresh."
            />
            <TimeChart
              points={data.points}
              pick={(i) => data.points[i]?.tokens_24h ?? 0}
              format={formatCompact}
              label="Fleet tokens (trailing 24h)"
              empty="No token samples in range yet."
            />
          </div>
          <ProjectTable projects={data.projects} />
        </>
      )}
    </div>
  );
}

function ProjectTable({ projects }: { projects: readonly HistoryProject[] }) {
  if (projects.length === 0) {
    return <div className="empty-state">No project activity in range.</div>;
  }
  return (
    <div className="history-projects">
      <div className="fleet-title">
        Top projects <span className="count">({projects.length})</span>
      </div>
      <table className="fleet-table">
        <thead>
          <tr>
            <th scope="col">Directory</th>
            <th scope="col">Sessions</th>
            <th scope="col">Running</th>
            <th scope="col">Cost 24h</th>
            <th scope="col">Cost 7d</th>
            <th scope="col">Cost 30d</th>
            <th scope="col">Trend</th>
          </tr>
        </thead>
        <tbody>
          {projects.map((p) => (
            <tr key={p.directory} className="fleet-row">
              <td className="mono project-path" title={p.directory}>
                {p.directory}
              </td>
              <td className="mono">{p.sessions}</td>
              <td className="mono">{p.running}</td>
              <td className="mono">{formatUSD(p.cost_24h)}</td>
              <td className="mono">{formatUSD(p.cost_7d)}</td>
              <td className="mono">{formatUSD(p.cost_30d)}</td>
              <td>
                <Sparkline data={p.spark.map((s) => s.cost_24h)} />
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
