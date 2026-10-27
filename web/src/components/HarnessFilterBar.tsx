/**
 * Harness filter bar (Wave 5): slice the fleet by agent runtime.
 * ALL / OPENCODE / PI chips with per-harness counts, mirroring the
 * StatusFilterBar chip pattern (glyph + label + count, never color alone).
 */

import { harnessBadgeSpec } from "./HarnessBadge";
import type {
  HarnessCounts,
  HarnessFilter,
} from "../lib/fleetSort";
import { HARNESS_FILTERS } from "../lib/fleetSort";

export const HARNESS_FILTER_LABELS: Record<HarnessFilter, string> = {
  all: "ALL",
  opencode: "OPENCODE",
  pi: "PI",
};

export interface HarnessFilterBarProps {
  /** Per-harness counts over the scoped snapshot (precomputed per tick). */
  counts: HarnessCounts;
  value: HarnessFilter;
  onSelect: (harness: HarnessFilter) => void;
}

export default function HarnessFilterBar({
  counts,
  value,
  onSelect,
}: HarnessFilterBarProps) {
  const total = counts.opencode + counts.pi;
  return (
    <div
      className="filter-bar"
      role="group"
      aria-label="Filter fleet by harness"
    >
      {HARNESS_FILTERS.map((filter) => {
        const selected = value === filter;
        const count = filter === "all" ? total : counts[filter];
        const spec = filter === "all" ? null : harnessBadgeSpec(filter);
        return (
          <button
            key={filter}
            type="button"
            className={selected ? "filter-chip filter-chip-on" : "filter-chip"}
            aria-pressed={selected}
            aria-label={`${HARNESS_FILTER_LABELS[filter]} sessions (${count})`}
            onClick={() => onSelect(filter)}
          >
            {selected && <span aria-hidden="true">✓</span>}
            {spec !== null && (
              <span className="badge-glyph" aria-hidden="true">
                {spec.glyph}
              </span>
            )}
            <span>{HARNESS_FILTER_LABELS[filter]}</span>
            <span className="count mono">{count}</span>
          </button>
        );
      })}
    </div>
  );
}
