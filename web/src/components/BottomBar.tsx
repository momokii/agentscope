/**
 * BottomBar (W2-2): persistent footer keymap (k9s canon). The activity
 * feed lives on its own tab now (`#/activity`); the footer keeps only
 * the always-visible keymap.
 *
 * The CmdBar overlay is rendered by the App shell (it needs session data +
 * hash routing). `paletteOpen` is threaded through for overlay
 * positioning state.
 */

export interface BottomBarProps {
  paletteOpen: boolean;
  live: boolean;
}

function Key({ children }: { children: string }) {
  return <kbd className="kbd">{children}</kbd>;
}

export default function BottomBar({
  paletteOpen,
  live,
}: BottomBarProps) {
  return (
    <footer
      className="bottombar"
      data-slot="bottom-bar"
      data-palette-open={paletteOpen ? "true" : "false"}
    >
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
