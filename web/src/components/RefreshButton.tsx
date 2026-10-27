/**
 * Manual refresh button: re-pulls /api/fleet through the snapshot pipeline
 * (works paused, resets staleness/ages, never touches the SSE stream).
 * Sits beside the Live toggle in the fleet and projects toolbars.
 */

import { RefreshCw } from "lucide-react";

export interface RefreshButtonProps {
  onRefresh?: () => void;
  refreshing?: boolean;
}

export default function RefreshButton({
  onRefresh,
  refreshing = false,
}: RefreshButtonProps) {
  return (
    <button
      type="button"
      className="live-toggle"
      aria-label={refreshing ? "Refreshing…" : "Refresh now"}
      title="Re-pull the fleet snapshot now"
      onClick={onRefresh}
      disabled={refreshing || onRefresh === undefined}
    >
      <RefreshCw
        aria-hidden="true"
        size={12}
        className={refreshing ? "spin" : undefined}
      />
      <span>{refreshing ? "…" : "Refresh"}</span>
    </button>
  );
}
