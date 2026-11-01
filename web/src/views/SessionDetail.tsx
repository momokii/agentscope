/**
 * SessionDetail (W3-1): routed drill-down `#/sessions/:id[/:tab]` with
 * Transcript (default) + Turns tabs.
 *
 * TAB REGISTRY (W3-2 handoff — read this): `DETAIL_TABS` below is the ONLY
 * place that declares tabs. W3-2 adds Timeline/Tools/Cost/Raw by flipping
 * `enabled` to true on those entries and adding their components to
 * `TAB_CONTENT` — no routing changes (segments already parse), no changes
 * to Transcript/Turns code. Disabled stubs render "lands in W3-2".
 *
 * W3-2 landed: all four tabs enabled; TAB_CONTENT maps each extra tab to
 * its component in SessionDetailTabs.tsx (uniform DetailExtraProps).
 * Yield chain: the shell Esc handler (window capture) closes the run
 * inspector first and stops propagation — TranscriptTab yields while it
 * is open (inspectorOpen prop), and App's bubble-phase Esc→fleet only
 * runs when the inspector is closed.
 *
 * DATA SEAM: reads through `SessionDataProvider`, defaulting to the live
 * export route (`liveProvider` → `/api/sessions/:id/export`). Tests inject
 * the committed fixture explicitly. Unknown id / fetch failure renders the
 * empty state, never invented detail.
 *
 * KEYBOARD: Esc resumes tail-follow. The listener runs on the CAPTURE phase
 * and stops propagation ONLY when it actually resumes (pinned or unseen),
 * so App's bubble-phase Esc→fleet still works otherwise; it also yields
 * when the command palette is open (`paletteOpen`) or when typing.
 */

import { useEffect, useMemo, useRef, useState } from "react";
import { ArrowDown, ArrowLeft, ChevronDown, Download, Search } from "lucide-react";
import { downloadJson, exportFilename } from "../lib/download";

import StatusBadge from "../components/StatusBadge";
import DataGuide from "../components/DataGuide";
import HarnessBadge from "../components/HarnessBadge";
import RelativeTime from "../components/RelativeTime";
import SubagentPanel from "../components/SubagentPanel";
import { formatCompact, formatUSD } from "../lib/format";
import { isTypingTarget } from "../lib/useCommandPalette";
import { buildFleetHash, buildSessionHash } from "../lib/routing";
import type { SessionTab } from "../lib/routing";
import type { SessionRow } from "../lib/sse";
import {
  buildTurns,
  computeWindow,
  firstText,
  flatMessageText,
  flattenTurns,
  isNearBottom,
  messageCost,
  messageDurationMs,
  messageTokens,
  summarizeTurn,
  EST_ROW_HEIGHT_PX,
} from "../lib/transcript";
import type {
  ExportMessage,
  FlatMessage,
  ToolStatus,
  Turn,
} from "../lib/transcript";
import { liveProvider } from "../lib/sessionProvider";
import type {
  SessionDataProvider,
  SessionDetailData,
} from "../lib/sessionProvider";
import {
  collectToolRuns,
  CostTab,
  RawTab,
  RunInspector,
  TimelineTab,
  ToolsTab,
  useInspectorEsc,
} from "./SessionDetailTabs";
import type { DetailExtraProps, ToolRun } from "./SessionDetailTabs";
import { ScreenTab } from "./ScreenTab";

/* ---------------- tab registry (W3-2 edits ONLY this + TAB_CONTENT) ---------------- */

export interface TabDef {
  id: SessionTab;
  label: string;
  enabled: boolean;
}

export const DETAIL_TABS: TabDef[] = [
  { id: "transcript", label: "Transcript", enabled: true },
  { id: "turns", label: "Turns", enabled: true },
  { id: "timeline", label: "Timeline", enabled: true },
  { id: "tools", label: "Tools", enabled: true },
  { id: "cost", label: "Cost", enabled: true },
  { id: "raw", label: "Raw", enabled: true },
  { id: "screen", label: "Live pane", enabled: true },
];

/** Extra-tab registry: tab id → component (uniform DetailExtraProps). */
export const TAB_CONTENT = {
  timeline: TimelineTab,
  tools: ToolsTab,
  cost: CostTab,
  raw: RawTab,
  screen: ScreenTab,
} as const;

export function DetailTabBar({ id, active }: { id: string; active: SessionTab }) {
  return (
    <nav className="dtabbar" role="tablist" aria-label="Session detail views">
      {DETAIL_TABS.map((tab) =>
        tab.enabled ? (
          <a
            key={tab.id}
            role="tab"
            aria-selected={active === tab.id}
            className={`dtab${active === tab.id ? " dtab-active" : ""}`}
            href={buildSessionHash(id, tab.id)}
          >
            {tab.label}
          </a>
        ) : (
          <span
            key={tab.id}
            role="tab"
            aria-selected={false}
            aria-disabled="true"
            className="dtab dtab-stub"
            title="lands in W3-2"
          >
            {tab.label}
            <em className="dtab-lands">lands in W3-2</em>
          </span>
        ),
      )}
    </nav>
  );
}

/* ---------------- small render helpers ---------------- */

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

const TOOL_GLYPH: Record<ToolStatus, { glyph: string; className: string }> = {
  completed: { glyph: "✓", className: "tok-ok" },
  error: { glyph: "✕", className: "tok-err" },
  running: { glyph: "●", className: "tok-run" },
  pending: { glyph: "◌", className: "tok-mute" },
};

function AgentLaneChip({ agent, lane }: { agent: string; lane: "main" | "sub" }) {
  const letter = agent.charAt(0).toUpperCase() || "?";
  return (
    <span
      className={`lane-chip lane-${lane}`}
      title={lane === "main" ? `main session agent ${agent}` : `subagent ${agent}`}
    >
      <span className="lane-glyph" aria-hidden="true">
        {letter}
      </span>
      <span>{agent}</span>
    </span>
  );
}

function ReasoningBlock({ text }: { text: string }) {
  const [open, setOpen] = useState(false);
  return (
    <div className="reasoning">
      <button
        type="button"
        className="reasoning-toggle"
        aria-expanded={open}
        onClick={() => setOpen((v) => !v)}
      >
        <ChevronDown
          aria-hidden="true"
          size={12}
          className={open ? "chev-open" : "chev-shut"}
        />
        <span>reasoning</span>
      </button>
      {open && <p className="reasoning-body">{text}</p>}
    </div>
  );
}

function MessageRow({ entry, highlight }: { entry: FlatMessage; highlight: boolean }) {
  const { message } = entry;
  const role = message.info.role;
  return (
    <article
      id={`turn-${message.info.id}`}
      data-testid={`tmsg-${message.info.id}`}
      className={`tmsg tmsg-${role}${highlight ? " tmsg-hl" : ""}`}
    >
      <header className="tmsg-head mono">
        <AgentLaneChip agent={entry.agent} lane={entry.lane} />
        <span className="tmsg-role">{role === "user" ? "USER" : "ASSISTANT"}</span>
        <time className="tmsg-time" dateTime={new Date(entry.timeCreated).toISOString()}>
          {formatClock(entry.timeCreated)}
        </time>
      </header>
      <div className="tmsg-body">
        {message.parts.map((part) => {
          const key = part.id;
          switch (part.type) {
            case "text":
              return (
                <p key={key} className="tmsg-text">
                  {"text" in part ? part.text : ""}
                </p>
              );
            case "reasoning":
              return (
                <ReasoningBlock
                  key={key}
                  text={"text" in part ? part.text : ""}
                />
              );
            case "tool": {
              if (!("state" in part) || !("tool" in part)) {
                return (
                  <div key={key} className="step-row mono">
                    <span>part · {part.type}</span>
                  </div>
                );
              }
              const status: ToolStatus = part.state.status;
              const spec = TOOL_GLYPH[status];
              return (
                <div key={key} className="tool-row mono">
                  <span className={`tool-glyph ${spec.className}`} aria-hidden="true">
                    {spec.glyph}
                  </span>
                  <span className="tool-name">{part.tool}</span>
                  {part.state.title !== undefined && part.state.title !== "" && (
                    <span className="tool-title">{part.state.title}</span>
                  )}
                  {part.state.time !== undefined && (
                    <span className="tool-dur">
                      {formatDuration(part.state.time.end - part.state.time.start)}
                    </span>
                  )}
                  <span className="tool-status">{status}</span>
                </div>
              );
            }
            case "step-finish":
              if (!("reason" in part) || !("tokens" in part) || !("cost" in part)) {
                return (
                  <div key={key} className="step-row mono">
                    <span>part · {part.type}</span>
                  </div>
                );
              }
              return (
                <div key={key} className="step-row mono">
                  <span>step-finish · {part.reason}</span>
                  <span>
                    {formatCompact(part.tokens.total)} tok · {formatUSD(part.cost)}
                  </span>
                </div>
              );
            case "file":
              return (
                <div key={key} className="step-row mono">
                  <span>file</span>
                  <span className="tool-title">{"filename" in part ? part.filename : ""}</span>
                </div>
              );
            case "patch":
              return (
                <div key={key} className="step-row mono">
                  <span>patch</span>
                  <span className="tool-title">
                    {"hash" in part ? `${part.hash.slice(0, 8)} · ${part.files.length} files` : ""}
                  </span>
                </div>
              );
            case "compaction":
              return (
                <div key={key} className="step-row mono">
                  <span>compaction boundary</span>
                </div>
              );
            case "step-start":
              return null;
            default:
              return (
                <div key={key} className="step-row mono">
                  <span>part · {part.type}</span>
                </div>
              );
          }
        })}
      </div>
    </article>
  );
}

/* ---------------- Transcript tab ---------------- */

export interface TranscriptTabProps {
  flat: FlatMessage[];
  jumpTarget: string | null;
  onJumped: () => void;
  paletteOpen: boolean;
  /** W3-2 additive: while the run inspector is open, Esc belongs to it. */
  inspectorOpen?: boolean;
}

export function TranscriptTab({ flat, jumpTarget, onJumped, paletteOpen, inspectorOpen = false }: TranscriptTabProps) {
  const [query, setQuery] = useState("");
  const containerRef = useRef<HTMLDivElement | null>(null);
  const [scrollTop, setScrollTop] = useState(0);
  const [viewportH, setViewportH] = useState(600);
  const [pinned, setPinned] = useState(false);
  const [unseen, setUnseen] = useState(0);
  const [highlight, setHighlight] = useState<string | null>(null);
  const pinnedRef = useRef(false);
  const needle = query.trim().toLowerCase();
  const shown = useMemo(() => {
    if (needle === "") return flat;
    return flat.filter((entry) =>
      flatMessageText(entry).toLowerCase().includes(needle),
    );
  }, [flat, needle]);
  const prevLenRef = useRef(shown.length);
  pinnedRef.current = pinned;

  useEffect(() => {
    const el = containerRef.current;
    if (el !== null && el.clientHeight > 0) setViewportH(el.clientHeight);
  }, []);

  const scrollToBottom = (): void => {
    const el = containerRef.current;
    if (el !== null) el.scrollTop = el.scrollHeight;
  };

  useEffect(() => {
    scrollToBottom();
    prevLenRef.current = shown.length;
    // Mount only: tail-follow starts ON at the live edge.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  useEffect(() => {
    const prev = prevLenRef.current;
    prevLenRef.current = shown.length;
    if (shown.length <= prev) return;
    if (pinnedRef.current) {
      setUnseen((u) => u + (shown.length - prev));
    } else {
      scrollToBottom();
    }
  }, [shown.length]);

  useEffect(() => {
    if (jumpTarget === null) return;
    const index = shown.findIndex((f) => f.message.info.id === jumpTarget);
    if (index < 0) {
      onJumped();
      return;
    }
    const top = Math.max(0, index * EST_ROW_HEIGHT_PX - viewportH / 2);
    const el = containerRef.current;
    if (el !== null) el.scrollTop = top;
    setScrollTop(top);
    setPinned(top > 0 && !isNearBottom(top + viewportH, top, viewportH));
    setHighlight(jumpTarget);
    onJumped();
  }, [jumpTarget, shown, viewportH, onJumped]);

  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent): void => {
      if (event.key !== "Escape" || paletteOpen || inspectorOpen) return;
      if (isTypingTarget(event.target)) return;
      if (!pinnedRef.current) return;
      event.preventDefault();
      event.stopPropagation();
      setPinned(false);
      setUnseen(0);
      setHighlight(null);
      scrollToBottom();
    };
    window.addEventListener("keydown", onKeyDown, true);
    return () => window.removeEventListener("keydown", onKeyDown, true);
  }, [paletteOpen, inspectorOpen]);

  const onScroll = (): void => {
    const el = containerRef.current;
    if (el === null) return;
    setScrollTop(el.scrollTop);
    if (isNearBottom(el.scrollHeight, el.scrollTop, el.clientHeight)) {
      setPinned(false);
      setUnseen(0);
    } else {
      setPinned(true);
    }
  };

  const resume = (): void => {
    setPinned(false);
    setUnseen(0);
    scrollToBottom();
  };

  const { start, end } = useMemo(
    () => computeWindow(scrollTop, shown.length, viewportH),
    [scrollTop, shown.length, viewportH],
  );

  return (
    <div className="transcript-wrap">
      <div className="transcript-tools">
        <label className="transcript-search">
          <Search aria-hidden="true" size={12} />
          <input
            type="search"
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            placeholder="Filter messages…"
            aria-label="Filter transcript messages"
          />
          {query !== "" && (
            <button
              type="button"
              className="transcript-clear"
              aria-label="Clear transcript filter"
              onClick={() => setQuery("")}
            >
              ×
            </button>
          )}
        </label>
        {query.trim() !== "" && (
          <span className="transcript-match mono" role="status">
            {`${shown.length} of ${flat.length}`}
          </span>
        )}
      </div>
      <div
        ref={containerRef}
        className="transcript"
        role="log"
        aria-label="Session transcript"
        tabIndex={0}
        onScroll={onScroll}
      >
        {shown.length === 0 ? (
          <div className="empty-state">
            {flat.length === 0
              ? "No messages in this session yet."
              : "No messages match this filter."}
          </div>
        ) : (
          <>
            <div style={{ height: start * EST_ROW_HEIGHT_PX }} aria-hidden="true" />
            {shown.slice(start, end).map((entry) => (
              <MessageRow
                key={entry.message.info.id}
                entry={entry}
                highlight={highlight === entry.message.info.id}
              />
            ))}
            <div
              style={{ height: (shown.length - end) * EST_ROW_HEIGHT_PX }}
              aria-hidden="true"
            />
          </>
        )}
      </div>
      {pinned && unseen > 0 && (
        <button type="button" className="tail-pill mono" onClick={resume}>
          <ArrowDown aria-hidden="true" size={12} />
          <span>
            ↓ {unseen} new
          </span>
        </button>
      )}
    </div>
  );
}

/* ---------------- Turns tab ---------------- */

export function TurnsTab({ turns, onSelect }: { turns: Turn[]; onSelect: (id: string) => void }) {
  if (turns.length === 0) {
    return <div className="empty-state">No assistant turns yet.</div>;
  }
  return (
    <ol className="turns">
      {turns.map((turn) => {
        const summary = summarizeTurn(turn);
        const hasError = summary.tools.some((t) => t.status === "error");
        return (
          <li key={turn.id}>
            <button
              type="button"
              className="turn-card"
              data-testid={`turn-card-${turn.id}`}
              onClick={() => onSelect(turn.id)}
              title="Jump to this turn in the transcript"
            >
              <span className="turn-top mono">
                <AgentLaneChip agent={turn.agent} lane={turn.lane} />
                <span className="tool-title">{formatDuration(summary.durationMs)}</span>
                <span className="tool-title">{formatUSD(summary.cost)}</span>
              </span>
              <span className="turn-input">{summary.inputSummary}</span>
              <span className="turn-meta mono">
                <span>
                  {summary.toolCount} tool{summary.toolCount === 1 ? "" : "s"}
                  {summary.toolNames.length > 0 && ` · ${summary.toolNames.join(", ")}`}
                </span>
                {hasError && (
                  <span className="tok-err" aria-label="turn contains a failed tool call">
                    ✕
                  </span>
                )}
                <span>
                  in {formatCompact(summary.tokens.input)} / out{" "}
                  {formatCompact(summary.tokens.output)} / {formatCompact(summary.tokens.total)}
                </span>
              </span>
            </button>
          </li>
        );
      })}
    </ol>
  );
}

export function StubTab({ label }: { label: string }) {
  return (
    <div className="empty-state">
      {label} lands in W3-2 — stub only, no content yet.
    </div>
  );
}

/* ---------------- detail shell ---------------- */

export interface SessionDetailProps {
  id: string;
  tab: SessionTab;
  row: SessionRow | null;
  paletteOpen: boolean;
  /** W3-2 additive: Raw-tab deep-link target (`?m=`); null = no target. */
  messageId?: string | null;
  /**
   * Snapshot rows for the Subagents panel: each child's TRUE status is
   * rendered from its matching row — never inferred from the parent.
   */
  sessions?: readonly SessionRow[];
  /** Data source; production defaults to the live export route. */
  provider?: SessionDataProvider;
}

export default function SessionDetail({
  id,
  tab,
  row,
  paletteOpen,
  messageId = null,
  sessions = [],
  provider = liveProvider,
}: SessionDetailProps) {
  const [data, setData] = useState<SessionDetailData | null | undefined>(
    undefined,
  );
  const [jumpTo, setJumpTo] = useState<string | null>(null);
  const [selectedRunId, setSelectedRunId] = useState<string | null>(null);

  useEffect(() => {
    let active = true;
    setData(undefined);
    Promise.resolve()
      .then(() => provider.getSessionDetail(id))
      .then(
        (next) => {
          if (active) setData(next);
        },
        () => {
          if (active) setData(null);
        },
      );
    return () => {
      active = false;
    };
  }, [provider, id]);

  const flat = useMemo(
    () => (data === null || data === undefined ? [] : flattenTurns(data.detail, data.children)),
    [data],
  );
  const turns = useMemo(() => buildTurns(flat), [flat]);
  const runs = useMemo(() => collectToolRuns(flat), [flat]);
  const selectedRun: ToolRun | null = runs.find((r) => r.runId === selectedRunId) ?? null;
  const inspectorOpen = selectedRun !== null && (tab === "timeline" || tab === "tools");

  // Yield chain: inspector (window capture + stopPropagation) → TranscriptTab
  // tail-resume (yields via inspectorOpen) → App bubble-phase Esc→fleet.
  // Fires ONLY while the inspector is open, so all other Esc paths are untouched.
  useInspectorEsc(inspectorOpen, () => setSelectedRunId(null), paletteOpen);

  useEffect(() => {
    setJumpTo(null);
  }, [id]);

  useEffect(() => {
    setSelectedRunId(null);
  }, [tab, id]);

  if (data === undefined) {
    return (
      <div className="session-detail">
        <a className="stub-back mono" href={buildFleetHash()}>
          <ArrowLeft aria-hidden="true" size={12} />
          <span>back to fleet</span>
        </a>
        <div className="empty-state" role="status">
          loading session…
        </div>
      </div>
    );
  }

  // Both harnesses serve export-shaped detail (pi via the P6 translator),
  // so a null payload means the same thing everywhere: unknown id or an
  // unreachable API. The Live pane tab still works for pi rows with panes
  // through the generic path — screen resolution is harness-scoped
  // server-side — and reports its own 409 empty state when there is no
  // pane to read.
  if (data === null) {
    return (
      <div className="session-detail">
        <a className="stub-back mono" href={buildFleetHash()}>
          <ArrowLeft aria-hidden="true" size={12} />
          <span>back to fleet</span>
        </a>
        <div className="empty-state">
          Session “{id}” has no live detail — it may not exist, or the API is
          unavailable.
        </div>
      </div>
    );
  }

  const assistantCount = turns.length;
  const totalTokens =
    data.detail.info.tokens.total +
    data.children.reduce((s, c) => s + c.info.tokens.total, 0);
  const totalCost =
    data.detail.info.cost + data.children.reduce((s, c) => s + c.info.cost, 0);

  const selectTurn = (turnId: string): void => {
    setJumpTo(turnId);
    if (tab !== "transcript") {
      window.location.hash = buildSessionHash(id, "transcript");
    }
  };

  return (
    <div className="session-detail">
      <a className="stub-back mono" href={buildFleetHash()}>
        <ArrowLeft aria-hidden="true" size={12} />
        <span>back to fleet</span>
      </a>
      <div className="detail-head">
        {row !== null && <StatusBadge status={row.status} />}
        {row !== null && <HarnessBadge harness={row.harness} />}
        <span className="detail-title">{data.detail.info.title}</span>
        <button
          type="button"
          className="live-toggle"
          aria-label="Download this session's detail as JSON"
          title="Download the loaded detail + children (same shape as the export endpoint)"
          onClick={() => downloadJson(exportFilename(`session-${id}`), data)}
        >
          <Download aria-hidden="true" size={12} />
          <span>Export</span>
        </button>
        <DataGuide
          blurb="One session, six ways to read it. Tabs share the same live export; pick the lens that answers your question."
          entries={[
            { term: "Transcript", definition: "Full message history in order (tail-follows new arrivals; scroll up pins with a new-message pill)." },
            { term: "Turns", definition: "One card per assistant turn: input summary, tool count, tokens, duration — the session as a work log." },
            { term: "Timeline", definition: "Tool-call bars scaled by duration over the token-rate underlay; click a bar for the inspector rail." },
            { term: "Tools", definition: "Every tool call with status, latency, and token cost, plus top error/latency tables and a per-call I/O inspector." },
            { term: "Cost", definition: "Cumulative spend line, input/output bars, cost-by-model donut. Child sessions roll into the totals — see the Subagent panel below the header." },
            { term: "Raw", definition: "The underlying export JSON, collapsible and deep-linkable per message — the ground truth behind every other tab." },
            { term: "Harness parity", definition: "Opencode rows render the native export (text, reasoning, tool, step-finish, file, patch, compaction). Pi rows render a server-side translation (text, reasoning, tool only) — per-turn tokens/cost come from usage records so Turns/Cost stay exact; part ids are synthesized. See docs/COST_METHOD.md for the counting rules." },
            { term: "Live pane", definition: "The session’s live terminal text, fetched on view (never polled). DB-only 409 means no live pane — the DB tabs above still work." },
          ]}
        />
      </div>
      <dl className="detail-facts mono">
        <div>
          <dt>id</dt>
          <dd>{data.detail.info.id}</dd>
        </div>
        <div>
          <dt>agent</dt>
          <dd>{data.detail.info.agent}</dd>
        </div>
        <div>
          <dt>model</dt>
          <dd>{data.detail.info.model.providerID}/{data.detail.info.model.id}</dd>
        </div>
        <div>
          <dt>turns</dt>
          <dd>{assistantCount}</dd>
        </div>
        <div>
          <dt>tokens</dt>
          <dd>{formatCompact(totalTokens)}</dd>
        </div>
        <div>
          <dt>cost</dt>
          <dd>{formatUSD(totalCost)}</dd>
        </div>
        {row !== null && (
          <div>
            <dt>updated</dt>
            <dd>
              <RelativeTime updatedRelativeMs={row.updated_relative_ms} />
            </dd>
          </div>
        )}
      </dl>
      <SubagentPanel children={data.children} sessions={sessions} />
      <DetailTabBar id={id} active={tab} />
      <div
        className={tab === "transcript" ? undefined : "detail-parked"}
        inert={tab !== "transcript"}
      >
        <TranscriptTab
          flat={flat}
          jumpTarget={jumpTo}
          onJumped={() => setJumpTo(null)}
          paletteOpen={paletteOpen}
          inspectorOpen={inspectorOpen}
        />
      </div>
      {tab === "turns" && <TurnsTab turns={turns} onSelect={selectTurn} />}
      {(tab === "timeline" || tab === "tools" || tab === "cost" || tab === "raw" || tab === "screen") && (
        <div className={inspectorOpen ? "detail-split" : undefined}>
          <div className="detail-main">
            <ExtraTab
              tab={tab}
              flat={flat}
              turns={turns}
              runs={runs}
              data={data}
              sessionId={id}
              messageId={messageId}
              selectedRun={selectedRun}
              paletteOpen={paletteOpen}
              onSelectRun={(run) => setSelectedRunId(run.runId)}
              onCloseInspector={() => setSelectedRunId(null)}
            />
          </div>
          {inspectorOpen && selectedRun !== null && (
            <RunInspector run={selectedRun} runs={runs} onClose={() => setSelectedRunId(null)} />
          )}
        </div>
      )}
    </div>
  );
}

function ExtraTab({ tab, ...extra }: { tab: "timeline" | "tools" | "cost" | "raw" | "screen" } & DetailExtraProps): React.JSX.Element {
  const Extra = TAB_CONTENT[tab];
  return <Extra {...extra} />;
}

/** Re-exported for tests: first user-visible text of a message. */
export function messagePreview(message: ExportMessage): string {
  return firstText(message);
}

export { messageTokens, messageCost, messageDurationMs };
