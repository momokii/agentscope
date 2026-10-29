/**
 * Activity view (`#/activity`): the fleet event log as a full page.
 * Same ring as the bottom ticker (newest first, capped), but browsable:
 * text search, harness slice, and paging. Rows link to their sessions.
 * The GUIDE blurb states the purpose: a "what just changed" feed for
 * operators watching long runs — transitions, errors, and token bursts.
 */

import { useMemo, useState } from "react";
import { Search } from "lucide-react";

import type { ActivityEvent } from "../lib/activity";
import type { HarnessFilter } from "../lib/fleetSort";
import { HARNESS_FILTERS } from "../lib/fleetSort";
import ActivityRow from "../components/ActivityRow";
import DataGuide from "../components/DataGuide";
import Pagination from "../components/Pagination";
import { ACTIVITY_PAGE_SIZE } from "../lib/routing";

export interface ActivityViewProps {
  events: readonly ActivityEvent[];
  /** URL-serialized search text. */
  query?: string;
  onQueryChange?: (query: string | undefined) => void;
  /** URL-serialized 1-based page. */
  page?: number;
  onPageChange?: (page: number) => void;
  /** True when LIVE is off: the ring is frozen at the last tick. */
  paused?: boolean;
}

function matchesQuery(event: ActivityEvent, needle: string): boolean {
  if (needle === "") return true;
  const hay = `${event.sessionId} ${event.sessionTitle} ${event.project} ${event.kind} ${event.harness}`.toLowerCase();
  return hay.includes(needle);
}

export default function ActivityView({
  events,
  query = "",
  onQueryChange,
  page = 1,
  onPageChange,
  paused = false,
}: ActivityViewProps) {
  const [harness, setHarness] = useState<HarnessFilter>("all");
  const [pageSize, setPageSize] = useState(ACTIVITY_PAGE_SIZE);
  const needle = query.trim().toLowerCase();

  const filtered = useMemo(
    () =>
      events.filter(
        (e) =>
          (harness === "all" || e.harness === harness) &&
          matchesQuery(e, needle),
      ),
    [events, harness, needle],
  );
  const pageCount = Math.max(1, Math.ceil(filtered.length / pageSize));
  const safePage = Math.min(Math.max(1, page), pageCount);
  const pageRows = filtered.slice(
    (safePage - 1) * pageSize,
    safePage * pageSize,
  );

  return (
    <div className="activity-view">
      <div className="fleet-toolbar">
        <span className="fleet-title">
          Activity <span className="count">({filtered.length})</span>
        </span>
        <span
          className="guide-blurb"
          title="Every snapshot tick diffed against the previous one: appearances, status flips, waiting-input raises/clears, completions, disappearances, and the largest token/cost movers."
        >
          What just changed across the fleet
        </span>
        <label className="fleet-search">
          <Search aria-hidden="true" />
          <input
            type="search"
            value={query}
            onChange={(event) => onQueryChange?.(event.target.value || undefined)}
            placeholder="Filter title, project, id…"
            aria-label="Filter activity"
          />
        </label>
        <div
          className="filter-bar activity-harness"
          role="group"
          aria-label="Filter activity by harness"
        >
          {HARNESS_FILTERS.map((filter) => {
            const selected = harness === filter;
            return (
              <button
                key={filter}
                type="button"
                className={selected ? "filter-chip filter-chip-on" : "filter-chip"}
                aria-pressed={selected}
                onClick={() => setHarness(filter)}
              >
                {selected && <span aria-hidden="true">✓</span>}
                <span>{filter === "all" ? "ALL" : filter.toUpperCase()}</span>
              </button>
            );
          })}
        </div>
        <DataGuide blurb="The full event log behind the bottom ticker: newest first, capped at the last 100 transitions. Search narrows to a session, project, or kind; pages slice history; every row deep-links to its session." />
        <span className="state-chip" aria-label={paused ? "Stream paused" : "Stream live"}>
          <span className="dot" aria-hidden="true" />
          <span>{paused ? "Paused" : "Live"}</span>
        </span>
      </div>

      {pageRows.length === 0 ? (
        <div className="empty-state">
          {events.length === 0 ? (
            <p>No activity yet — transitions appear as snapshots tick.</p>
          ) : (
            <p>No activity matches the current filter.</p>
          )}
        </div>
      ) : (
        <ul className="ticker-list activity-list" aria-label="Activity log">
          {pageRows.map((event, index) => (
            <li
              key={`${event.seq}-${event.sessionId}-${event.kind}-${index}`}
              className="ticker-row mono"
            >
              <ActivityRow event={event} />
            </li>
          ))}
        </ul>
      )}

      {filtered.length > 0 && (
        <Pagination
          page={safePage}
          totalPages={pageCount}
          total={filtered.length}
          onPageChange={(next) => onPageChange?.(next)}
          pageSize={pageSize}
          onPageSizeChange={(size) => {
            setPageSize(size);
            onPageChange?.(1);
          }}
        />
      )}
    </div>
  );
}
