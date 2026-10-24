/**
 * Transcript pure library (W3-1): `opencode export`-shaped types +
 * interleaving / turn-summary / token-math helpers.
 *
 * MIRROR: field names are byte-identical to `opencode export` JSON
 * (`info`/`messages[]`/`parts[]`, per-part `type` discriminants, token
 * fields `input`/`output`/`reasoning`/`cache`/`total`). The fixture in
 * `testutils/sessionDetail.fixture.ts` is hand-shaped against these types;
 * real shapes were cribbed read-only from `opencode export <ses> --sanitize`.
 * Per-message tokens in real export output are cumulative snapshots; the
 * fixture instead carries per-turn deltas so the export-parity test can
 * assert `rollUp(tokens) === info.tokens` (documented in the fixture).
 */

/** Token snapshot — field names match `opencode export` exactly. */
export interface ExportTokens {
  input: number;
  output: number;
  reasoning: number;
  cache: { read: number; write: number };
  total: number;
}

export function zeroTokens(): ExportTokens {
  return { input: 0, output: 0, reasoning: 0, cache: { read: 0, write: 0 }, total: 0 };
}

/** Additive roll-up over per-turn token snapshots (fixture parity math). */
export function rollUpTokens(all: readonly ExportTokens[]): ExportTokens {
  const out = zeroTokens();
  for (const t of all) {
    out.input += t.input;
    out.output += t.output;
    out.reasoning += t.reasoning;
    out.cache.read += t.cache.read;
    out.cache.write += t.cache.write;
    out.total += t.total;
  }
  return out;
}

export type MessageRole = "user" | "assistant";

export interface ExportMessageTime {
  created: number;
  completed?: number;
}

export interface ExportMessageInfo {
  id: string;
  sessionID: string;
  role: MessageRole;
  time: ExportMessageTime;
  agent?: string;
  modelID?: string;
  providerID?: string;
  tokens?: ExportTokens;
  cost?: number;
}

export interface TextPart {
  type: "text";
  id: string;
  messageID: string;
  sessionID: string;
  text: string;
}

export interface ReasoningPart {
  type: "reasoning";
  id: string;
  messageID: string;
  sessionID: string;
  text: string;
  time?: { start: number; end: number };
}

export type ToolStatus = "completed" | "error" | "pending" | "running";

export interface ToolPart {
  type: "tool";
  id: string;
  messageID: string;
  sessionID: string;
  tool: string;
  callID: string;
  state: {
    status: ToolStatus;
    input?: unknown;
    output?: string;
    title?: string;
    time?: { start: number; end: number };
  };
}

export interface StepStartPart {
  type: "step-start";
  id: string;
  messageID: string;
  sessionID: string;
}

export interface StepFinishPart {
  type: "step-finish";
  id: string;
  messageID: string;
  sessionID: string;
  reason: string;
  tokens: ExportTokens;
  cost: number;
}

export interface FilePart {
  type: "file";
  id: string;
  messageID: string;
  sessionID: string;
  mime: string;
  filename: string;
  url: string;
}

export interface PatchPart {
  type: "patch";
  id: string;
  messageID: string;
  sessionID: string;
  hash: string;
  files: string[];
}

export interface CompactionPart {
  type: "compaction";
  id: string;
  messageID: string;
  sessionID: string;
}

/** Forward-compatible catch-all: unknown future part types never crash us. */
export interface UnknownPart {
  type: string;
  id: string;
  messageID: string;
  sessionID: string;
}

export type Part =
  | TextPart
  | ReasoningPart
  | ToolPart
  | StepStartPart
  | StepFinishPart
  | FilePart
  | PatchPart
  | CompactionPart
  | UnknownPart;

export interface ExportMessage {
  info: ExportMessageInfo;
  parts: Part[];
}

export interface ExportSessionInfo {
  id: string;
  slug: string;
  projectID: string;
  directory: string;
  title: string;
  agent: string;
  model: { id: string; providerID: string; variant: string };
  cost: number;
  tokens: ExportTokens;
  time: { created: number; updated: number };
  parentID: string | null;
}

export interface ExportSession {
  info: ExportSessionInfo;
  messages: ExportMessage[];
}

/** One message positioned in the merged main+subagent stream. */
export interface FlatMessage {
  message: ExportMessage;
  /** Owning session id (main detail id or child id). */
  sessionId: string;
  /** Agent lane for the chip (letter, not color, is the distinguisher). */
  agent: string;
  /** "main" for the viewed session, "sub" for parent_id-linked children. */
  lane: "main" | "sub";
  timeCreated: number;
}

/**
 * Merge main + parent_id-linked child messages into one chronological
 * stream (stable on ties via array order: main first, then children).
 */
export function flattenTurns(
  detail: ExportSession,
  children: readonly ExportSession[],
): FlatMessage[] {
  const flat: FlatMessage[] = [];
  const push = (
    session: ExportSession,
    lane: "main" | "sub",
  ): void => {
    for (const message of session.messages) {
      flat.push({
        message,
        sessionId: session.info.id,
        agent: message.info.agent ?? session.info.agent,
        lane,
        timeCreated: message.info.time.created,
      });
    }
  };
  push(detail, "main");
  for (const child of children) push(child, "sub");
  return flat
    .map((entry, index) => ({ entry, index }))
    .sort((a, b) => a.entry.timeCreated - b.entry.timeCreated || a.index - b.index)
    .map(({ entry }) => entry);
}

/** One assistant turn: the response plus its triggering user prompt. */
export interface Turn {
  /** Anchor id shared by the Transcript row and the Turns card. */
  id: string;
  sessionId: string;
  agent: string;
  lane: "main" | "sub";
  prompt: ExportMessage | null;
  response: ExportMessage;
  startMs: number;
}

/** One card per assistant message; prompt = nearest preceding user msg. */
export function buildTurns(flat: readonly FlatMessage[]): Turn[] {
  const turns: Turn[] = [];
  let lastUser: ExportMessage | null = null;
  for (const entry of flat) {
    if (entry.message.info.role === "user") {
      lastUser = entry.message;
    } else {
      turns.push({
        id: entry.message.info.id,
        sessionId: entry.sessionId,
        agent: entry.agent,
        lane: entry.lane,
        prompt: lastUser,
        response: entry.message,
        startMs: entry.timeCreated,
      });
      lastUser = null;
    }
  }
  return turns;
}

/** Truncate with an explicit ellipsis marker (never silent clipping). */
export function truncateWithEllipsis(text: string, max: number = 120): string {
  const flat = text.replace(/\s+/g, " ").trim();
  if (flat.length <= max) return flat;
  return `${flat.slice(0, max)}…`;
}

/** First text part of a message ("" when the message carries none). */
export function firstText(message: ExportMessage | null): string {
  if (message === null) return "";
  for (const part of message.parts) {
    if (part.type === "text") return (part as TextPart).text;
  }
  return "";
}

export interface ToolCallSummary {
  name: string;
  status: ToolStatus;
}

export interface TurnSummary {
  /** First user (prompt) text, truncated with ellipsis marker. */
  inputSummary: string;
  toolCount: number;
  toolNames: string[];
  tools: ToolCallSummary[];
  tokens: ExportTokens;
  durationMs: number;
  cost: number;
}

/** Per-turn token math reads `opencode export` fields only (no invention). */
export function messageTokens(message: ExportMessage): ExportTokens {
  if (message.info.tokens) return message.info.tokens;
  for (const part of message.parts) {
    if (part.type === "step-finish") return (part as StepFinishPart).tokens;
  }
  return zeroTokens();
}

export function messageCost(message: ExportMessage): number {
  if (typeof message.info.cost === "number") return message.info.cost;
  for (const part of message.parts) {
    if (part.type === "step-finish") return (part as StepFinishPart).cost;
  }
  return 0;
}

export function messageDurationMs(message: ExportMessage): number {
  const { created, completed } = message.info.time;
  if (typeof completed === "number" && completed >= created) {
    return completed - created;
  }
  return 0;
}

export function summarizeTurn(turn: Turn): TurnSummary {
  const tools: ToolCallSummary[] = [];
  for (const part of turn.response.parts) {
    if (part.type === "tool") {
      const tool = part as ToolPart;
      tools.push({ name: tool.tool, status: tool.state.status });
    }
  }
  const promptText = firstText(turn.prompt);
  const responseText = firstText(turn.response);
  const inputSummary =
    promptText !== ""
      ? truncateWithEllipsis(promptText)
      : responseText !== ""
        ? truncateWithEllipsis(responseText)
        : "(no text)";
  return {
    inputSummary,
    toolCount: tools.length,
    toolNames: [...new Set(tools.map((t) => t.name))],
    tools,
    tokens: messageTokens(turn.response),
    durationMs: messageDurationMs(turn.response),
    cost: messageCost(turn.response),
  };
}

/* ---------- tail-follow + windowing (pure, tested without layout) ---------- */

export const TAIL_PIN_THRESHOLD_PX = 24;
export const EST_ROW_HEIGHT_PX = 120;
export const WINDOW_OVERSCAN = 20;
export const WINDOW_MAX_ROWS = 120;

/** True while the viewport sits at (or near) the live bottom edge. */
export function isNearBottom(
  scrollHeight: number,
  scrollTop: number,
  clientHeight: number,
  thresholdPx: number = TAIL_PIN_THRESHOLD_PX,
): boolean {
  return scrollHeight - scrollTop - clientHeight <= thresholdPx;
}

/**
 * Hand-rolled windowing: render [start, end) of `total` rows. Constant
 * estimated row height keeps total scroll height honest via spacer divs;
 * the window is capped so 1500+ message sessions stay at a bounded DOM size.
 */
export function computeWindow(
  scrollTop: number,
  total: number,
  viewportHeight: number,
  estRowHeightPx: number = EST_ROW_HEIGHT_PX,
): { start: number; end: number } {
  if (total <= 0) return { start: 0, end: 0 };
  const first = Math.max(
    0,
    Math.floor(scrollTop / estRowHeightPx) - WINDOW_OVERSCAN,
  );
  const visible = Math.ceil(viewportHeight / estRowHeightPx) + WINDOW_OVERSCAN * 2;
  const capped = Math.min(visible, WINDOW_MAX_ROWS);
  const start = Math.min(first, Math.max(0, total - capped));
  return { start, end: Math.min(total, start + capped) };
}
