/**
 * Live pane tab: one on-demand `GET /api/sessions/:id/screen?full=1` fetch
 * when the tab mounts. No timer, no polling — reopening the tab or
 * navigating back is the only way to refresh.
 */

import { useEffect, useState } from "react";

type PaneState =
  | { phase: "loading" }
  | { phase: "ready"; text: string; paneId: string; capturedAt: string }
  | { phase: "db-only"; hint: string }
  | { phase: "timeout" }
  | { phase: "unavailable" }
  | { phase: "error" };

const DB_ONLY_FALLBACK =
  "session has no live zellij pane; transcript and DB data remain available";

function hintFromBody(value: unknown): string {
  if (typeof value === "object" && value !== null && !Array.isArray(value)) {
    const hint = (value as Record<string, unknown>).hint;
    if (typeof hint === "string" && hint !== "") return hint;
  }
  return DB_ONLY_FALLBACK;
}

export function ScreenTab({ sessionId }: { sessionId: string }) {
  const [state, setState] = useState<PaneState>({ phase: "loading" });

  useEffect(() => {
    const controller = new AbortController();
    let active = true;
    setState({ phase: "loading" });

    void (async () => {
      try {
        const response = await fetch(
          `/api/sessions/${encodeURIComponent(sessionId)}/screen?full=1`,
          { credentials: "same-origin", signal: controller.signal },
        );
        if (!active) return;
        if (response.status === 200) {
          const text = await response.text();
          if (!active) return;
          setState({
            phase: "ready",
            text,
            paneId: response.headers.get("X-Pane-Id") ?? "unknown",
            capturedAt: response.headers.get("X-Captured-At") ?? "unknown",
          });
          return;
        }
        if (response.status === 409) {
          const body: unknown = await response.json().catch(() => null);
          if (active) setState({ phase: "db-only", hint: hintFromBody(body) });
          return;
        }
        if (response.status === 504) {
          setState({ phase: "timeout" });
          return;
        }
        if (response.status === 503 || response.status === 404) {
          setState({ phase: "unavailable" });
          return;
        }
        setState({ phase: "error" });
      } catch {
        if (!active || controller.signal.aborted) return;
        setState({ phase: "error" });
      }
    })();

    return () => {
      active = false;
      controller.abort();
    };
  }, [sessionId]);

  if (state.phase === "loading") {
    return <div className="empty-state">loading live pane…</div>;
  }
  if (state.phase === "db-only") {
    return (
      <div className="pane-note" data-testid="pane-db-only">
        <span className="pane-note-label mono">DB-only</span>
        <span>{state.hint}</span>
      </div>
    );
  }
  if (state.phase === "timeout") {
    return (
      <div className="pane-note" data-testid="pane-timeout">
        <span className="pane-note-label mono">timeout</span>
        <span>
          The pane dump took too long. The session&apos;s transcript and DB
          tabs remain available.
        </span>
      </div>
    );
  }
  if (state.phase === "unavailable") {
    return (
      <div className="pane-note" data-testid="pane-unavailable">
        <span className="pane-note-label mono">unavailable</span>
        <span>
          No live pane is available for this session. Use the transcript or DB
          tabs.
        </span>
      </div>
    );
  }
  if (state.phase === "error") {
    return (
      <div className="pane-note" data-testid="pane-error">
        <span className="pane-note-label mono">error</span>
        <span>Could not load the live pane.</span>
      </div>
    );
  }
  return (
    <div className="pane-wrap">
      <div className="panel-head mono" data-testid="pane-caption">
        <span>live pane</span>
        <span>
          pane {state.paneId} · captured {state.capturedAt}
        </span>
      </div>
      <pre className="pane-pre mono" data-testid="pane-text">
        {state.text}
      </pre>
    </div>
  );
}
