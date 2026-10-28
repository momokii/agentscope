/**
 * BottomBar (W2-2): event ticker + persistent footer keymap (k9s canon).
 *
 * The CmdBar overlay is rendered by the App shell (it needs session data +
 * hash routing); this footer owns the ticker and the always-visible keymap.
 * `paletteOpen` is threaded through for overlay positioning state.
 */

import EventTicker from "./EventTicker";
import type { ActivityEvent } from "../lib/activity";

export interface BottomBarProps {
  paletteOpen: boolean;
  events: readonly ActivityEvent[];
  paused: boolean;
  live: boolean;
  /** View scope label for the ticker ("Fleet" default). */
  scopeLabel?: string;
}

function Key({ children }: { children: string }) {
  return <kbd className="kbd">{children}</kbd>;
}

export default function BottomBar({
  paletteOpen,
  events,
  paused,
  live,
  scopeLabel,
}: BottomBarProps) {
  return (
    <footer
      className="bottombar"
      data-slot="bottom-bar"
      data-palette-open={paletteOpen ? "true" : "false"}
    >
      <EventTicker events={events} paused={paused} scopeLabel={scopeLabel} />
      <div
        className="keymap"
        aria-label="Keyboard shortcuts"
        data-live={live ? "true" : "false"}
      >
        <span className="keymap-item">
          <Key>:</Key>
          <span>jump</span>
        </span>
        <span className="keymap-item">
          <Key>⌘K</Key>
          <span>palette</span>
        </span>
        <span className="keymap-item">
          <Key>1–9</Key>
          <span>pinned</span>
        </span>
        <span className="keymap-item">
          <Key>p</Key>
          <span>pin selected</span>
        </span>
        <span className="keymap-item">
          <Key>l</Key>
          <span>{live ? "pause live" : "resume live"}</span>
        </span>
        <span className="keymap-item">
          <Key>esc</Key>
          <span>close</span>
        </span>
      </div>
    </footer>
  );
}
