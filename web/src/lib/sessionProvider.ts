/**
 * Session drill-down data provider. Production reads the live export route
 * (`GET /api/sessions/:id/export`); tests inject the committed fixture.
 *
 * The wire payload is `opencode export`-shaped with two deliberate
 * differences: session tokens omit the derived `total` (computed here from
 * input + output + reasoning + cache.read, the same formula as the fleet
 * roll-up), and model.variant may be absent on real DB rows.
 */

import type {
  ExportMessage,
  ExportSession,
  ExportSessionInfo,
  ExportTokens,
  Part,
} from "./transcript";

export interface SessionDetailData {
  detail: ExportSession;
  children: ExportSession[];
}

/**
 * Providers may answer synchronously (the test fixture map) or
 * asynchronously (the live fetch). The view resolves either form.
 */
export type SessionDetailResult =
  | SessionDetailData
  | null
  | Promise<SessionDetailData | null>;

export interface SessionDataProvider {
  getSessionDetail(id: string): SessionDetailResult;
}

function asRecord(value: unknown): Record<string, unknown> | null {
  return typeof value === "object" && value !== null && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : null;
}

function asString(value: unknown, fallback = ""): string {
  return typeof value === "string" ? value : fallback;
}

function asNumber(value: unknown, fallback = 0): number {
  return typeof value === "number" && Number.isFinite(value) ? value : fallback;
}

function toTokens(value: unknown): ExportTokens {
  const raw = asRecord(value) ?? {};
  const cache = asRecord(raw.cache) ?? {};
  const input = asNumber(raw.input);
  const output = asNumber(raw.output);
  const reasoning = asNumber(raw.reasoning);
  const read = asNumber(cache.read);
  const write = asNumber(cache.write);
  return {
    input,
    output,
    reasoning,
    cache: { read, write },
    total:
      typeof raw.total === "number" && Number.isFinite(raw.total)
        ? raw.total
        : input + output + reasoning + read,
  };
}

function toPart(
  value: unknown,
  messageID: string,
  sessionID: string,
  index: number,
): Part | null {
  const raw = asRecord(value);
  if (raw === null) return null;
  const type = asString(raw.type);
  if (type === "") return null;
  const id = asString(raw.id, `part-${messageID}-${index}`);
  const partMessageID = asString(raw.messageID, messageID);
  const partSessionID = asString(raw.sessionID, sessionID);
  // Raw pass-through: the export route sends stored part.data verbatim, so
  // unknown future part fields survive. Identity fields are normalized above.
  return {
    ...raw,
    type,
    id,
    messageID: partMessageID,
    sessionID: partSessionID,
  } as unknown as Part;
}

function toMessage(value: unknown, sessionID: string): ExportMessage | null {
  const raw = asRecord(value);
  const info = asRecord(raw?.info);
  if (raw === null || info === null) return null;
  const id = asString(info.id);
  const role = asString(info.role);
  if (id === "" || (role !== "user" && role !== "assistant")) return null;
  const time = asRecord(info.time) ?? {};
  const completed =
    typeof time.completed === "number" && Number.isFinite(time.completed)
      ? time.completed
      : undefined;
  const tokens = asRecord(info.tokens);
  const cost =
    typeof info.cost === "number" && Number.isFinite(info.cost)
      ? info.cost
      : undefined;
  const parts = Array.isArray(raw.parts)
    ? raw.parts
        .map((part, index) => toPart(part, id, sessionID, index))
        .filter((part): part is Part => part !== null)
    : [];
  return {
    info: {
      id,
      sessionID: asString(info.sessionID, sessionID),
      role,
      time: {
        created: asNumber(time.created),
        ...(completed === undefined ? {} : { completed }),
      },
      ...(asString(info.agent) === "" ? {} : { agent: asString(info.agent) }),
      ...(asString(info.modelID) === ""
        ? {}
        : { modelID: asString(info.modelID) }),
      ...(asString(info.providerID) === ""
        ? {}
        : { providerID: asString(info.providerID) }),
      ...(tokens === null ? {} : { tokens: toTokens(tokens) }),
      ...(cost === undefined ? {} : { cost }),
    },
    parts,
  };
}

function toSession(value: unknown): ExportSession | null {
  const raw = asRecord(value);
  const info = asRecord(raw?.info);
  if (raw === null || info === null) return null;
  const id = asString(info.id);
  if (id === "") return null;
  const model = asRecord(info.model) ?? {};
  const time = asRecord(info.time) ?? {};
  const parentID =
    typeof info.parentID === "string" && info.parentID !== ""
      ? info.parentID
      : null;
  const messages = Array.isArray(raw.messages)
    ? raw.messages
        .map((message) => toMessage(message, id))
        .filter((message): message is ExportMessage => message !== null)
    : [];
  const sessionInfo: ExportSessionInfo = {
    id,
    slug: asString(info.slug),
    projectID: asString(info.projectID),
    directory: asString(info.directory),
    title: asString(info.title),
    agent: asString(info.agent),
    model: {
      id: asString(model.id),
      providerID: asString(model.providerID),
      variant: asString(model.variant),
    },
    cost: asNumber(info.cost),
    tokens: toTokens(info.tokens),
    time: {
      created: asNumber(time.created),
      updated: asNumber(time.updated),
    },
    parentID,
  };
  return { info: sessionInfo, messages };
}

/** Map the additive export route payload onto the view's data shape. */
export function mapExportPayload(payload: unknown): SessionDetailData {
  const raw = asRecord(payload);
  if (raw === null) throw new Error("export: payload is not an object");
  const detail = toSession(raw.session);
  if (detail === null) throw new Error("export: missing session");
  const children = Array.isArray(raw.children)
    ? raw.children
        .map(toSession)
        .filter((child): child is ExportSession => child !== null)
    : [];
  return { detail, children };
}

async function fetchSessionDetail(id: string): Promise<SessionDetailData | null> {
  try {
    const response = await fetch(
      `/api/sessions/${encodeURIComponent(id)}/export`,
      {
        credentials: "same-origin",
        headers: { Accept: "application/json" },
      },
    );
    if (response.status === 404) return null;
    if (!response.ok) return null;
    return mapExportPayload(await response.json());
  } catch {
    return null;
  }
}

/** Live provider: same-origin fetch; any failure returns null, never fake data. */
export const liveProvider: SessionDataProvider = {
  getSessionDetail: fetchSessionDetail,
};
