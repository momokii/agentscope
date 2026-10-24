/**
 * Pinned sessions (W2-2): persisted quick-switch slots 1–9.
 *
 * Storage is localStorage direct (no state lib — the task bans new deps).
 * Key is namespaced; corrupt JSON resets to empty instead of crashing.
 */

import { useCallback, useState } from "react";

export const PINS_STORAGE_KEY = "opencode-dashboard:pins:v1";
export const MAX_PINS = 9;

function readPins(storage: Storage): string[] {
  try {
    const raw = storage.getItem(PINS_STORAGE_KEY);
    if (raw === null) return [];
    const parsed: unknown = JSON.parse(raw);
    if (!Array.isArray(parsed)) return [];
    return parsed.filter((id): id is string => typeof id === "string");
  } catch {
    // Corrupt JSON (or throwing storage): reset to empty, never crash.
    return [];
  }
}

function writePins(storage: Storage, pins: readonly string[]): void {
  try {
    storage.setItem(PINS_STORAGE_KEY, JSON.stringify(pins));
  } catch {
    // Quota/private-mode failures must not break the fleet view.
  }
}

function storageOrNull(): Storage | null {
  try {
    if (typeof localStorage === "undefined") return null;
    return localStorage;
  } catch {
    return null;
  }
}

export function loadPins(storage: Storage): string[] {
  return readPins(storage).slice(0, MAX_PINS);
}

export function savePins(storage: Storage, pins: readonly string[]): void {
  writePins(storage, pins.slice(0, MAX_PINS));
}

export function togglePin(pins: readonly string[], id: string): string[] {
  if (pins.includes(id)) return pins.filter((pin) => pin !== id);
  return [...pins, id].slice(0, MAX_PINS);
}

export interface PinsApi {
  pins: string[];
  toggle: (id: string) => void;
  isPinned: (id: string) => boolean;
}

/** React binding: pins state synced to localStorage on every change. */
export function usePins(): PinsApi {
  const [pins, setPins] = useState<string[]>(() => {
    const storage = storageOrNull();
    return storage ? readPins(storage).slice(0, MAX_PINS) : [];
  });

  const toggle = useCallback((id: string) => {
    setPins((prev) => {
      const next = togglePin(prev, id);
      const storage = storageOrNull();
      if (storage) writePins(storage, next);
      return next;
    });
  }, []);

  const isPinned = useCallback(
    (id: string) => pins.includes(id),
    [pins],
  );

  return { pins, toggle, isPinned };
}
