/**
 * SessionDetail W3-2 tabs: Timeline / Tools / Cost / Raw + shared inspector.
 *
 * Data honesty: every figure derives from the session `info`/`messages`
 * blocks (live export or an injected fixture) via transcript.ts helpers
 * (messageTokens/messageCost) — no invented math. Each non-trivial formula
 * carries a `// =` comment citing the fields it sums. Charts are hand-rolled SVG/HTML (W3-3
 * precedent — no chart dep): zero animation, axes/ticks in mono 11px
 * `--ink-400`, max 5 hues from DESIGN tokens (documented maps below).
 * Every chart point carries its session/run id (data attrs + <title>).
 *
 * Layout: the run inspector is a right rail that pushes content (flex),
 * never a modal. Esc closes it via useInspectorEsc (window capture +
 * stopPropagation, firing only while open); TranscriptTab yields to it
 * via its `inspectorOpen` prop — chain: inspector → tail-resume → fleet.
 */

import { useEffect, useMemo, useRef, useState } from "react";
import { Copy, Link2, X } from "lucide-react";

import { buildSessionHash } from "../lib/routing";
import {
  buildTurns,
  computeWindow,
  messageCost,
  messageTokens,
  summarizeTurn,
} from "../lib/transcript";
import type {
  ExportMessage,
  ExportSession,
  ExportTokens,
  FlatMessage,
  ToolStatus,
  Turn,
} from "../lib/transcript";
import { isTypingTarget } from "../lib/useCommandPalette";
import { formatCompact, formatUSD } from "../lib/format";
import type { SessionDetailData } from "../lib/sessionProvider";

/* ---------------- shared: tool runs ---------------- */

export interface ToolRun {
  runId: string;
  callId: string;
  name: string;
  status: ToolStatus;
  title: string;
  input: unknown;
  output: string;
  startMs: number;
  endMs: number;
  durationMs: number;
  lane: "main" | "sub";
  agent: string;
  sessionId: string;
  messageId: string;
  /** Parent-message tokens (messageTokens); the export shape carries no per-tool tokens. */
  turnTokens: ExportTokens;
}

export function collectToolRuns(flat: FlatMessage[]): ToolRun[] {
  const runs: ToolRun[] = [];
  for (const entry of flat) {
    if (entry.message.info.role !== "assistant") continue;
    const turnTokens = messageTokens(entry.message);
    for (const part of entry.message.parts) {
      if (part.type !== "tool") continue;
      if (!("state" in part) || !("tool" in part)) continue;
      const start = part.state.time?.start ?? entry.timeCreated;
      const end = part.state.time?.end ?? start;
      runs.push({
        runId: part.id,
        callId: part.callID,
        name: part.tool,
        status: part.state.status,
        title: part.state.title ?? "",
        input: part.state.input,
        output: part.state.output ?? "",
        startMs: start,
        endMs: end,
        durationMs: Math.max(0, end - start),
        lane: entry.lane,
        agent: entry.agent,
        sessionId: entry.sessionId,
        messageId: entry.message.info.id,
        turnTokens,
      });
    }
  }
  return runs.sort((a, b) => a.startMs - b.startMs || (a.runId < b.runId ? -1 : 1));
}

/** Retry = earlier runs of the same tool in the same session (ordered by start, runId tiebreak). */
export function retryCount(run: ToolRun, runs: readonly ToolRun[]): number {
  return runs.filter(
    (o) =>
      o.sessionId === run.sessionId &&
      o.name === run.name &&
      (o.startMs < run.startMs || (o.startMs === run.startMs && o.runId < run.runId)),
  ).length;
}

/**
 * Tool hue mapping (documented, 4 hues + near-white inks which do not
 * count per DESIGN §2): read/search `--accent`, edit/write `--ink-100`,
 * exec `--ok`, other `--ink-200`. Status overrides: error `--err`,
 * running `--run`, pending `--mute`.
 */
export const TOOL_HUE: Record<string, string> = {
  read: "var(--accent)",
  edit: "var(--ink-100)",
  exec: "var(--ok)",
  other: "var(--ink-200)",
};

export function toolCategory(name: string): string {
  if (name === "read" || name === "grep" || name === "glob") return "read";
  if (name === "edit" || name === "write") return "edit";
  if (name === "bash") return "exec";
  return "other";
}

export function toolBarFill(run: ToolRun): string {
  if (run.status === "error") return "var(--err)";
  if (run.status === "running") return "var(--run)";
  if (run.status === "pending") return "var(--mute)";
  return TOOL_HUE[toolCategory(run.name)] ?? "var(--ink-200)";
}

export const TOOL_GLYPH_W32: Record<ToolStatus, { glyph: string; className: string }> = {
  completed: { glyph: "✓", className: "tok-ok" },
  error: { glyph: "✕", className: "tok-err" },
  running: { glyph: "●", className: "tok-run" },
  pending: { glyph: "◌", className: "tok-mute" },
};

function formatDuration(ms: number): string {
  if (!Number.isFinite(ms) || ms <= 0) return "0ms";
  if (ms < 1000) return `${Math.round(ms)}ms`;
  return `${(ms / 1000).toFixed(1)}s`;
}

function formatClock(epochMs: number): string {
  const date = new Date(epochMs);
  const pad = (n: number): string => String(n).padStart(2, "0");
  return `${pad(date.getHours())}:${pad(date.getMinutes())}:${pad(date.getSeconds())}`;
}

export function useInspectorEsc(open: boolean, onClose: () => void, paletteOpen: boolean): void {
  const closeRef = useRef(onClose);
  closeRef.current = onClose;
  useEffect(() => {
    if (!open) return;
    const onKeyDown = (event: KeyboardEvent): void => {
      if (event.key !== "Escape" || paletteOpen) return;
      if (isTypingTarget(event.target)) return;
      event.preventDefault();
      event.stopPropagation();
      closeRef.current();
    };
    window.addEventListener("keydown", onKeyDown, true);
    return () => window.removeEventListener("keydown", onKeyDown, true);
  }, [open, paletteOpen]);
}

/* ---------------- shared tab props (TAB_CONTENT registry) ---------------- */

export interface DetailExtraProps {
  flat: FlatMessage[];
  turns: Turn[];
  runs: ToolRun[];
  data: SessionDetailData;
  sessionId: string;
  messageId: string | null;
  selectedRun: ToolRun | null;
  paletteOpen: boolean;
  onSelectRun: (run: ToolRun) => void;
  onCloseInspector: () => void;
}

/* ---------------- Timeline tab ---------------- */

const TL_ROW_H = 26;

type TLRow =
  | { kind: "lane"; key: string; label: string; agent: string; lane: "main" | "sub"; count: number }
  | { kind: "run"; run: ToolRun };

export function TimelineTab({ flat, runs, selectedRun, onSelectRun }: DetailExtraProps): React.JSX.Element {
  const containerRef = useRef<HTMLDivElement | null>(null);
  const dragRef = useRef<{ anchor: number } | null>(null);
  const suppressClickRef = useRef(false);
  const [scrollTop, setScrollTop] = useState(0);
  const [viewportH, setViewportH] = useState(384);
  const [zoom, setZoom] = useState<{ f0: number; f1: number } | null>(null);
  const [sel, setSel] = useState<{ f0: number; f1: number } | null>(null);

  const full = useMemo(() => {
    let start = Number.POSITIVE_INFINITY;
    let end = Number.NEGATIVE_INFINITY;
    for (const run of runs) {
      if (run.startMs < start) start = run.startMs;
      if (run.endMs > end) end = run.endMs;
    }
    if (!Number.isFinite(start) || !Number.isFinite(end)) return null;
    if (end <= start) end = start + 1;
    return { start, end };
  }, [runs]);

  const view = useMemo(() => {
    if (full === null) return null;
    if (zoom === null) return full;
    const span = full.end - full.start;
    return { start: full.start + zoom.f0 * span, end: full.start + zoom.f1 * span };
  }, [full, zoom]);

  const rows: TLRow[] = useMemo(() => {
    const out: TLRow[] = [];
    const seen = new Set<string>();
    for (const run of runs) {
      const key = `${run.lane}:${run.sessionId}`;
      if (!seen.has(key)) {
        seen.add(key);
        const count = runs.filter((o) => o.lane === run.lane && o.sessionId === run.sessionId).length;
        out.push({
          kind: "lane",
          key,
          label: run.lane === "main" ? `main · ${run.agent}` : `↳ sub · ${run.agent}`,
          agent: run.agent,
          lane: run.lane,
          count,
        });
      }
      out.push({ kind: "run", run });
    }
    return out;
  }, [runs]);

  const tokenArea = useMemo(() => {
    if (view === null) return "";
    const turns = buildTurns(flat);
    const span = Math.max(1e-9, view.end - view.start);
    const pts = turns
      .map((t) => ({ t: t.startMs, v: messageTokens(t.response).total }))
      .filter((p) => p.t >= view.start && p.t <= view.end);
    if (pts.length === 0) return "";
    const max = Math.max(1, ...pts.map((p) => p.v));
    const x = (t: number): number => ((t - view.start) / span) * 1000;
    const y = (v: number): number => 40 - 4 - (v / max) * (40 - 8);
    if (pts.length === 1) {
      const only = pts[0];
      return `M 0,${y(only.v).toFixed(1)} L 1000,${y(only.v).toFixed(1)} L 1000,40 L 0,40 Z`;
    }
    const line = pts.map((p) => `L ${x(p.t).toFixed(1)},${y(p.v).toFixed(1)}`).join(" ");
    const first = pts[0];
    const last = pts[pts.length - 1];
    return `M ${x(first.t).toFixed(1)},40 ${line} L ${x(last.t).toFixed(1)},40 Z`;
  }, [flat, view]);

  useEffect(() => {
    const el = containerRef.current;
    if (el !== null && el.clientHeight > 0) setViewportH(el.clientHeight);
  }, []);

  const onScroll = (): void => {
    const el = containerRef.current;
    if (el === null) return;
    setScrollTop(el.scrollTop);
  };

  if (full === null || view === null) {
    return <div className="empty-state">No tool calls in this session yet.</div>;
  }

  const span = Math.max(1e-9, view.end - view.start);
  const leftPct = (t: number): number => ((t - view.start) / span) * 100;
  const widthPct = (d: number): number => (d / span) * 100;
  const fracFromEvent = (event: React.MouseEvent): number => {
    const rect = event.currentTarget.getBoundingClientRect();
    if (rect.width <= 0) return 0;
    return Math.min(1, Math.max(0, (event.clientX - rect.left) / rect.width));
  };

  const commitZoom = (a: number, b: number): void => {
    const f0 = Math.min(a, b);
    const f1 = Math.max(a, b);
    if (f1 - f0 > 0.02) setZoom({ f0, f1 });
    setSel(null);
  };

  const { start, end } = computeWindow(scrollTop, rows.length, viewportH, TL_ROW_H);
  const ticks = [0, 1, 2, 3, 4].map((i) => view.start + (i / 4) * span);

  return (
    <div className="tl-wrap">
      <div className="panel-head mono">
        <span>
          timeline · {runs.length} tool run{runs.length === 1 ? "" : "s"}
        </span>
        {zoom !== null ? (
          <button type="button" className="tl-zoom-reset mono" onClick={() => setZoom(null)}>
            reset zoom
          </button>
        ) : (
          <span>drag to zoom · double-click resets</span>
        )}
      </div>
      <div className="tl-axis mono" aria-hidden="true">
        {ticks.map((t) => (
          <span key={t}>{formatClock(t)}</span>
        ))}
      </div>
      <div
        ref={containerRef}
        className="tl-scroll"
        tabIndex={0}
        role="list"
        aria-label="Tool timeline"
        onScroll={onScroll}
        onMouseDown={(e) => {
          dragRef.current = { anchor: fracFromEvent(e) };
          setSel(null);
        }}
        onMouseMove={(e) => {
          if (dragRef.current === null) return;
          const cursor = fracFromEvent(e);
          setSel({ f0: dragRef.current.anchor, f1: cursor });
        }}
        onMouseUp={(e) => {
          if (dragRef.current === null) return;
          const anchor = dragRef.current.anchor;
          dragRef.current = null;
          const cursor = fracFromEvent(e);
          if (Math.abs(cursor - anchor) > 0.02) suppressClickRef.current = true;
          commitZoom(anchor, cursor);
        }}
        onDoubleClick={() => setZoom(null)}
      >
        <div style={{ height: rows.length * TL_ROW_H, position: "relative" }}>
          <div style={{ height: start * TL_ROW_H }} aria-hidden="true" />
          <svg
            className="tl-underlay"
            viewBox="0 0 1000 40"
            preserveAspectRatio="none"
            aria-hidden="true"
            focusable="false"
          >
            {tokenArea !== "" && (
              <path
                d={tokenArea}
                style={{ fill: "var(--wash-accent)", stroke: "var(--accent)" }}
                strokeWidth={1}
                vectorEffect="non-scaling-stroke"
              />
            )}
          </svg>
          {rows.slice(start, end).map((row) => {
            if (row.kind === "lane") {
              return (
                <div key={row.key} className="tl-lane mono" style={{ height: TL_ROW_H }} role="listitem">
                  <span className={`lane-chip lane-${row.lane}`}>
                    <span className="lane-glyph" aria-hidden="true">
                      {row.agent.charAt(0).toUpperCase()}
                    </span>
                    <span>{row.label}</span>
                  </span>
                  <span className="tl-lane-count">
                    {row.count} run{row.count === 1 ? "" : "s"}
                  </span>
                </div>
              );
            }
            const run = row.run;
            const spec = TOOL_GLYPH_W32[run.status];
            const selected = selectedRun?.runId === run.runId;
            return (
              <button
                key={run.runId}
                type="button"
                role="listitem"
                data-testid={`tl-row-${run.runId}`}
                aria-current={selected}
                className={`tl-run mono${selected ? " tl-run-sel" : ""}${run.lane === "sub" ? " tl-run-sub" : ""}`}
                style={{ height: TL_ROW_H }}
                title={`${run.name} · ${run.status} · ${formatDuration(run.durationMs)} · ${run.sessionId}`}
                onClick={() => {
                  if (suppressClickRef.current) {
                    suppressClickRef.current = false;
                    return;
                  }
                  onSelectRun(run);
                }}
              >
                <span className="tl-time">{formatClock(run.startMs)}</span>
                <span className="tl-track">
                  <span
                    data-testid={`tl-bar-${run.runId}`}
                    className="tl-bar"
                    style={{
                      left: `${leftPct(run.startMs).toFixed(3)}%`,
                      width: `${widthPct(run.durationMs).toFixed(3)}%`,
                      background: toolBarFill(run),
                    }}
                  />
                </span>
                <span className={`tl-glyph ${spec.className}`} aria-hidden="true">
                  {spec.glyph}
                </span>
                <span className="tl-dur">{formatDuration(run.durationMs)}</span>
              </button>
            );
          })}
          <div style={{ height: (rows.length - end) * TL_ROW_H }} aria-hidden="true" />
          {sel !== null && (
            <div
              className="tl-sel"
              aria-hidden="true"
              style={{
                left: `${Math.min(sel.f0, sel.f1) * 100}%`,
                width: `${Math.abs(sel.f1 - sel.f0) * 100}%`,
              }}
            />
          )}
        </div>
      </div>
    </div>
  );
}

/* ---------------- Tools tab + inspector ---------------- */

function TopBlock({
  title,
  entries,
  max,
  empty,
}: {
  title: string;
  entries: { key: string; label: string; detail: string; fill: string; glyph: string; glyphClass: string; value: number }[];
  max: number;
  empty: string;
}): React.JSX.Element {
  return (
    <section className="tools-sum" aria-label={title}>
      <div className="panel-head mono">
        <span>{title}</span>
      </div>
      {entries.length === 0 ? (
        <div className="tools-sum-empty">{empty}</div>
      ) : (
        <ol className="tools-sum-list">
          {entries.map((e) => (
            <li key={e.key} className="tools-sum-row mono">
              <span className={e.glyphClass} aria-hidden="true">
                {e.glyph}
              </span>
              <span className="tools-sum-label">{e.label}</span>
              <span className="tools-sum-track">
                <span
                  className="tools-sum-bar"
                  style={{ width: `${(max > 0 ? (e.value / max) * 100 : 0).toFixed(1)}%`, background: e.fill }}
                />
              </span>
              <span className="tools-sum-detail">{e.detail}</span>
            </li>
          ))}
        </ol>
      )}
    </section>
  );
}

export function RunInspector({
  run,
  runs,
  onClose,
}: {
  run: ToolRun;
  runs: readonly ToolRun[];
  onClose: () => void;
}): React.JSX.Element {
  const spec = TOOL_GLYPH_W32[run.status];
  const retries = retryCount(run, runs);
  const output = run.output;
  const excerpt = output.length > 400 ? `${output.slice(0, 400)}… (${output.length - 400} more chars)` : output;
  let inputNode: React.JSX.Element;
  if (run.input === undefined) {
    inputNode = <span className="insp-mute">(no input recorded)</span>;
  } else if (typeof run.input === "object" && run.input !== null) {
    inputNode = <JsonValue value={run.input} />;
  } else {
    inputNode = <span>{String(run.input)}</span>;
  }
  return (
    <aside className="inspector" aria-label="Tool run inspector" data-testid={`inspector-${run.runId}`}>
      <div className="inspector-head">
        <span className={`tool-glyph ${spec.className}`} aria-hidden="true">
          {spec.glyph}
        </span>
        <span className="inspector-title mono">{run.name}</span>
        <span className="tool-status mono">{run.status}</span>
        <button type="button" className="inspector-close" aria-label="Close inspector" onClick={onClose}>
          <X aria-hidden="true" size={12} />
        </button>
      </div>
      <dl className="inspector-facts mono">
        <div>
          <dt>run</dt>
          <dd>{run.runId}</dd>
        </div>
        <div>
          <dt>message</dt>
          <dd>{run.messageId}</dd>
        </div>
        <div>
          <dt>session</dt>
          <dd>{run.sessionId}</dd>
        </div>
        <div>
          <dt>timing</dt>
          <dd>
            {formatClock(run.startMs)} → {formatClock(run.endMs)} · {formatDuration(run.durationMs)}
          </dd>
        </div>
        <div>
          <dt>retry</dt>
          {/* = earlier same-name runs in the same session (retryCount) */}
          <dd>
            attempt {retries + 1} · {retries} earlier
          </dd>
        </div>
        <div>
          <dt>turn tokens</dt>
          {/* = messageTokens(parent message): the export shape carries no per-tool tokens */}
          <dd>
            in {formatCompact(run.turnTokens.input)} / out {formatCompact(run.turnTokens.output)} /{" "}
            {formatCompact(run.turnTokens.total)}
          </dd>
        </div>
      </dl>
      {run.title !== "" && <p className="inspector-title-line">{run.title}</p>}
      <section aria-label="Tool input">
        <div className="panel-head mono">
          <span>input</span>
        </div>
        <div className="inspector-io mono">{inputNode}</div>
      </section>
      <section aria-label="Tool output">
        <div className="panel-head mono">
          <span>output</span>
        </div>
        <div className="inspector-io mono">{excerpt === "" ? <span className="insp-mute">(empty)</span> : excerpt}</div>
      </section>
      {run.status === "error" && (
        <section aria-label="Tool error">
          <div className="panel-head mono">
            <span>error</span>
          </div>
          <div className="inspector-err mono">{run.output === "" ? "(no error text)" : run.output}</div>
        </section>
      )}
    </aside>
  );
}

export function ToolsTab({ runs, selectedRun, onSelectRun }: DetailExtraProps): React.JSX.Element {
  if (runs.length === 0) {
    return <div className="empty-state">No tool calls in this session yet.</div>;
  }
  const errors = runs.filter((r) => r.status === "error").sort((a, b) => b.durationMs - a.durationMs).slice(0, 5);
  const slowest = [...runs].sort((a, b) => b.durationMs - a.durationMs).slice(0, 5);
  const maxDur = Math.max(1e-9, ...runs.map((r) => r.durationMs));
  return (
    <div className="tools-main">
        <div className="tools-sums">
          <TopBlock
            title="top errors"
            empty="No errors recorded."
            max={maxDur}
            entries={errors.map((r) => {
              const spec = TOOL_GLYPH_W32[r.status];
              return {
                key: r.runId,
                label: r.name,
                detail: formatDuration(r.durationMs),
                fill: "var(--err)",
                glyph: spec.glyph,
                glyphClass: spec.className,
                value: r.durationMs,
              };
            })}
          />
          <TopBlock
            title="top latency"
            empty="No runs recorded."
            max={maxDur}
            entries={slowest.map((r) => {
              const spec = TOOL_GLYPH_W32[r.status];
              return {
                key: r.runId,
                label: r.name,
                detail: formatDuration(r.durationMs),
                fill: toolBarFill(r),
                glyph: spec.glyph,
                glyphClass: spec.className,
                value: r.durationMs,
              };
            })}
          />
        </div>
        <div className="tools-table" role="table" aria-label="Tool calls">
          <div className="tools-head mono" role="row">
            <span>tool</span>
            <span>status</span>
            <span>duration</span>
            <span>turn in/out</span>
            <span>lane</span>
          </div>
          {runs.map((run) => {
            const spec = TOOL_GLYPH_W32[run.status];
            const selected = selectedRun?.runId === run.runId;
            return (
              <button
                key={run.runId}
                type="button"
                role="row"
                data-testid={`tool-row-${run.runId}`}
                aria-current={selected}
                className={`tools-row mono${selected ? " tools-row-sel" : ""}`}
                title={`${run.name} · ${run.status} · ${run.sessionId}`}
                onClick={() => onSelectRun(run)}
              >
                <span className="tool-name">{run.name}</span>
                <span>
                  <span className={`tool-glyph ${spec.className}`} aria-hidden="true">
                    {spec.glyph}
                  </span>{" "}
                  <span className="tool-status">{run.status}</span>
                </span>
                <span>{formatDuration(run.durationMs)}</span>
                <span>
                  {formatCompact(run.turnTokens.input)} / {formatCompact(run.turnTokens.output)}
                </span>
                <span className="tool-lane">
                  {run.lane === "sub" ? `↳ ${run.agent}` : run.agent}
                </span>
              </button>
            );
          })}
        </div>
    </div>
  );
}

/* ---------------- Cost tab ---------------- */

/** Fixed context window: the export shape carries no context-limit field; the used value derives from info.tokens. */
export const CONTEXT_TOKENS = 200_000;

/** Donut slice order per DESIGN §8 (stays within the 5-hue budget). */
export const DONUT_HUES = [
  "var(--accent)",
  "var(--ink-200)",
  "var(--ok)",
  "var(--warn)",
  "var(--mute)",
] as const;

function modelLabel(data: SessionDetailData, turn: Turn): string {
  if (turn.response.info.modelID !== undefined) {
    return `${turn.response.info.providerID ?? "?"}/${turn.response.info.modelID}`;
  }
  const sess: ExportSession | undefined =
    turn.sessionId === data.detail.info.id
      ? data.detail
      : data.children.find((c) => c.info.id === turn.sessionId);
  return `${sess?.info.model.providerID ?? "?"}/${sess?.info.model.id ?? "?"}`;
}

export function CostTab({ data, flat }: DetailExtraProps): React.JSX.Element {
  const turns = useMemo(() => buildTurns(flat), [flat]);
  const perTurn = useMemo(
    () =>
      turns.map((t) => {
        const s = summarizeTurn(t);
        return { turn: t, cost: s.cost, tokens: s.tokens };
      }),
    [turns],
  );
  // cumulative[i] = Σ messageCost(turns[0..i]) — same source as the export-parity test
  const cumulative = useMemo(() => {
    let acc = 0;
    return perTurn.map((p) => {
      acc += p.cost;
      return acc;
    });
  }, [perTurn]);
  // totalCost = Σ info.cost over detail + children (matches the shell header math)
  const totalCost = data.detail.info.cost + data.children.reduce((s, c) => s + c.info.cost, 0);
  // totalTokens = Σ info.tokens.total over detail + children
  const totalTokens =
    data.detail.info.tokens.total + data.children.reduce((s, c) => s + c.info.tokens.total, 0);
  const maxCost = Math.max(1e-9, ...cumulative);
  const maxTokens = Math.max(1, ...perTurn.map((p) => p.tokens.input + p.tokens.output));
  const modelCosts = useMemo(() => {
    const map = new Map<string, number>();
    for (const p of perTurn) {
      const key = modelLabel(data, p.turn);
      map.set(key, (map.get(key) ?? 0) + p.cost);
    }
    return [...map.entries()].sort((a, b) => b[1] - a[1]);
  }, [data, perTurn]);
  // donutPct = modelCost / totalCost * 100 (sums to 100 by construction)
  const donutTotal = modelCosts.reduce((s, [, c]) => s + c, 0);
  const usedPct = (totalTokens / CONTEXT_TOKENS) * 100;
  const nearLimit = usedPct > 85;

  const linePts = perTurn
    .map((_, i) => {
      const n = perTurn.length;
      const x = n === 1 ? 280 : (i / (n - 1)) * 560;
      const y = 116 - (cumulative[i] / maxCost) * 108;
      return `${x.toFixed(1)},${y.toFixed(1)}`;
    })
    .join(" ");

  return (
    <div className="cost-grid">
      <section className="cost-panel" aria-label="Cumulative cost">
        <div className="panel-head mono">
          <span>cumulative cost</span>
          {/* = last running Σ messageCost == Σ info.cost */}
          <span data-testid="cost-total">{formatUSD(totalCost)}</span>
        </div>
        <svg className="cost-chart" viewBox="0 0 560 120" role="img" aria-label="Cumulative cost line">
          <polyline points={linePts} style={{ fill: "none", stroke: "var(--accent)", strokeWidth: 1.5 }} />
          {perTurn.map((p, i) => {
            const n = perTurn.length;
            const cx = n === 1 ? 280 : (i / (n - 1)) * 560;
            const cy = 116 - (cumulative[i] / maxCost) * 108;
            return (
              <circle
                key={p.turn.id}
                cx={cx}
                cy={cy}
                r={3.5}
                data-session-id={p.turn.sessionId}
                data-testid={`cost-point-${p.turn.id}`}
                style={{ fill: "var(--accent)", stroke: "none" }}
              >
                <title>{`${p.turn.sessionId} · ${p.turn.id} · ${formatUSD(cumulative[i])}`}</title>
              </circle>
            );
          })}
        </svg>
        <div className="cost-axis mono">
          <span>oldest turn</span>
          <span>latest turn</span>
        </div>
      </section>

      <section className="cost-panel" aria-label="Tokens per turn">
        <div className="panel-head mono">
          <span>tokens in/out per turn</span>
          <span>
            in {formatCompact(perTurn.reduce((s, p) => s + p.tokens.input, 0))} / out{" "}
            {formatCompact(perTurn.reduce((s, p) => s + p.tokens.output, 0))}
          </span>
        </div>
        <div className="cost-bars" role="img" aria-label="Stacked input/output tokens per turn">
          {perTurn.map((p) => {
            const inH = (p.tokens.input / maxTokens) * 100;
            const outH = (p.tokens.output / maxTokens) * 100;
            return (
              <div
                key={p.turn.id}
                className="cost-bar"
                data-session-id={p.turn.sessionId}
                title={`${p.turn.id} · in ${p.tokens.input} / out ${p.tokens.output}`}
              >
                {/* stacked: input var(--ink-200) + output var(--ink-400) per DESIGN §8 */}
                <span className="cost-bar-out" style={{ height: `${outH.toFixed(1)}%` }} />
                <span className="cost-bar-in" style={{ height: `${inH.toFixed(1)}%` }} />
              </div>
            );
          })}
        </div>
        <div className="cost-legend mono">
          <span className="compare-legend-item">
            <span className="compare-swatch" aria-hidden="true" style={{ background: "var(--ink-200)" }} />
            <span>input</span>
          </span>
          <span className="compare-legend-item">
            <span className="compare-swatch" aria-hidden="true" style={{ background: "var(--ink-400)" }} />
            <span>output</span>
          </span>
        </div>
      </section>

      <section className="cost-panel" aria-label="Cost by model">
        <div className="panel-head mono">
          <span>cost by model</span>
        </div>
        <div className="cost-donut-row">
          <svg className="cost-donut" viewBox="0 0 120 120" role="img" aria-label="Cost by model donut">
            <circle cx={60} cy={60} r={45} style={{ fill: "none", stroke: "var(--ink-700)" }} strokeWidth={14} />
            {modelCosts.map(([model, cost], i) => {
              const pct = donutTotal > 0 ? (cost / donutTotal) * 100 : 0;
              const offset = modelCosts.slice(0, i).reduce((s, [, c]) => s + (donutTotal > 0 ? (c / donutTotal) * 100 : 0), 0);
              return (
                <circle
                  key={model}
                  cx={60}
                  cy={60}
                  r={45}
                  pathLength={100}
                  strokeDasharray={`${pct.toFixed(2)} ${(100 - pct).toFixed(2)}`}
                  strokeDashoffset={-offset}
                  transform="rotate(-90 60 60)"
                  data-testid={`donut-slice-${i}`}
                  style={{
                    fill: "none",
                    stroke: DONUT_HUES[i % DONUT_HUES.length],
                    strokeWidth: 14,
                  }}
                >
                  <title>{`${model} · ${formatUSD(cost)} · ${pct.toFixed(1)}%`}</title>
                </circle>
              );
            })}
            <text x={60} y={58} textAnchor="middle" className="cost-donut-total">
              {formatUSD(totalCost)}
            </text>
            <text x={60} y={72} textAnchor="middle" className="cost-donut-sub">
              total
            </text>
          </svg>
          <ul className="cost-legend-list mono">
            {modelCosts.map(([model, cost], i) => {
              const pct = donutTotal > 0 ? (cost / donutTotal) * 100 : 0;
              return (
                <li key={model}>
                  <span
                    className="compare-swatch"
                    aria-hidden="true"
                    style={{ background: DONUT_HUES[i % DONUT_HUES.length] }}
                  />
                  {/* provider/model labels are long and unbroken — ellipsize
                      in the box, full label on hover (same pattern as the
                      donut slice <title>). */}
                  <span className="cost-model-label" title={model}>
                    {model}
                  </span>
                  <span data-pct={pct.toFixed(2)} data-testid={`donut-pct-${i}`}>
                    {pct.toFixed(1)}%
                  </span>
                  <span>{formatUSD(cost)}</span>
                </li>
              );
            })}
          </ul>
        </div>
      </section>

      <section className="cost-panel" aria-label="Context window">
        <div className="panel-head mono">
          <span>context window</span>
          <span data-testid="cost-context">
            {formatCompact(totalTokens)} / {formatCompact(CONTEXT_TOKENS)}
          </span>
        </div>
        <div className="cost-donut-row">
          <svg className="cost-donut" viewBox="0 0 120 120" role="img" aria-label="Context window usage">
            <circle cx={60} cy={60} r={45} style={{ fill: "none", stroke: "var(--ink-700)" }} strokeWidth={14} />
            <circle
              cx={60}
              cy={60}
              r={45}
              pathLength={100}
              strokeDasharray={`${usedPct.toFixed(2)} ${(100 - usedPct).toFixed(2)}`}
              strokeDashoffset={0}
              transform="rotate(-90 60 60)"
              data-testid="context-ring"
              style={{
                fill: "none",
                stroke: nearLimit ? "var(--warn)" : "var(--accent)",
                strokeWidth: 14,
              }}
            >
              <title>{`used ${formatCompact(totalTokens)} of ${formatCompact(CONTEXT_TOKENS)}`}</title>
            </circle>
            <text x={60} y={58} textAnchor="middle" className="cost-donut-total">
              {usedPct.toFixed(1)}%
            </text>
            <text x={60} y={72} textAnchor="middle" className="cost-donut-sub">
              {nearLimit ? "NEAR LIMIT" : "used"}
            </text>
          </svg>
          {nearLimit && (
            <span className="tok-warn mono" aria-label="context window near limit">
              ▲ NEAR LIMIT
            </span>
          )}
        </div>
      </section>

      <section className="cost-panel cost-wide" aria-label="Cost by session">
        <div className="panel-head mono">
          <span>per-session roll-up</span>
        </div>
        <div className="tools-table" role="table" aria-label="Per-session cost roll-up">
          <div className="tools-head mono" role="row">
            <span>session</span>
            <span>agent</span>
            <span>model</span>
            <span>tokens</span>
            <span>cost</span>
          </div>
          {[data.detail, ...data.children].map((sess) => (
            <div key={sess.info.id} className="tools-row mono" role="row" data-testid={`rollup-${sess.info.id}`}>
              {/* = info.tokens.total / info.cost per session (no math beyond the sum in the footer) */}
              <span>{sess.info.id}</span>
              <span>{sess.info.agent}</span>
              <span>
                {sess.info.model.providerID}/{sess.info.model.id}
              </span>
              <span>{formatCompact(sess.info.tokens.total)}</span>
              <span>{formatUSD(sess.info.cost)}</span>
            </div>
          ))}
          <div className="tools-row mono tools-row-total" role="row">
            <span>total</span>
            <span>—</span>
            <span>—</span>
            <span data-testid="rollup-total-tokens">{formatCompact(totalTokens)}</span>
            <span data-testid="rollup-total-cost">{formatUSD(totalCost)}</span>
          </div>
        </div>
      </section>
    </div>
  );
}

/* ---------------- Raw JSON tab ---------------- */

/**
 * Raw JSON hue map (documented, 4 hues): keys `--ink-400`, strings
 * `--ok`, numbers `--accent`, booleans `--warn`, null `--mute`.
 */
export function JsonValue({ value }: { value: unknown }): React.JSX.Element {
  if (value === null) return <span style={{ color: "var(--mute)" }}>null</span>;
  if (typeof value === "string") return <span style={{ color: "var(--ok)" }}>{JSON.stringify(value)}</span>;
  if (typeof value === "number") return <span style={{ color: "var(--accent)" }}>{String(value)}</span>;
  if (typeof value === "boolean") return <span style={{ color: "var(--warn)" }}>{String(value)}</span>;
  if (Array.isArray(value)) {
    if (value.length === 0) return <span style={{ color: "var(--ink-400)" }}>[]</span>;
    return (
      <span>
        <span style={{ color: "var(--ink-400)" }}>[</span>
        <span className="raw-indent">
          {value.map((v, i) => (
            <span key={i} className="raw-line">
              <JsonValue value={v} />
              {i < value.length - 1 && <span style={{ color: "var(--ink-400)" }}>,</span>}
            </span>
          ))}
        </span>
        <span style={{ color: "var(--ink-400)" }}>]</span>
      </span>
    );
  }
  if (typeof value === "object") {
    const entries = Object.entries(value as Record<string, unknown>);
    if (entries.length === 0) return <span style={{ color: "var(--ink-400)" }}>{"{}"}</span>;
    return (
      <span>
        <span style={{ color: "var(--ink-400)" }}>{"{"}</span>
        <span className="raw-indent">
          {entries.map(([k, v], i) => (
            <span key={k} className="raw-line">
              <span style={{ color: "var(--ink-400)" }}>"{k}"</span>
              <span style={{ color: "var(--ink-400)" }}>: </span>
              <JsonValue value={v} />
              {i < entries.length - 1 && <span style={{ color: "var(--ink-400)" }}>,</span>}
            </span>
          ))}
        </span>
        <span style={{ color: "var(--ink-400)" }}>{"}"}</span>
      </span>
    );
  }
  return <span>{String(value)}</span>;
}

function RawMessage({
  message,
  sessionId,
  viewerId,
  targeted,
  forcedOpen,
}: {
  message: ExportMessage;
  sessionId: string;
  viewerId: string;
  targeted: boolean;
  forcedOpen: boolean;
}): React.JSX.Element {
  const [open, setOpen] = useState(true);
  const [copied, setCopied] = useState(false);
  useEffect(() => {
    if (forcedOpen) setOpen(true);
  }, [forcedOpen]);
  const expanded = forcedOpen || open;
  const onCopy = (): void => {
    const text = JSON.stringify(message, null, 2);
    const done = (): void => setCopied(true);
    try {
      const clip = typeof navigator !== "undefined" ? navigator.clipboard : undefined;
      if (clip) void clip.writeText(text).then(done, done);
      else done();
    } catch {
      done();
    }
  };
  return (
    <section
      id={`raw-msg-${message.info.id}`}
      data-testid={`raw-msg-${message.info.id}`}
      className={`raw-msg${targeted ? " raw-target" : ""}`}
      aria-label={`Raw message ${message.info.id}`}
    >
      <div className="raw-head mono">
        <button
          type="button"
          data-testid={`raw-toggle-${message.info.id}`}
          aria-expanded={expanded}
          className="raw-toggle"
          onClick={() => setOpen((v) => !v)}
        >
          <span aria-hidden="true">{expanded ? "▾" : "▸"}</span>
          <span className="raw-role">{message.info.role}</span>
          <span className="raw-id">{message.info.id}</span>
        </button>
        <span className="tool-title">
          {formatClock(message.info.time.created)} · {sessionId} ·{" "}
          {formatCompact(messageTokens(message).total)} tok · {formatUSD(messageCost(message))}
        </span>
        <a
          href={buildSessionHash(viewerId, "raw", message.info.id)}
          className="raw-anchor"
          data-testid={`raw-anchor-${message.info.id}`}
          title="Deep link to this message"
        >
          <Link2 aria-hidden="true" size={12} />
        </a>
        <button
          type="button"
          className="raw-copy mono"
          data-testid={`raw-copy-${message.info.id}`}
          onClick={onCopy}
          title="Copy message JSON"
        >
          <Copy aria-hidden="true" size={12} />
          <span>{copied ? "copied" : "copy"}</span>
        </button>
      </div>
      {expanded && (
        <pre className="raw-pre mono" data-testid={`raw-pre-${message.info.id}`}>
          <JsonValue value={message} />
        </pre>
      )}
    </section>
  );
}

export function RawTab({ data, sessionId, messageId }: DetailExtraProps): React.JSX.Element {
  const sessions = useMemo(() => [data.detail, ...data.children], [data]);
  const targetRef = useRef<string | null>(null);
  useEffect(() => {
    if (messageId === null) return;
    if (targetRef.current === messageId) return;
    targetRef.current = messageId;
    const el = document.getElementById(`raw-msg-${messageId}`);
    if (el !== null && typeof el.scrollIntoView === "function") {
      el.scrollIntoView({ block: "nearest" });
    }
  }, [messageId]);
  return (
    <div className="raw-wrap">
      {sessions.map((sess) => (
        <section key={sess.info.id} className="raw-sec" aria-label={`Raw session ${sess.info.id}`}>
          <div className="panel-head mono">
            <span>
              {sess.info.id} · {sess.info.agent} · {sess.messages.length} message
              {sess.messages.length === 1 ? "" : "s"}
            </span>
          </div>
          {sess.messages.map((message) => (
            <RawMessage
              key={message.info.id}
              message={message}
              sessionId={sess.info.id}
              viewerId={sessionId}
              targeted={messageId === message.info.id}
              forcedOpen={messageId === message.info.id}
            />
          ))}
        </section>
      ))}
    </div>
  );
}
