/**
 * Event ticker (W2-2, enriched Wave 5+): bottom strip rendering the activity
 * ring with source context and paging.
 *
 * - One row per event: seq stamp + glyph + text + source (session title ·
 *   project · harness glyph). The session id links to its drill-down route.
 * - Newest-first ordering means appends never move existing rows: no
 *   scroll-yank by construction (no auto-scroll, no reordering).
 * - Pages slice the ring at ACTIVITY_VISIBLE_CAP rows: Prev/Next step
 *   through history instead of dumping it.
 * - `paused` (LIVE off): freezes the list — new events are NOT appended
 *   while paused, and a ‖ PAUSED chip is shown (DESIGN section 6).
 */

import { useEffect, useState } from "react";
import { Activity } from "lucide-react";

import type { ActivityEvent } from "../lib/activity";
import { ACTIVITY_VISIBLE_CAP } from "../lib/activity";
import ActivityRow from "./ActivityRow";

export interface EventTickerProps {
  events: readonly ActivityEvent[];
  /** True when LIVE is off: freeze appends, show PAUSED chip. */
  paused: boolean;
  /** View scope label ("Fleet" default): project name or session title. */
  scopeLabel?: string;
}

/** localStorage key for the collapsed preference (view-independent). */
const COLLAPSE_KEY = "ocd.tickerCollapsed";

export default function EventTicker({
  events,
  paused,
  scopeLabel = "Fleet",
}: EventTickerProps) {
  const [page, setPage] = useState(0);
  const [collapsed, setCollapsed] = useState<boolean>(() => {
    try {
      return window.localStorage.getItem(COLLAPSE_KEY) === "1";
    } catch {
      return false;
    }
  });
  const toggleCollapsed = () => {
    setCollapsed((v) => {
      const next = !v;
      try {
        window.localStorage.setItem(COLLAPSE_KEY, next ? "1" : "0");
      } catch {
        // Private mode etc: collapse still works for this view.
      }
      return next;
    });
  };
  const pageCount = Math.max(1, Math.ceil(events.length / ACTIVITY_VISIBLE_CAP));
  const safePage = Math.min(page, pageCount - 1);
  useEffect(() => {
    setPage(0);
  }, [paused]);
  const start = safePage * ACTIVITY_VISIBLE_CAP;
  const visible = events.slice(start, start + ACTIVITY_VISIBLE_CAP);

  return (
    <section
      className="ticker"
      aria-label={`${scopeLabel} activity ticker`}
      data-paused={paused ? "true" : "false"}
    >
      <div className="ticker-head">
        <button
          type="button"
          className="ticker-toggle"
          aria-expanded={!collapsed}
          aria-label={collapsed ? "Show activity" : "Hide activity"}
          title={collapsed ? "Show the activity feed" : "Hide the activity feed"}
          onClick={toggleCollapsed}
        >
          <Activity aria-hidden="true" size={12} />
          <span className="ticker-title">Activity</span>
          <span aria-hidden="true">{collapsed ? "▸" : "▾"}</span>
        </button>
        {scopeLabel !== "Fleet" && (
          <span className="ticker-scope mono" title={`Scoped to ${scopeLabel}`}>
            {scopeLabel}
          </span>
        )}
        {paused && (
          <span className="state-chip state-chip-paused">
            <span aria-hidden="true">‖</span>
            <span>Paused</span>
          </span>
        )}
        <span className="ticker-count mono">{events.length}</span>
        {pageCount > 1 && (
          <span className="ticker-pages mono" aria-live="polite">
            <button
              type="button"
              className="ticker-page-btn"
              disabled={safePage === 0}
              aria-label="Newer activity page"
              onClick={() => setPage(safePage - 1)}
            >
              ‹
            </button>
            <span>
              {safePage + 1}/{pageCount}
            </span>
            <button
              type="button"
              className="ticker-page-btn"
              disabled={safePage >= pageCount - 1}
              aria-label="Older activity page"
              onClick={() => setPage(safePage + 1)}
            >
              ›
            </button>
          </span>
        )}
      </div>
      {collapsed ? null : visible.length === 0 ? (
        <div className="ticker-empty">
          No activity yet — transitions appear as snapshots tick.
        </div>
      ) : (
        <ul className="ticker-list">
          {visible.map((event, index) => (
            <li
              key={`${event.seq}-${event.sessionId}-${event.kind}-${index}`}
              className="ticker-row mono"
            >
              <ActivityRow event={event} />
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}
