import { statusBadgeSpec } from "./StatusBadge";
import type { StatusFilter } from "../lib/fleetSort";
import {
  STATUS_CHIP_FILTERS,
  STATUS_FILTERS,
  isLiveStatus,
} from "../lib/fleetSort";
import type { SessionStatus } from "../lib/sse";

/** Short chip labels (fleet toolbar space is tight; badge keeps full text). */
export const STATUS_FILTER_LABELS: Record<StatusFilter, string> = {
  all: "ALL",
  running: "RUNNING",
  "waiting-input": "WAITING",
  idle: "IDLE",
  error: "ERROR",
  archived: "ARCHIVED",
};

const STATUS_ORDER: readonly SessionStatus[] = [
  "running",
  "waiting-input",
  "idle",
  "error",
  "archived",
];

export interface StatusFilterBarProps {
  /** Per-status counts over the FULL snapshot (precomputed once per tick). */
  counts: Record<SessionStatus, number>;
  total: number;
  value: StatusFilter;
  liveOnly: boolean;
  onSelect: (status: StatusFilter) => void;
  onToggleLiveOnly: () => void;
}

export default function StatusFilterBar({
  counts,
  total,
  value,
  liveOnly,
  onSelect,
  onToggleLiveOnly,
}: StatusFilterBarProps) {
  const liveCount = STATUS_ORDER.filter(isLiveStatus).reduce(
    (sum, status) => sum + counts[status],
    0,
  );
  const filters =
    value === "idle"
      ? STATUS_FILTERS
      : STATUS_CHIP_FILTERS;
  return (
    <div className="filter-bar" role="group" aria-label="Filter fleet by status">
      {filters.map((filter) => {
        const selected = value === filter;
        const count = filter === "all" ? total : counts[filter];
        const spec = filter === "all" ? null : statusBadgeSpec(filter);
        return (
          <button
            key={filter}
            type="button"
            className={selected ? "filter-chip filter-chip-on" : "filter-chip"}
            aria-pressed={selected}
            aria-label={`Show ${STATUS_FILTER_LABELS[filter]} sessions (${count})`}
            onClick={() => onSelect(filter)}
          >
            {selected && (
              <span aria-hidden="true">✓</span>
            )}
            {spec !== null && (
              <span className="filter-glyph" aria-hidden="true">
                {spec.glyph}
              </span>
            )}
            <span>{STATUS_FILTER_LABELS[filter]}</span>
            <span className="count mono">{count}</span>
          </button>
        );
      })}
      <button
        type="button"
        className={liveOnly ? "filter-live filter-live-on" : "filter-live"}
        aria-pressed={liveOnly}
        aria-label={`Show only live sessions, running or waiting for input (${liveCount})`}
        onClick={onToggleLiveOnly}
      >
        <span aria-hidden="true">●</span>
        <span>Live only</span>
        <span className="count mono">{liveCount}</span>
      </button>
    </div>
  );
}
