import type {
  DateFilter,
  DatePreset,
  IdleBucket,
  IdleBucketCounts,
} from "../lib/fleetSort";
import {
  DATE_PRESETS,
  DATE_PRESET_LABELS,
  IDLE_BUCKETS,
  IDLE_BUCKET_LABELS,
  IDLE_BUCKET_TITLES,
} from "../lib/fleetSort";

export interface RecencyFilterBarProps {
  counts: IdleBucketCounts;
  bucket: IdleBucket;
  onSelectBucket: (bucket: IdleBucket) => void;
  date: DateFilter;
  onChangeDate: (date: DateFilter) => void;
}

export default function RecencyFilterBar({
  counts,
  bucket,
  onSelectBucket,
  date,
  onChangeDate,
}: RecencyFilterBarProps) {
  const selectPreset = (preset: DatePreset) => {
    onChangeDate({ preset, from: null });
  };
  return (
    <div className="recency-bar">
      <div
        className="filter-bar"
        role="group"
        aria-label="Filter sessions by recency bucket"
      >
        {IDLE_BUCKETS.map((option) => {
          const selected = bucket === option;
          return (
            <button
              key={option}
              type="button"
              className={selected ? "filter-chip filter-chip-on" : "filter-chip"}
              aria-pressed={selected}
              aria-label={`${IDLE_BUCKET_LABELS[option]} sessions (${counts[option]})`}
              title={IDLE_BUCKET_TITLES[option]}
              onClick={() => onSelectBucket(option)}
            >
              {selected && <span aria-hidden="true">✓</span>}
              <span>{IDLE_BUCKET_LABELS[option]}</span>
              <span className="count mono">{counts[option]}</span>
            </button>
          );
        })}
      </div>

      <div className="recency-date" role="group" aria-label="Filter by last activity date">
        <span className="recency-date-label">Last activity</span>
        {DATE_PRESETS.map((preset) => {
          const selected = date.from === null && date.preset === preset;
          return (
            <button
              key={preset}
              type="button"
              className={
                selected ? "filter-chip filter-chip-on" : "filter-chip"
              }
              aria-pressed={selected}
              onClick={() => selectPreset(preset)}
            >
              {DATE_PRESET_LABELS[preset]}
            </button>
          );
        })}
        <label
          className="date-from"
          title="Approximate: derived from the snapshot clock because rows carry relative ages"
        >
          <span>From</span>
          <input
            type="date"
            value={date.from ?? ""}
            aria-label="Filter sessions updated on or after this date"
            onChange={(event) =>
              onChangeDate({
                preset: "all",
                from: event.target.value === "" ? null : event.target.value,
              })
            }
          />
        </label>
      </div>

      <div className="age-legend" aria-label="Idle bucket boundaries">
        <span className="age-legend-item">
          Recent ≤24h <span className="count mono">{counts.recent}</span>
        </span>
        <span className="age-legend-item">
          Stale 1–7d <span className="count mono">{counts.stale}</span>
        </span>
        <span className="age-legend-item">
          Old &gt;7d <span className="count mono">{counts.old}</span>
        </span>
      </div>
    </div>
  );
}
