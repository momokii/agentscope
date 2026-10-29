/**
 * DataGuide (Wave 5+): "how to read this dashboard" for new operators.
 * A (?) toggle revealing a glossary: statuses, harnesses, KPIs, recency,
 * sparks/rate charts, and Live-pane states. Plain <dl> markup, no new
 * tokens, no new hues — glyph + label carry every meaning.
 */

import { useState } from "react";
import { CircleHelp } from "lucide-react";

export interface DataGuideProps {
  /** Optional one-liner stating the current view's purpose, shown above
   * the glossary (used by Compare/Timeline/Activity/Session views). */
  blurb?: string;
}

export default function DataGuide({ blurb }: DataGuideProps) {
  const [open, setOpen] = useState(false);
  return (
    <div className="data-guide">
      <button
        type="button"
        className={open ? "guide-toggle guide-toggle-on" : "guide-toggle"}
        aria-expanded={open}
        aria-label={open ? "Hide data guide" : "How to read this dashboard"}
        title="How to read this dashboard"
        onClick={() => setOpen((v) => !v)}
      >
        <CircleHelp aria-hidden="true" size={12} />
        <span>Guide</span>
      </button>
      {open && (
        <section className="guide-panel" aria-label="Data guide">
          {blurb !== undefined && blurb !== "" && (
            <p className="guide-blurb-text">{blurb}</p>
          )}
          <dl className="guide-facts mono">
            <div>
              <dt>Status</dt>
              <dd>
                ● running = generating now · ▲ needs input = heuristic (user
                spoke last, nothing in flight — there is no pending flag) · ◌
                idle = finished · ✕ error = newest assistant turn failed · ▪
                archived. Only sessions active in the last ~2m may show
                running / needs input / error; older rows read idle.
              </dd>
            </div>
            <div>
              <dt>Harness</dt>
              <dd>
                OC = opencode (database) · π = pi (session files). Both merge
                in one fleet; the filter slices by owner.
              </dd>
            </div>
            <div>
              <dt>KPIs</dt>
              <dd>
                Active / Waiting / Errors count the current date,
                bucket, harness, and search selection — not the status
                chips (those slice further). Tokens/hr = message tokens in
                the last 60s × 60 (both harnesses). Cost today = message
                spend since local midnight. Cost total = lifetime,
                subagents not double-counted. Live procs = observed agent
                processes; live panes = observed panes running an agent
                (plain shells excluded, so the KPI matches the project
                columns). Metrics age = sample age + time since this
                frame arrived; −/— = not yet measured. Refresh re-pulls
                the snapshot on demand (works paused).
              </dd>
            </div>
            <div>
              <dt>Recency</dt>
              <dd>
                Idle splits into Recent (≤24h) / Stale (1–7d) / Old (&gt;7d).
                Date presets filter by last activity; custom from wins.
              </dd>
            </div>
            <div>
              <dt>Sparks &amp; rate</dt>
              <dd>
                Row sparks = tokens/min per poll tick (newest last). Project
                and fleet charts sum member rows; 1 sample ≈ 1 poll tick
                (default 10s). Hover any dot for its value.
              </dd>
            </div>
            <div>
              <dt>Live pane</dt>
              <dd>
                On-demand terminal text for the session holding the pane.
                DB-only 409 = no live pane (or the pane belongs to the named
                session); 504 = dump timed out; 503 = backend unavailable.
              </dd>
            </div>
          </dl>
        </section>
      )}
    </div>
  );
}
