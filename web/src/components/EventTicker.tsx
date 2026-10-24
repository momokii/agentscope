/**
 * Event ticker (W2-2): bottom strip rendering the activity ring.
 *
 * - One row per event: relative-ish seq stamp + glyph + one-line text.
 * - Newest-first ordering means appends never move existing rows: no
 *   scroll-yank by construction (no auto-scroll, no reordering).
 * - `paused` (LIVE off): freezes the list — new events are NOT appended
 *   while paused, and a ‖ PAUSED chip is shown (DESIGN section 6).
 * - Visible cap ACTIVITY_VISIBLE_CAP with a "show more" expander.
 */

import { useState } from "react";
import { Activity } from "lucide-react";

import type { ActivityEvent } from "../lib/activity";
import { ACTIVITY_VISIBLE_CAP } from "../lib/activity";

export interface EventTickerProps {
  events: readonly ActivityEvent[];
  /** True when LIVE is off: freeze appends, show PAUSED chip. */
  paused: boolean;
}

export default function EventTicker({ events, paused }: EventTickerProps) {
  const [expanded, setExpanded] = useState(false);
  const visible = expanded ? events : events.slice(0, ACTIVITY_VISIBLE_CAP);
  const hidden = events.length - visible.length;

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
              <span className="ticker-text">{event.text}</span>
            </li>
          ))}
        </ul>
      )}
      {hidden > 0 && (
        <button
          type="button"
          className="ticker-more"
          onClick={() => setExpanded(true)}
        >
          show more · {hidden} hidden
        </button>
      )}
      {expanded && events.length > ACTIVITY_VISIBLE_CAP && (
        <button
          type="button"
          className="ticker-more"
          onClick={() => setExpanded(false)}
        >
          show less
        </button>
      )}
    </section>
  );
}
