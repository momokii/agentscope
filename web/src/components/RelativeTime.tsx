/**
 * Fleet "updated" cell (DESIGN section 6): relative label with the absolute
 * timestamp in the tooltip. Pure: callers pass updated_relative_ms + now.
 */

import { formatRelativeTime } from "../lib/format";

interface RelativeTimeProps {
  updatedRelativeMs: number;
  /** Injectable clock for tests; defaults to Date.now(). */
  nowMs?: number;
}

export default function RelativeTime({
  updatedRelativeMs,
  nowMs,
}: RelativeTimeProps) {
  const now = nowMs ?? Date.now();
  const { label, absolute } = formatRelativeTime(updatedRelativeMs, now);
  return (
    <time className="mono" title={absolute} dateTime={absolute}>
      {label}
    </time>
  );
}
