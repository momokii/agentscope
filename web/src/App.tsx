/**
 * App shell: header + Fleet/SessionStub views + CmdBar overlay + BottomBar.
 *
 * Owns the SSE client lifecycle (the only stateful data owner — Fleet and
 * KpiStrip are pure Snapshot consumers, max 2 levels of prop drilling).
 * LIVE-off closes the client (paused chip); LIVE-on reconnects from the
 * last applied seq via the client's resume cursor.
 *
 * W2-2 additions (surgical, shell preserved):
 * - hash routing (`#/` fleet, `#/sessions/:id` stub) with selected-row
 *   highlight + scroll-into-view on select;
 * - activity ring accumulation from consecutive Snapshot ticks
 *   (diffSnapshots over the prev/snapshot pair already kept for KPI deltas);
 * - global keys: digits 1–9 jump to pinned slots, `l` toggles live,
 *   `p` pins/unpins the selected session — all ignored while typing;
 * - CmdBar overlay + BottomBar ticker/keymap wiring.
 */

import { useCallback, useEffect, useRef, useState } from "react";
import { Activity } from "lucide-react";

import { createSSEClient } from "./lib/sse";
import type { Snapshot, StatusEvent } from "./lib/sse";
import { diffSnapshots, prependEvents } from "./lib/activity";
import type { ActivityEvent } from "./lib/activity";
import { usePins } from "./lib/pins";
import {
  buildCompareHash,
  buildFleetHash,
  buildSessionHash,
  buildTimelineHash,
} from "./lib/routing";
import { parseHash } from "./lib/routing";
import type { Route } from "./lib/routing";
import type { StatusFilter } from "./lib/fleetSort";
import { isTypingTarget, useCommandPalette } from "./lib/useCommandPalette";
import BottomBar from "./components/BottomBar";
import CmdBar from "./components/CmdBar";
import Compare from "./views/Compare";
import Fleet from "./views/Fleet";
import GlobalTimeline from "./views/GlobalTimeline";
import SessionDetail from "./views/SessionDetail";

function readRoute(): Route {
  return parseHash(
    typeof window === "undefined" ? "#/" : window.location.hash,
  );
}

/** Digit keys 1–9 map to pinned slots 0–8 (char codes, no regex). */
function digitSlot(key: string): number | null {
  if (key.length !== 1) return null;
  const code = key.charCodeAt(0);
  if (code < 49 || code > 57) return null;
  return code - 49;
}

export default function App() {
  const [snapshot, setSnapshot] = useState<Snapshot | null>(null);
  const [prevSnapshot, setPrevSnapshot] = useState<Snapshot | null>(null);
  const [status, setStatus] = useState<StatusEvent>({
    state: "connecting",
    attempt: 0,
    lastEventId: 0,
  });
  const [live, setLive] = useState(true);
  const [receivedAt, setReceivedAt] = useState<number | null>(null);
  const [, forceAgeTick] = useState(0);
  const palette = useCommandPalette();
  const snapshotRef = useRef<Snapshot | null>(null);
  const [events, setEvents] = useState<ActivityEvent[]>([]);
  const pausedRef = useRef(false);
  const { pins, toggle: togglePin } = usePins();
  const [route, setRoute] = useState<Route>(readRoute);

  // Re-render once a second so the stale chip's "Xs ago" age stays fresh.
  // Fleet rows re-sort on snapshot ticks only (no scroll-yank from this).
  useEffect(() => {
    const timer = setInterval(() => forceAgeTick((n) => n + 1), 1000);
    return () => clearInterval(timer);
  }, []);

  const liveRef = useRef(live);
  liveRef.current = live;
  pausedRef.current = !live;

  useEffect(() => {
    const onHashChange = () => setRoute(readRoute());
    window.addEventListener("hashchange", onHashChange);
    return () => window.removeEventListener("hashchange", onHashChange);
  }, []);

  useEffect(() => {
    if (!live) return;
    const client = createSSEClient({
      url: "/api/events",
      onSnapshot: (next) => {
        // Atomic replace per the client contract: prev holds the last
        // applied tick for KPI deltas, snapshot becomes the new tick.
        // Ticker ring: diff the same consecutive pair, newest-first.
        const prev = snapshotRef.current;
        setEvents((ring) =>
          pausedRef.current
            ? ring
            : prependEvents(ring, diffSnapshots(prev, next)),
        );
        setPrevSnapshot(prev);
        snapshotRef.current = next;
        setSnapshot(next);
        setReceivedAt(Date.now());
      },
      onStatus: setStatus,
    });
    return () => client.close();
  }, [live]);

  const selectSession = useCallback(
    (id: string) => {
      window.location.hash = buildSessionHash(id);
      palette.setOpen(false);
      requestAnimationFrame(() => {
        document
          .getElementById(`row-${CSS.escape(id)}`)
          ?.scrollIntoView({ block: "nearest" });
      });
    },
    [palette],
  );

  // Compare picker writes hash state; back/forward works via hashchange.
  const changeCompareIds = useCallback((ids: string[]) => {
    window.location.hash = buildCompareHash(ids);
  }, []);

  // Wave-3-drilldown hook stub: chart-point click selects the session via
  // the existing hash route (full span inspector is W3-2's right-rail).
  const selectPointSession = useCallback(
    (id: string) => {
      selectSession(id);
    },
    [selectSession],
  );

  const selectedId = route.view === "session" ? route.id : null;

  // Fleet-UX filter state lives in the hash (`#/?status=running&live=1`) so
  // filtered views are shareable and back/forward works via hashchange.
  const fleetStatus: StatusFilter =
    route.view === "fleet" ? (route.status ?? "all") : "all";
  const fleetLiveOnly = route.view === "fleet" && route.liveOnly === true;

  const changeFleetFilter = useCallback(
    (status: StatusFilter, liveOnly: boolean) => {
      window.location.hash = buildFleetHash(status, liveOnly);
    },
    [],
  );

  // Global quick-switch + live/pin keys. Ignored while typing; Esc closes
  // the palette first (hook), then drops back from stub/compare/timeline
  // to fleet.
  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        if (!palette.open && route.view !== "fleet") {
          window.location.hash = "#/";
        }
        return;
      }
      if (palette.open || isTypingTarget(event.target)) return;
      if (event.metaKey || event.ctrlKey || event.altKey) return;
      const slot = digitSlot(event.key);
      if (slot !== null) {
        const id = pins[slot];
        if (id) {
          event.preventDefault();
          selectSession(id);
        }
        return;
      }
      if (event.key === "l") {
        event.preventDefault();
        setLive((v) => !v);
      } else if (event.key === "p" && selectedId !== null) {
        event.preventDefault();
        togglePin(selectedId);
      }
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [palette.open, pins, route.view, selectedId, selectSession, togglePin]);

  const selectedRow =
    selectedId === null
      ? null
      : (snapshot?.sessions.find((row) => row.id === selectedId) ?? null);

  return (
    <div className="app">
      <header className="app-header">
        <span className="brand">
          <Activity aria-hidden="true" />
          <span>opencode mission control</span>
        </span>
        <nav className="app-nav" aria-label="Views">
          <a
            className={
              route.view === "fleet" || route.view === "session"
                ? "app-nav-link app-nav-link-active"
                : "app-nav-link"
            }
            href="#/"
          >
            Fleet
          </a>
          <a
            className={
              route.view === "compare"
                ? "app-nav-link app-nav-link-active"
                : "app-nav-link"
            }
            href="#/compare"
          >
            Compare
          </a>
          <a
            className={
              route.view === "timeline"
                ? "app-nav-link app-nav-link-active"
                : "app-nav-link"
            }
            href={buildTimelineHash()}
          >
            Timeline
          </a>
        </nav>
        <span className="seq mono">
          {snapshot ? `seq ${snapshot.seq}` : "no snapshot yet"}
        </span>
      </header>
      <main className="app-main">
        {route.view === "session" ? (
          <SessionDetail
            id={route.id}
            tab={route.tab}
            row={selectedRow}
            paletteOpen={palette.open}
            messageId={route.messageId ?? null}
            sessions={snapshot?.sessions ?? []}
          />
        ) : route.view === "compare" ? (
          <Compare
            sessions={snapshot?.sessions ?? []}
            ids={route.ids}
            onChangeIds={changeCompareIds}
            onSelect={selectSession}
            onPointSelect={selectPointSession}
          />
        ) : route.view === "timeline" ? (
          <GlobalTimeline
            sessions={snapshot?.sessions ?? []}
            selectedId={selectedId}
            onSelect={selectSession}
          />
        ) : (
          <Fleet
            snapshot={snapshot}
            prevSnapshot={prevSnapshot}
            phase={status.state}
            attempt={status.attempt}
            live={live}
            snapshotAgeMs={receivedAt === null ? null : Date.now() - receivedAt}
            onToggleLive={() => setLive((v) => !v)}
            selectedId={selectedId}
            pins={pins}
            onTogglePin={togglePin}
            onSelect={selectSession}
            statusFilter={fleetStatus}
            liveOnly={fleetLiveOnly}
            onFilterChange={changeFleetFilter}
          />
        )}
      </main>
      <CmdBar
        open={palette.open}
        sessions={snapshot?.sessions ?? []}
        onSelect={selectSession}
        onClose={() => palette.setOpen(false)}
      />
      <BottomBar
        paletteOpen={palette.open}
        events={events}
        paused={!live}
        live={live}
      />
    </div>
  );
}
