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
import { buildSessionHash } from "../lib/routing";

export interface EventTickerProps {
  events: readonly ActivityEvent[];
  /** True when LIVE is off: freeze appends, show PAUSED chip. */
  paused: boolean;
}

function harnessGlyph(harness: ActivityEvent["harness"]): string {
  return harness === "pi" ? "π" : "OC";
}

export default function EventTicker({ events, paused }: EventTickerProps) {
  const [page, setPage] = useState(0);
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
      aria-label="Fleet activity ticker"
      data-paused={paused ? "true" : "false"}
    >
      <div className="ticker-head">
        <Activity aria-hidden="true" size={12} />
        <span className="ticker-title">Activity</span>
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
      {visible.length === 0 ? (
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
              <span className="ticker-seq">seq {event.seq}</span>
              <span className="ticker-glyph" aria-hidden="true">
                {event.glyph}
              </span>
              <a
                className="ticker-session"
                href={buildSessionHash(event.sessionId)}
                title={`${event.sessionTitle} · ${event.project}`}
              >
                {event.sessionTitle}
              </a>
              <span className="ticker-text">{event.text}</span>
              <span
                className="ticker-source"
                title={`project ${event.project} · harness ${event.harness}`}
              >
                {event.project} · {harnessGlyph(event.harness)}
              </span>
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}
