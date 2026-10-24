/**
 * Command-palette open-state hook (W2-2): `:` / Cmd+K listener.
 *
 * Keyboard discipline (binding): the global handler ignores events from
 * inputs, textareas, selects, and contentEditable — `:` opens the palette
 * only when NOT typing; digits 1–9 (handled in App) follow the same rule.
 * Esc closes at every level.
 */

import { useCallback, useEffect, useState } from "react";

export interface CommandPalette {
  open: boolean;
  setOpen: (open: boolean) => void;
}

/** True when the event target is an editable surface — never steal keys. */
export function isTypingTarget(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) return false;
  if (
    target instanceof HTMLInputElement ||
    target instanceof HTMLTextAreaElement ||
    target instanceof HTMLSelectElement
  ) {
    return true;
  }
  return target.isContentEditable === true;
}

export function useCommandPalette(): CommandPalette {
  const [open, setOpen] = useState(false);

  const onKeyDown = useCallback((event: KeyboardEvent) => {
    if (event.key === "Escape") {
      setOpen(false);
      return;
    }
    if (isTypingTarget(event.target)) return;
    const cmdK =
      (event.metaKey || event.ctrlKey) && event.key.toLowerCase() === "k";
    const colon = event.key === ":";
    if (cmdK || colon) {
      event.preventDefault();
      setOpen((prev) => !prev);
    }
  }, []);

  useEffect(() => {
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [onKeyDown]);

  return { open, setOpen };
}
