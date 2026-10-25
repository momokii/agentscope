/**
 * SSE client for opencode mission control (Wave 2, task W2-3).
 *
 * Transport-only: every fleet/detail view consumes snapshots through
 * `createSSEClient`. No rendering code, no chart code, no external store
 * library — plain callbacks (vanilla TS), per the task brief.
 *
 * Server contract (W1-3, frozen):
 * - `GET /api/events` streams `event: snapshot` frames with `data: <Snapshot
 *   JSON>` + `id: <seq>` lines, plus a `retry:` hint and `: ping` keepalives.
 * - Reconnects send `Last-Event-ID` (native EventSource behavior) and may
 *   pass `?lastEventId=` / `?last_event_id=` as query fallback.
 * - `GET /api/fleet` returns the same snapshot shape for re-sync.
 */

// MIRROR: TypeScript mirror of the frozen Go contract in
// `internal/model/types.go`. Field names are snake_case BY DESIGN — they must
// stay byte-identical to the Go `json:` tags (`seq`, `sessions`, `kpis`,
// `last_poll_ms`, `stale`; row fields per `SessionRow`; KPI fields per
// `KPIs`). DRIFT RISK: if the Go tags change, update these interfaces in the
// same change and grep for `MIRROR` to find every copy. The Go enum
// `SessionStatus` (running|idle|waiting-input|error|archived) is mirrored by
// `SessionStatus`; unknown wire values are rejected at the boundary.

export type SessionStatus =
  | "running"
  | "idle"
  | "waiting-input"
  | "error"
  | "archived";

const SESSION_STATUSES: readonly string[] = [
  "running",
  "idle",
  "waiting-input",
  "error",
  "archived",
];

export interface TokenCounts {
  in: number;
  out: number;
  total: number;
}

export interface PaneInfo {
  cwd: string;
  exited: boolean;
}

export interface SessionRow {
  id: string;
  title: string;
  project: string;
  directory: string;
  model: string;
  /** Agent name when the backend reports one; absent → display the model. */
  agent?: string;
  status: SessionStatus;
  tokens: TokenCounts;
  cost: number;
  updated_relative_ms: number;
  spark: number[];
  subagent_count: number;
  pane: PaneInfo;
  orphan: boolean;
}

export interface KPIs {
  active: number;
  waiting_input: number;
  tokens_per_hour: number;
  cost_today: number;
  errors: number;
  sse_connected: boolean;
  /**
   * Live runtime probes (collector R1 lane). OPTIONAL until the backend
   * lands them — absent means "runtime not yet reporting", never zero.
   */
  live_processes?: number;
  live_panes?: number;
}

export interface Snapshot {
  seq: number;
  sessions: SessionRow[];
  kpis: KPIs;
  last_poll_ms: number;
  stale: boolean;
}

export type ConnectionPhase =
  | "connecting"
  | "connected"
  | "reconnecting"
  | "closed";

export interface StatusEvent {
  /** Lifecycle phase. Views render the "reconnecting…" banner off this. */
  state: ConnectionPhase;
  /** Consecutive-drop count while reconnecting; 0 when connected. */
  attempt: number;
  /** Last transport/sync error message, if any. Never a secret. */
  lastError?: string;
  /** Highest snapshot seq applied so far (the resume cursor). */
  lastEventId: number;
}

/** Minimal EventSource surface this client uses (injectable for tests). */
export type EventSourceFactory = (url: string) => Pick<
  EventSource,
  "addEventListener" | "removeEventListener" | "close"
>;

export type FetchSnapshotFn = (snapshotUrl: string) => Promise<Snapshot>;

export interface SSEClientOptions {
  /** SSE endpoint, e.g. `/api/events`. Resume cursor is appended as query. */
  url: string;
  /** Called exactly once per applied frame with the FULL snapshot (replace). */
  onSnapshot: (snapshot: Snapshot) => void;
  /** Lifecycle + reconnect-attempt visibility for the banner. */
  onStatus?: (status: StatusEvent) => void;
  /**
   * REST endpoint for full-snapshot re-sync on reconnect. Defaults to the
   * `/api/fleet` sibling of `url` when `url` ends in `/api/events`.
   */
  snapshotUrl?: string;
  /** Max downstream emission rate; default 1000ms (1Hz sparkline cap). */
  minEmitIntervalMs?: number;
  /** First backoff step; default 1000ms. Attempt 1 retries immediately. */
  baseDelayMs?: number;
  /** Backoff ceiling; default 30000ms. */
  maxDelayMs?: number;
  /** EventSource constructor; defaults to the global. */
  createEventSource?: EventSourceFactory;
  /** Snapshot fetcher; defaults to global fetch. */
  fetchSnapshot?: FetchSnapshotFn;
  /** Jitter source in [0,1); defaults to Math.random. Inject for tests. */
  random?: () => number;
  /** Clock; defaults to Date.now. */
  now?: () => number;
}

export interface SSEClient {
  /** Stop everything: close the socket, clear all timers. No leaks. */
  close(): void;
  /** Highest applied seq (durable resume cursor). */
  getLastSeq(): number;
  /** Current lifecycle phase. */
  getPhase(): ConnectionPhase;
  /** Consecutive-drop reconnect attempt count. */
  getAttempt(): number;
}

const DEFAULT_MIN_EMIT_MS = 1000;
const DEFAULT_BASE_DELAY_MS = 1000;
const DEFAULT_MAX_DELAY_MS = 30000;

/**
 * Backoff for reconnect attempt n (1-based): attempt 1 retries immediately
 * (operator dashboard must recover fast from a blip); later attempts grow
 * exponentially from baseDelayMs, capped at maxDelayMs, with equal jitter
 * (half deterministic + half random) so a fleet of tabs does not thunder.
 * Never gives up — the dashboard stays up for days by design.
 */
export function computeReconnectDelayMs(
  attempt: number,
  baseDelayMs: number = DEFAULT_BASE_DELAY_MS,
  maxDelayMs: number = DEFAULT_MAX_DELAY_MS,
  random: () => number = Math.random,
): number {
  if (attempt <= 1) return 0;
  const grown = baseDelayMs * 2 ** (attempt - 2);
  const capped = Math.min(grown, maxDelayMs);
  return capped / 2 + random() * (capped / 2);
}

/** Append the resume cursor as query fallback (`?lastEventId=`). */
export function buildResumeUrl(baseUrl: string, lastSeq: number): string {
  if (lastSeq <= 0) return baseUrl;
  const sep = baseUrl.includes("?") ? "&" : "?";
  return `${baseUrl}${sep}lastEventId=${lastSeq}`;
}

/** Derive the re-sync endpoint from the stream URL (`/api/events`→`/api/fleet`). */
export function deriveSnapshotUrl(eventsUrl: string): string {
  const cut = eventsUrl.search(/[?#]/);
  const base = cut >= 0 ? eventsUrl.slice(0, cut) : eventsUrl;
  if (base.endsWith("/api/events")) {
    return `${base.slice(0, -"/api/events".length)}/api/fleet`;
  }
  return base;
}

function errorMessage(error: unknown): string {
  if (error instanceof Error) return error.message;
  return String(error);
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null;
}

/**
 * Validate + normalize one wire payload into a Snapshot. Accepts both the
 * SSE frame shape (full Snapshot) and the `GET /api/fleet` shape
 * (`{seq, sessions, kpis}` without `last_poll_ms`/`stale`). Throws on any
 * contract violation — fail fast at the boundary, never render half-shapes.
 */
export function normalizeSnapshot(payload: unknown): Snapshot {
  if (!isRecord(payload)) throw new Error("snapshot: payload is not an object");
  if (typeof payload["seq"] !== "number" || !Number.isFinite(payload["seq"])) {
    throw new Error("snapshot: missing numeric seq");
  }
  if (!Array.isArray(payload["sessions"])) {
    throw new Error("snapshot: missing sessions array");
  }
  if (!isRecord(payload["kpis"])) throw new Error("snapshot: missing kpis");
  const sessions = (payload["sessions"] as unknown[]).map((row, index) =>
    normalizeRow(row, index),
  );
  const kpis = payload["kpis"] as Record<string, unknown>;
  for (const key of [
    "active",
    "waiting_input",
    "tokens_per_hour",
    "cost_today",
    "errors",
    "sse_connected",
  ]) {
    if (!(key in kpis)) throw new Error(`snapshot: kpis missing ${key}`);
  }
  return {
    seq: payload["seq"] as number,
    sessions,
    kpis: kpis as unknown as KPIs,
    last_poll_ms:
      typeof payload["last_poll_ms"] === "number"
        ? (payload["last_poll_ms"] as number)
        : 0,
    stale: payload["stale"] === true,
  };
}

function normalizeRow(row: unknown, index: number): SessionRow {
  if (!isRecord(row)) throw new Error(`snapshot: session ${index} not an object`);
  if (typeof row["id"] !== "string" || row["id"] === "") {
    throw new Error(`snapshot: session ${index} missing id`);
  }
  if (typeof row["status"] !== "string" || !SESSION_STATUSES.includes(row["status"])) {
    throw new Error(
      `snapshot: session ${row["id"]} has unknown status ${JSON.stringify(row["status"])}`,
    );
  }
  return row as unknown as SessionRow;
}

async function defaultFetchSnapshot(snapshotUrl: string): Promise<Snapshot> {
  const response = await fetch(snapshotUrl, {
    headers: { Accept: "application/json" },
  });
  if (!response.ok) {
    throw new Error(`snapshot fetch ${response.status} from ${snapshotUrl}`);
  }
  return normalizeSnapshot(await response.json());
}

/**
 * Create an auto-reconnecting SSE client over full-snapshot frames.
 *
 * - Resume: every (re)connect carries the cursor — native `Last-Event-ID`
 *   header (EventSource replays the last `id:` automatically) plus the
 *   `?lastEventId=` query fallback for servers reading the query string.
 * - Re-sync: on reconnect, `GET snapshotUrl` replaces local state wholesale.
 *   Frames are never merged across a gap; `lastAppliedSeq` drops stale or
 *   duplicate seqs so the same tick is never applied twice.
 * - Rate: downstream `onSnapshot` fires at most once per `minEmitIntervalMs`
 *   (default 1Hz). Bursts keep only the latest frame (aggregate-or-drop);
 *   each emission is exactly one call with the full snapshot, so consumers
 *   replace state atomically (no scroll-yank from partial merges).
 * - Lifetime: reconnects forever with capped exponential backoff; `close()`
 *   kills the socket and every timer.
 */
export function createSSEClient(options: SSEClientOptions): SSEClient {
  const minEmitMs = options.minEmitIntervalMs ?? DEFAULT_MIN_EMIT_MS;
  const baseDelayMs = options.baseDelayMs ?? DEFAULT_BASE_DELAY_MS;
  const maxDelayMs = options.maxDelayMs ?? DEFAULT_MAX_DELAY_MS;
  const random = options.random ?? Math.random;
  const now = options.now ?? (() => Date.now());
  const createEventSource =
    options.createEventSource ?? ((url: string) => new EventSource(url));
  const fetchSnapshot = options.fetchSnapshot ?? defaultFetchSnapshot;
  const snapshotUrl = options.snapshotUrl ?? deriveSnapshotUrl(options.url);
  const onStatus = options.onStatus ?? ((): void => undefined);

  let closed = false;
  let phase: ConnectionPhase = "connecting";
  let attempt = 0;
  let lastAppliedSeq = 0;
  let lastError: string | undefined;
  let stream: Pick<EventSource, "addEventListener" | "removeEventListener" | "close"> | null =
    null;
  let reconnectTimer: ReturnType<typeof setTimeout> | null = null;
  let emitTimer: ReturnType<typeof setTimeout> | null = null;
  let lastEmitAt = Number.NEGATIVE_INFINITY;
  let pendingLatest: Snapshot | null = null;

  function emitStatus(state: ConnectionPhase): void {
    phase = state;
    onStatus({ state, attempt, lastError, lastEventId: lastAppliedSeq });
  }

  function emit(snapshot: Snapshot): void {
    lastEmitAt = now();
    // Atomic replace: exactly one call per applied frame with the FULL
    // snapshot. Consumers assign (never merge), so list scroll position and
    // expanded rows survive a tick — no scroll-yank.
    options.onSnapshot(snapshot);
  }

  function flushPending(): void {
    emitTimer = null;
    if (closed || pendingLatest === null) {
      pendingLatest = null;
      return;
    }
    const latest = pendingLatest;
    pendingLatest = null;
    emit(latest);
  }

  /**
   * Ingest one snapshot. Returns true when the frame is accepted (new seq).
   * Stale/duplicate seqs are dropped; accepted stream frames are throttled
   * to minEmitMs while re-sync frames replace immediately (authoritative).
   */
  function ingest(snapshot: Snapshot, immediate: boolean): boolean {
    if (closed) return false;
    if (snapshot.seq <= lastAppliedSeq) return false; // stale or duplicate
    lastAppliedSeq = snapshot.seq;
    if (immediate || now() - lastEmitAt >= minEmitMs) {
      if (emitTimer !== null) {
        clearTimeout(emitTimer);
        emitTimer = null;
      }
      pendingLatest = null;
      emit(snapshot);
      return true;
    }
    pendingLatest = snapshot; // aggregate: keep latest, drop the rest
    if (emitTimer === null) {
      const wait = minEmitMs - (now() - lastEmitAt);
      emitTimer = setTimeout(flushPending, Math.max(0, wait));
    }
    return true;
  }

  function handleFrame(data: string): void {
    let snapshot: Snapshot;
    try {
      snapshot = normalizeSnapshot(JSON.parse(data));
    } catch (error) {
      // Malformed frame: log with the seq context we have, keep the stream.
      // The next tick or re-sync heals; never crash the dashboard on one bad frame.
      console.warn(`[sse] dropped malformed frame: ${errorMessage(error)}`);
      return;
    }
    ingest(snapshot, false);
  }

  function onOpen(): void {
    if (closed) return;
    attempt = 0;
    lastError = undefined;
    emitStatus("connected");
  }

  function onStreamError(): void {
    if (closed) return;
    detachStream();
    attempt += 1;
    lastError = "event stream dropped";
    console.warn(
      `[sse] stream dropped (attempt ${attempt}, lastEventId ${lastAppliedSeq}); reconnecting`,
    );
    emitStatus("reconnecting");
    const delay = computeReconnectDelayMs(attempt, baseDelayMs, maxDelayMs, random);
    reconnectTimer = setTimeout(() => {
      reconnectTimer = null;
      void resyncAndConnect();
    }, delay);
  }

  /** Full-snapshot re-sync, then re-open the stream from the new cursor. */
  async function resyncAndConnect(): Promise<void> {
    if (closed) return;
    try {
      const snapshot = await fetchSnapshot(snapshotUrl);
      // Replace, never merge: authoritative state replaces whatever the
      // consumer holds, closing the gap the dropped socket left behind.
      ingest(snapshot, true);
      lastError = undefined;
    } catch (error) {
      // Re-sync failed but the stream may still heal via Last-Event-ID
      // replay, so reconnect anyway and surface the error on the banner.
      lastError = errorMessage(error);
      console.warn(`[sse] re-sync failed: ${lastError}; reconnecting anyway`);
      emitStatus("reconnecting");
    }
    if (!closed) connect();
  }

  function onSnapshotEvent(event: Event): void {
    const data = (event as MessageEvent).data;
    if (typeof data !== "string") return;
    handleFrame(data);
  }

  function onErrorEvent(): void {
    onStreamError();
  }

  function onOpenEvent(): void {
    onOpen();
  }

  function detachStream(): void {
    if (stream !== null) {
      stream.removeEventListener("snapshot", onSnapshotEvent);
      stream.removeEventListener("message", onSnapshotEvent);
      stream.removeEventListener("open", onOpenEvent);
      stream.removeEventListener("error", onErrorEvent);
      try {
        stream.close();
      } catch (error) {
        console.warn(`[sse] stream close threw: ${errorMessage(error)}`);
      }
      stream = null;
    }
  }

  function connect(): void {
    if (closed) return;
    // Query fallback: native EventSource also replays Last-Event-ID from the
    // last `id:` field as a header; the server reads header first, query next.
    const url = buildResumeUrl(options.url, lastAppliedSeq);
    const source = createEventSource(url);
    stream = source;
    source.addEventListener("snapshot", onSnapshotEvent);
    // Fallback for senders that emit default `message` frames.
    source.addEventListener("message", onSnapshotEvent);
    source.addEventListener("open", onOpenEvent);
    source.addEventListener("error", onErrorEvent);
  }

  emitStatus("connecting");
  connect();

  return {
    close(): void {
      if (closed) return;
      closed = true;
      if (reconnectTimer !== null) {
        clearTimeout(reconnectTimer);
        reconnectTimer = null;
      }
      if (emitTimer !== null) {
        clearTimeout(emitTimer);
        emitTimer = null;
      }
      pendingLatest = null;
      detachStream();
      emitStatus("closed");
    },
    getLastSeq(): number {
      return lastAppliedSeq;
    },
    getPhase(): ConnectionPhase {
      return phase;
    },
    getAttempt(): number {
      return attempt;
    },
  };
}
