/**
 * DataGuide: "how to read this view" for new operators. A (?) toggle
 * revealing a blurb plus a glossary. Fleet/Projects use the core fleet
 * glossary (statuses, harnesses, KPIs, recency, sparks, live pane); every
 * other view passes its own entries describing exactly what that screen
 * shows and how to read it. Plain <dl> markup, no new tokens, no new
 * hues — glyph + label carry every meaning.
 */

import { useState } from "react";
import { CircleHelp } from "lucide-react";

export interface GuideEntry {
  term: string;
  definition: string;
}

export interface DataGuideProps {
  /** Optional one-liner stating the current view's purpose, shown above
   * the glossary. */
  blurb?: string;
  /** View-specific glossary. Defaults to the core fleet glossary (used by
   * the Projects landing and the fleet table). */
  entries?: readonly GuideEntry[];
}

/** Core fleet glossary: the shared language of badges, KPIs, buckets. */
export const FLEET_GUIDE: readonly GuideEntry[] = [
  {
    term: "Status",
    definition:
      "● running = generating now · ▲ needs input = heuristic (user spoke last, nothing in flight — there is no pending flag) · ◌ idle = finished · ✕ error = newest assistant turn failed · ▪ archived. Only sessions active in the last ~2m may show running / needs input / error; older rows read idle.",
  },
  {
    term: "Harness",
    definition:
      "OC = opencode (database) · π = pi (session files). Both merge in one fleet; the filter slices by owner.",
  },
  {
    term: "KPIs",
    definition:
      "Active / Waiting / Errors count the current date, bucket, harness, and search selection — not the status chips (those slice further). Tokens/hr = message tokens in the last 60s × 60 (both harnesses). Cost today = message spend since local midnight. Cost total = lifetime, subagents not double-counted. Live procs = observed agent processes; live panes = observed panes running an agent (plain shells excluded, so the KPI matches the project columns). Metrics age = sample age + time since this frame arrived; −/— = not yet measured. Refresh re-pulls the snapshot on demand (works paused).",
  },
  {
    term: "Recency",
    definition:
      "Idle splits into Recent (≤24h) / Stale (1–7d) / Old (>7d). Date presets filter by last activity; custom from wins.",
  },
  {
    term: "Sparks & rate",
    definition:
      "Row sparks = tokens/min per poll tick (newest last). Project and fleet charts sum member rows; 1 sample ≈ 1 poll tick (default 10s). Hover any dot for its value.",
  },
  {
    term: "Live pane",
    definition:
      "On-demand terminal text for the session holding the pane. DB-only 409 = no live pane (or the pane belongs to the named session); 504 = dump timed out; 503 = backend unavailable.",
  },
];

export default function DataGuide({
  blurb,
  entries = FLEET_GUIDE,
}: DataGuideProps) {
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
            {entries.map((entry) => (
              <div key={entry.term}>
                <dt>{entry.term}</dt>
                <dd>{entry.definition}</dd>
              </div>
            ))}
          </dl>
        </section>
      )}
    </div>
  );
}
