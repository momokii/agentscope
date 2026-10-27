/**
 * ProjectInsight (Wave 5 enrichment): per-project detail below the header.
 * Status distribution bar (Running / Needs input / Idle with counts —
 * glyph + label + count, never color alone) plus the aggregate token-rate
 * chart (element-wise spark sums, real snapshot data) and the harness
 * split. All numbers reconcile with the fleet rows by construction.
 */

import { countByHarness } from "../lib/fleetSort";
import { sumSparks } from "../lib/rate";
import type { SessionRow } from "../lib/sse";
import RateChart from "./RateChart";

export interface ProjectInsightProps {
  /** Member sessions of this project (unfiltered by status/bucket). */
  rows: readonly SessionRow[];
}

export default function ProjectInsight({ rows }: ProjectInsightProps) {
  const total = rows.length;
  let running = 0;
  let waiting = 0;
  for (const row of rows) {
    if (row.status === "running") running++;
    else if (row.status === "waiting-input") waiting++;
  }
  const idle = total - running - waiting;
  const harness = countByHarness(rows);
  const pct = (n: number): string =>
    total === 0 ? "0%" : `${((n / total) * 100).toFixed(1)}%`;
  return (
    <section
      className="project-insight"
      aria-label={`Project detail: ${total} sessions`}
    >
      <div
        className="dist-bar"
        role="img"
        aria-label={`Status mix: ${running} running, ${waiting} needs input, ${idle} idle`}
      >
        <div
          className="dist-seg-run"
          style={{ width: pct(running) }}
          title={`${running} running`}
        />
        <div
          className="dist-seg-wait"
          style={{ width: pct(waiting) }}
          title={`${waiting} needs input`}
        />
        <div className="dist-seg-idle" style={{ width: pct(idle) }} title={`${idle} idle`} />
      </div>
      <div className="dist-legend mono">
        <span>
          ● Running <span className="count">{running}</span>
        </span>
        <span>
          ▲ Needs input <span className="count">{waiting}</span>
        </span>
        <span>
          ◌ Idle <span className="count">{idle}</span>
        </span>
        <span>
          OC opencode <span className="count">{harness.opencode}</span>
        </span>
        <span>
          π pi <span className="count">{harness.pi}</span>
        </span>
      </div>
      <RateChart values={sumSparks(rows)} label="Project" />
    </section>
  );
}
