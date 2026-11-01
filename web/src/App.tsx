/**
 * App shell: header + views + CmdBar overlay + BottomBar.
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
 * - CmdBar overlay + BottomBar keymap wiring (the activity ring still
 *   accumulates here to feed the #/activity log view).
 */

import { useCallback, useEffect, useRef, useState } from "react";
import Logo from "./components/Logo";

import { createSSEClient, normalizeSnapshot } from "./lib/sse";
import type { Snapshot, StatusEvent } from "./lib/sse";
import { diffSnapshots, prependEvents } from "./lib/activity";
import type { ActivityEvent } from "./lib/activity";
import {
  LONG_RUN_MS,
  appendAlertLog,
  clearAlertLog,
  createAlertTracker,
  detectAlerts,
  loadAlertLog,
} from "./lib/alerts";
import type { AlertEvent } from "./lib/alerts";
import AlertsBell, {
  loadAlertsConfig,
  notificationSupport,
  saveAlertsConfig,
} from "./components/AlertsBell";
import type { AlertsConfig } from "./components/AlertsBell";
import Toasts from "./components/Toasts";
import type { Toast } from "./components/Toasts";
import { usePins } from "./lib/pins";
import {
  buildActivityHash,
  buildCompareHash,
  buildFleetQueryHash,
  buildProjectHash,
  buildSessionHash,
  buildTimelineHash,
} from "./lib/routing";
import { parseHash } from "./lib/routing";
import type { FleetQuery, Route } from "./lib/routing";
import type { StatusFilter } from "./lib/fleetSort";
import { DEFAULT_DATE_FILTER, DEFAULT_IDLE_BUCKET } from "./lib/fleetSort";
import { hasProjects } from "./lib/projects";
import { isTypingTarget, useCommandPalette } from "./lib/useCommandPalette";
import BottomBar from "./components/BottomBar";
import CmdBar from "./components/CmdBar";
import Compare from "./views/Compare";
import Fleet from "./views/Fleet";
import ActivityView from "./views/Activity";
import GlobalTimeline from "./views/GlobalTimeline";
import HistoryView from "./views/History";
import Projects from "./views/Projects";
import SessionDetail from "./views/SessionDetail";

function readRoute(): Route {
  return parseHash(
    typeof window === "undefined" ? "#/" : window.location.hash,
  );
}

function queryFromRoute(route: Route): FleetQuery | null {
  if (route.view !== "fleet" && route.view !== "project") return null;
  return {
    status: route.status,
    liveOnly: route.liveOnly,
    harness: route.harness,
    bucket: route.bucket,
    since: route.since,
    from: route.from,
    page: route.page,
  };
}

function trimmedQuery(query: FleetQuery): FleetQuery {
  const next: FleetQuery = { ...query };
  if (next.from) next.since = undefined;
  else if (next.since) next.from = undefined;
  return next;
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
  // Alerts (Wave 6): client-side only, derived from the snapshots the App
  // already owns. Detection always runs (keeps dedupe memory correct);
  // surfacing (toast + notification + title badge) requires opt-in.
  const [alertsConfig, setAlertsConfig] = useState<AlertsConfig>(() =>
    typeof window === "undefined" ? { enabled: false, budget: null } : loadAlertsConfig(),
  );
  const alertsConfigRef = useRef(alertsConfig);
  alertsConfigRef.current = alertsConfig;
  const trackerRef = useRef(createAlertTracker());
  const toastIdRef = useRef(0);
  const [toasts, setToasts] = useState<Toast[]>([]);
  const [unread, setUnread] = useState(0);
  // Alert history: every surfaced trigger with when + why, persisted
  // across reloads (see alerts.ts). Only surfaced alerts are logged —
  // detection runs always, but history records what the operator saw.
  const [alertLog, setAlertLog] = useState<AlertEvent[]>(() =>
    typeof window === "undefined" ? [] : loadAlertLog(),
  );
  const [notifyState, setNotifyState] = useState(() =>
    typeof window === "undefined" || typeof Notification === "undefined"
      ? { supported: false as const, permission: "unsupported" as const }
      : notificationSupport(),
  );
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

  const dismissToast = useCallback((toastId: number) => {
    setToasts((list) => list.filter((t) => t.toastId !== toastId));
  }, []);

  const surfaceAlert = useCallback(
    (alert: AlertEvent) => {
      const id = (toastIdRef.current += 1);
      setToasts((list) => [...list.slice(-4), { ...alert, toastId: id }]);
      setUnread((n) => n + 1);
      window.setTimeout(() => dismissToast(id), 8000);
      // System notification only when explicitly enabled AND the platform
      // can actually show it (secure contexts only — plain-HTTP tailnet IPs
      // can never grant permission, so the toast above is the fallback).
      if (
        alertsConfigRef.current.enabled &&
        notifyState.supported &&
        notifyState.permission === "granted" &&
        window.isSecureContext
      ) {
        try {
          new Notification(alert.title, { body: alert.body, tag: `ocd-${alert.kind}-${alert.sessionId ?? "fleet"}` });
        } catch {
          // Toast already covers it; never let notification infra throw.
        }
      }
    },
    [dismissToast, notifyState.permission, notifyState.supported],
  );

  // Shared apply path for SSE ticks and manual refreshes: atomic replace
  // (prev holds the last applied tick for KPI deltas), ticker ring diffed
  // over the same consecutive pair, newest-first. Alerts diff the same
  // pair (detection always runs; surfacing honors the opt-in).
  const applySnapshot = useCallback(
    (next: Snapshot) => {
      const prev = snapshotRef.current;
      setEvents((ring) =>
        pausedRef.current ? ring : prependEvents(ring, diffSnapshots(prev, next)),
      );
      const fired = detectAlerts(prev, next, Date.now(), trackerRef.current, {
        longRunMs: LONG_RUN_MS,
        budget: alertsConfigRef.current.budget,
      });
      if (alertsConfigRef.current.enabled) {
        for (const alert of fired) surfaceAlert(alert);
        if (fired.length > 0) setAlertLog((log) => appendAlertLog(log, fired));
      }
      setPrevSnapshot(prev);
      snapshotRef.current = next;
      setSnapshot(next);
      setReceivedAt(Date.now());
    },
    [surfaceAlert],
  );

  // Unread badge in the tab title; clears when the tab regains focus.
  useEffect(() => {
    document.title =
      unread > 0 ? `(${unread}) opencode mission control` : "opencode mission control";
  }, [unread]);
  useEffect(() => {
    const clear = () => setUnread(0);
    window.addEventListener("focus", clear);
    document.addEventListener("visibilitychange", () => {
      if (!document.hidden) clear();
    });
    return () => {
      window.removeEventListener("focus", clear);
    };
  }, []);

  const changeAlertsConfig = useCallback((next: AlertsConfig) => {
    setAlertsConfig(next);
    saveAlertsConfig(next);
  }, []);

  const enableNotifications = useCallback(async () => {
    if (!("Notification" in window)) return;
    try {
      const result = await Notification.requestPermission();
      setNotifyState({ supported: true, permission: result });
    } catch {
      // Denied or unavailable: toasts still work.
    }
  }, []);

  const testAlert = useCallback(() => {
    const now = Date.now();
    surfaceAlert({
      kind: "waiting",
      title: "Test alert: input needed",
      body: "This is how waiting-input, long runs, and budget crosses reach you.",
      ts: now,
      why: "manual test from the Alerts panel — no rule fired",
    });
    setAlertLog((log) =>
      appendAlertLog(log, [
        {
          kind: "waiting",
          title: "Test alert: input needed",
          body: "This is how waiting-input, long runs, and budget crosses reach you.",
          ts: now,
          why: "manual test from the Alerts panel — no rule fired",
        },
      ]),
    );
    if (notifyState.supported && Notification.permission === "default") {
      void enableNotifications();
    } else if (notifyState.supported && Notification.permission === "granted" && window.isSecureContext) {
      try {
        new Notification("Test alert: input needed", {
          body: "This is how waiting-input, long runs, and budget crosses reach you.",
          tag: "ocd-test",
        });
      } catch {
        // Toast already covers it.
      }
    }
  }, [surfaceAlert, notifyState.supported, enableNotifications]);

  useEffect(() => {
    if (!live) return;
    const client = createSSEClient({
      url: "/api/events",
      onSnapshot: applySnapshot,
      onStatus: setStatus,
    });
    return () => client.close();
  }, [live, applySnapshot]);

  // Manual refresh (toolbar button): re-pull /api/fleet through the same
  // pipeline — works paused, resets staleness/ages, never touches SSE.
  const [refreshing, setRefreshing] = useState(false);
  const refreshNow = useCallback(async () => {
    if (refreshing) return;
    setRefreshing(true);
    try {
      const response = await fetch("/api/fleet", {
        credentials: "same-origin",
      });
      if (response.ok) {
        applySnapshot(normalizeSnapshot(await response.json()));
      }
    } catch {
      // A failed manual pull changes nothing: the next SSE tick recovers.
    } finally {
      setRefreshing(false);
    }
  }, [refreshing, applySnapshot]);

  // P6 serves pi transcripts through the same export endpoint, so every
  // row deep-links to the transcript default (the Live pane tab stays one
  // click away for rows with a running pane).
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

  const fleetRoute = route.view === "fleet" || route.view === "project";
  const fleetStatus: StatusFilter = fleetRoute
    ? (route.status ?? "all")
    : "all";
  const fleetLiveOnly = fleetRoute && route.liveOnly === true;
  const fleetHarness = fleetRoute ? (route.harness ?? "all") : "all";
  const fleetBucket = fleetRoute
    ? (route.bucket ?? DEFAULT_IDLE_BUCKET)
    : DEFAULT_IDLE_BUCKET;
  const fleetDate = fleetRoute
    ? { preset: route.since ?? "all", from: route.from ?? null }
    : DEFAULT_DATE_FILTER;
  const fleetPage = fleetRoute ? (route.page ?? 1) : 1;

  const changeFleetFilter = useCallback(
    (status: StatusFilter, liveOnly: boolean) => {
      const current = readRoute();
      if (current.view !== "fleet" && current.view !== "project") return;
      const base = trimmedQuery(queryFromRoute(current) ?? {});
      const next: FleetQuery = { ...base, status, liveOnly, page: 1 };
      window.location.hash =
        current.view === "project"
          ? buildProjectHash(current.directory, next)
          : buildFleetQueryHash(next);
    },
    [],
  );

  const changeFleetQuery = useCallback((patch: FleetQuery) => {
    const current = readRoute();
    if (current.view !== "fleet" && current.view !== "project") return;
    const base = queryFromRoute(current) ?? {};
    const next = trimmedQuery({ ...base, ...patch });
    window.location.hash =
      current.view === "project"
        ? buildProjectHash(current.directory, next)
        : buildFleetQueryHash(next);
  }, []);

  const changeActivityQuery = useCallback(
    (patch: { query?: string; page?: number }) => {
      const current = readRoute();
      if (current.view !== "activity") return;
      const next = { ...current };
      if (patch.query !== undefined) {
        if (patch.query === "") delete next.query;
        else next.query = patch.query;
      }
      if (patch.page !== undefined) {
        if (patch.page <= 1) delete next.page;
        else next.page = patch.page;
      }
      window.location.hash = buildActivityHash({
        query: next.query,
        page: next.page,
      });
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
          <Logo size={18} />
          <span>opencode mission control</span>
        </span>
        <nav className="app-nav" aria-label="Views">
          <a
            className={
              route.view === "fleet" ||
              route.view === "project" ||
              route.view === "session"
                ? "app-nav-link app-nav-link-active"
                : "app-nav-link"
            }
            href="#/"
          >
            Projects
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
          <a
            className={
              route.view === "activity"
                ? "app-nav-link app-nav-link-active"
                : "app-nav-link"
            }
            href={buildActivityHash()}
          >
            Activity
          </a>
          <a
            className={
              route.view === "history"
                ? "app-nav-link app-nav-link-active"
                : "app-nav-link"
            }
            href="#/history"
          >
            History
          </a>
        </nav>
        <AlertsBell
          config={alertsConfig}
          onChange={changeAlertsConfig}
          supported={notifyState.supported}
          permission={notifyState.permission}
          onEnable={enableNotifications}
          onTest={testAlert}
          unread={unread}
          history={alertLog}
          onClearHistory={() => setAlertLog(clearAlertLog())}
        />
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
        ) : route.view === "activity" ? (
          <ActivityView
            events={events}
            query={route.query ?? ""}
            onQueryChange={(query) => changeActivityQuery({ query, page: 1 })}
            page={route.page ?? 1}
            onPageChange={(page) => changeActivityQuery({ page })}
            paused={!live}
          />
        ) : route.view === "history" ? (
          <HistoryView />
        ) : route.view === "project" ? (
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
            harnessFilter={fleetHarness}
            projectDirectory={route.directory}
            bucket={fleetBucket}
            date={fleetDate}
            page={fleetPage}
            onFilterChange={changeFleetFilter}
            onQueryChange={changeFleetQuery}
            onRefresh={refreshNow}
            refreshing={refreshing}
          />
        ) : hasProjects(snapshot) ? (
          <Projects
            projects={snapshot.projects}
            sessions={snapshot.sessions}
            live={live}
            onToggleLive={() => setLive((v) => !v)}
            phase={status.state}
            attempt={status.attempt}
            stale={snapshot.stale}
            snapshotAgeMs={receivedAt === null ? null : Date.now() - receivedAt}
            kpis={snapshot.kpis}
            prevKpis={prevSnapshot?.kpis ?? null}
            lastSeq={snapshot.seq}
            onRefresh={refreshNow}
            refreshing={refreshing}
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
            harnessFilter={fleetHarness}
            bucket={fleetBucket}
            date={fleetDate}
            page={fleetPage}
            onFilterChange={changeFleetFilter}
            onQueryChange={changeFleetQuery}
            onRefresh={refreshNow}
            refreshing={refreshing}
          />
        )}
      </main>
      <CmdBar
        open={palette.open}
        sessions={snapshot?.sessions ?? []}
        onSelect={selectSession}
        onClose={() => palette.setOpen(false)}
      />
      <Toasts toasts={toasts} onDismiss={dismissToast} />
      <BottomBar paletteOpen={palette.open} live={live} />
    </div>
  );
}
