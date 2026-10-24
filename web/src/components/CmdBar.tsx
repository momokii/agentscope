/**
 * CmdBar jump bar (W2-2): `:` / Cmd+K fuzzy jump over id/title/cwd/model.
 *
 * - Subsequence fuzzy match (case-insensitive) across id, title, directory,
 *   model; empty query lists every session (fleet order).
 * - Keyboard: up/down moves the active hit, Enter selects (hash route +
 *   row highlight via onSelect), Esc closes. Global key handling lives in
 *   useCommandPalette; this component owns list-local nav keys only.
 * - Each hit shows status glyph + relative time; empty-state text when
 *   nothing matches. Lucide-only icons, var()-only styling.
 */

import { useEffect, useMemo, useRef, useState } from "react";
import { Check, Search } from "lucide-react";

import type { SessionRow } from "../lib/sse";
import { statusBadgeSpec } from "./StatusBadge";
import { formatRelativeTime } from "../lib/format";

export interface CmdBarProps {
  open: boolean;
  sessions: readonly SessionRow[];
  onSelect: (id: string) => void;
  onClose: () => void;
  /** Injectable clock for tests; defaults to Date.now(). */
  nowMs?: number;
}

/** Subsequence fuzzy match: every needle char appears in order in haystack. */
export function fuzzyMatch(needle: string, haystack: string): boolean {
  const n = needle.toLowerCase();
  const h = haystack.toLowerCase();
  if (n === "") return true;
  let j = 0;
  for (let i = 0; i < h.length && j < n.length; i += 1) {
    if (h[i] === n[j]) j += 1;
  }
  return j === n.length;
}

export function filterCmdSessions(
  sessions: readonly SessionRow[],
  query: string,
): SessionRow[] {
  const needle = query.trim().toLowerCase();
  if (needle === "") return [...sessions];
  return sessions.filter((row) =>
    [row.id, row.title, row.directory, row.model].some((field) =>
      fuzzyMatch(needle, field),
    ),
  );
}

export default function CmdBar({
  open,
  sessions,
  onSelect,
  onClose,
  nowMs,
}: CmdBarProps) {
  const [query, setQuery] = useState("");
  const [active, setActive] = useState(0);
  const inputRef = useRef<HTMLInputElement>(null);

  const hits = useMemo(
    () => filterCmdSessions(sessions, query),
    [sessions, query],
  );

  useEffect(() => {
    if (open) {
      setQuery("");
      setActive(0);
      inputRef.current?.focus();
    }
  }, [open ]);

  useEffect(() => {
    setActive(0);
  }, [query]);

  useEffect(() => {
    if (active > hits.length - 1) setActive(Math.max(0, hits.length - 1));
  }, [active, hits.length]);

  if (!open) return null;

  const onKeyDown = (event: React.KeyboardEvent) => {
    if (event.key === "ArrowDown") {
      event.preventDefault();
      setActive((prev) => Math.min(prev + 1, hits.length - 1));
    } else if (event.key === "ArrowUp") {
      event.preventDefault();
      setActive((prev) => Math.max(prev - 1, 0));
    } else if (event.key === "Enter") {
      event.preventDefault();
      const hit = hits[active];
      if (hit) onSelect(hit.id);
    } else if (event.key === "Escape") {
      event.preventDefault();
      onClose();
    }
  };

  const now = nowMs ?? Date.now();

  return (
    <div
      className="cmdbar-overlay"
      role="dialog"
      aria-modal="true"
      aria-label="Session jump bar"
    >
      <div className="cmdbar" onKeyDown={onKeyDown}>
        <label className="cmdbar-input-row">
          <Search aria-hidden="true" size={12} />
          <input
            ref={inputRef}
            type="text"
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            placeholder="Jump to session — id, title, cwd, model…"
            aria-label="Jump to session"
          />
        </label>
        {hits.length === 0 ? (
          <div className="cmdbar-empty">
            No sessions match “{query.trim()}”.
          </div>
        ) : (
          <ul className="cmdbar-list" role="listbox" aria-label="Sessions">
            {hits.map((row, index) => {
              const spec = statusBadgeSpec(row.status);
              const rel = formatRelativeTime(row.updated_relative_ms, now);
              return (
                <li key={row.id} role="option" aria-selected={index === active}>
                  <button
                    type="button"
                    className={
                      index === active
                        ? "cmdbar-hit cmdbar-hit-active"
                        : "cmdbar-hit"
                    }
                    onClick={() => onSelect(row.id)}
                    onMouseMove={() => setActive(index)}
                  >
                    <span className="badge-glyph" aria-hidden="true">
                      {spec.glyph}
                    </span>
                    <span className="cmdbar-hit-id mono">{row.id}</span>
                    <span className="cmdbar-hit-title">{row.title}</span>
                    <span className="cmdbar-hit-meta mono" title={row.directory}>
                      {row.model} · {rel.label}
                    </span>
                    {index === active && (
                      <Check aria-hidden="true" size={12} />
                    )}
                  </button>
                </li>
              );
            })}
          </ul>
        )}
      </div>
    </div>
  );
}
