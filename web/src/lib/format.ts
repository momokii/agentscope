/**
 * Fleet number/time formatting. All output is plain text rendered in
 * --font-mono with tabular-nums (see app.css .mono); no chart code here.
 */

/** USD: sub-cent values keep 4 decimals (per-row costs), else 2. */
export function formatUSD(value: number): string {
  const safe = Number.isFinite(value) ? value : 0;
  const abs = Math.abs(safe);
  if (abs !== 0 && abs < 0.01) return `$${safe.toFixed(4)}`;
  return `$${safe.toLocaleString("en-US", {
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  })}`;
}

/** Compact magnitude: 1.2k / 3.4M, else rounded integer. */
export function formatCompact(value: number): string {
  const safe = Number.isFinite(value) ? value : 0;
  const abs = Math.abs(safe);
  if (abs >= 1_000_000) return `${(safe / 1_000_000).toFixed(1)}M`;
  if (abs >= 1_000) return `${(safe / 1_000).toFixed(1)}k`;
  return `${Math.round(safe)}`;
}

/**
 * Short age for stale/paused chips: "Xs ago" / "Xm ago" / "Xh ago".
 * Relative text only — never raw ms (DESIGN section 6).
 */
export function formatAge(ageMs: number): string {
  const safe = Math.max(0, ageMs);
  if (safe < 60_000) return `${Math.floor(safe / 1000)}s ago`;
  if (safe < 3_600_000) return `${Math.floor(safe / 60_000)}m ago`;
  return `${Math.floor(safe / 3_600_000)}h ago`;
}

/** Compact age for dense numeric slots: "12s" / "3m" / "2h" / "4d". */
export function formatAgeShort(ageMs: number): string {
  const safe = Math.max(0, ageMs);
  if (safe < 60_000) return `${Math.floor(safe / 1000)}s`;
  if (safe < 3_600_000) return `${Math.floor(safe / 60_000)}m`;
  if (safe < 86_400_000) return `${Math.floor(safe / 3_600_000)}h`;
  return `${Math.floor(safe / 86_400_000)}d`;
}

export interface RelativeTime {
  /** Short label: "Xs ago" / "Xm ago" / Today / Yesterday / date. */
  label: string;
  /** Absolute timestamp for the <time> tooltip. */
  absolute: string;
}

/**
 * Fleet "updated" cell (DESIGN section 6): <60s "Xs ago", <60m "Xm ago",
 * else Today/Yesterday/date grouping; absolute timestamp in tooltip.
 */
export function formatRelativeTime(
  updatedRelativeMs: number,
  nowMs: number,
): RelativeTime {
  const safe = Math.max(0, updatedRelativeMs);
  const updatedAt = new Date(nowMs - safe);
  const absolute = updatedAt.toISOString();
  if (safe < 60_000) return { label: `${Math.floor(safe / 1000)}s ago`, absolute };
  if (safe < 3_600_000)
    return { label: `${Math.floor(safe / 60_000)}m ago`, absolute };
  const startOfToday = new Date(nowMs);
  startOfToday.setHours(0, 0, 0, 0);
  if (updatedAt.getTime() >= startOfToday.getTime())
    return { label: "Today", absolute };
  if (updatedAt.getTime() >= startOfToday.getTime() - 86_400_000)
    return { label: "Yesterday", absolute };
  return {
    label: updatedAt.toLocaleDateString("en-US", {
      month: "short",
      day: "numeric",
    }),
    absolute,
  };
}
