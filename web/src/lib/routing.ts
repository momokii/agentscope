/**
 * Hash routing (W2-2 fleet/session + W3-3 compare/timeline):
 * `#/` fleet, `#/sessions/:id` session stub, `#/compare?ids=a,b,c`
 * compare 2–4 sessions, `#/timeline` global swimlanes.
 *
 * Hand-rolled (no router library) per the no-new-deps rule — plain string
 * prefix parsing. Wave 3 reuses `parseHash`/`buildSessionHash` for
 * drill-down deep links; compare/timeline selection state is hash
 * serializable so back/forward works via the existing hashchange wiring.
 */

import type { StatusFilter } from "./fleetSort";
import { STATUS_FILTERS } from "./fleetSort";

export type Route =
  | { view: "fleet"; status?: StatusFilter; liveOnly?: boolean }
  | { view: "session"; id: string; tab: SessionTab; messageId?: string }
  | { view: "compare"; ids: string[] }
  | { view: "timeline" };

const SESSION_PREFIX = "/sessions/";
const COMPARE_PATH = "/compare";
const TIMELINE_PATH = "/timeline";
const MAX_COMPARE_IDS = 4;

/**
 * SessionDetail tab segments (W3-1 owns transcript+turns; W3-2 owns
 * timeline/tools/cost/raw — the segments parse from day one so W3-2 only
 * flips the SessionDetail tab registry, never this file).
 */
export const SESSION_TABS = [
  "transcript",
  "turns",
  "timeline",
  "tools",
  "cost",
  "raw",
] as const;

export type SessionTab = (typeof SESSION_TABS)[number];

export function isSessionTab(value: string): value is SessionTab {
  return (SESSION_TABS as readonly string[]).includes(value);
}

export function parseHash(hash: string): Route {
  const clean = hash.startsWith("#") ? hash.slice(1) : hash;
  if (clean.startsWith(SESSION_PREFIX)) {
    const rest = clean.slice(SESSION_PREFIX.length);
    // W3-2 additive: Raw-tab deep links append `?m=<msgId>`; split the
    // query BEFORE segment parsing so tab matching is query-blind.
    const [path, query] = splitQuery(rest);
    const messageId = parseMessageId(query);
    // One segment (session id → Transcript default) or two
    // (session id + tab). Deeper/unknown paths fall back to fleet.
    const slash = path.indexOf("/");
    if (slash === -1) {
      if (path !== "") {
        return {
          view: "session",
          id: decodeURIComponent(path),
          tab: "transcript",
          ...(messageId === undefined ? {} : { messageId }),
        };
      }
    } else {
      const id = path.slice(0, slash);
      const tab = path.slice(slash + 1);
      if (id !== "" && !tab.includes("/") && isSessionTab(tab)) {
        return {
          view: "session",
          id: decodeURIComponent(id),
          tab,
          ...(messageId === undefined ? {} : { messageId }),
        };
      }
    }
  }
  const [path, query] = splitQuery(clean);
  if (path === COMPARE_PATH) return { view: "compare", ids: parseIds(query) };
  if (path === TIMELINE_PATH) return { view: "timeline" };
  if (path === "/" || path === "") return fleetRoute(query);
  return { view: "fleet" };
}

/**
 * Fleet query params (additive, shareable): `status=<filter>` pins a status
 * chip, `live=1` pins the live-only toggle. Unknown values fall back to the
 * unfiltered fleet so hand-typed URLs never strand the operator. Defaults
 * are omitted from the Route so `{ view: "fleet" }` equality holds.
 */
function fleetRoute(query: string): Route {
  if (query === "") return { view: "fleet" };
  const status = parseStatusParam(query);
  const liveOnly = parseLiveParam(query);
  return {
    view: "fleet",
    ...(status === "all" ? {} : { status }),
    ...(liveOnly ? { liveOnly: true } : {}),
  };
}

/** `status=running|waiting-input|idle|error|archived|all` (`waiting` alias). */
export function parseStatusParam(query: string): StatusFilter {
  const raw =
    new URLSearchParams(query).get("status")?.trim().toLowerCase() ?? "";
  if (raw === "waiting") return "waiting-input";
  return (STATUS_FILTERS as readonly string[]).includes(raw)
    ? (raw as StatusFilter)
    : "all";
}

/** `live=1` (or `live=true`) pins the live-only toggle. */
export function parseLiveParam(query: string): boolean {
  const raw = new URLSearchParams(query).get("live")?.trim().toLowerCase() ?? "";
  return raw === "1" || raw === "true";
}

/**
 * Parse `m=<msgId>`: Raw-tab deep link target. Empty/missing → undefined
 * (the Raw tab renders fully expanded, no scroll target).
 */
export function parseMessageId(query: string): string | undefined {
  if (query === "") return undefined;
  const raw = new URLSearchParams(query).get("m");
  if (raw === null || raw.trim() === "") return undefined;
  return raw;
}
/** Split `/path?query` into path + raw query (no query → ""). */
function splitQuery(clean: string): [string, string] {
  const cut = clean.indexOf("?");
  if (cut < 0) return [clean, ""];
  return [clean.slice(0, cut), clean.slice(cut + 1)];
}

/**
 * Parse `ids=a,b,c`: deduped in order, empties dropped, capped at 4
 * (Compare renders 2–4 sessions; extras are ignored, never error).
 */
export function parseIds(query: string): string[] {
  const params = new URLSearchParams(query);
  const raw = params.get("ids") ?? "";
  const seen = new Set<string>();
  for (const part of raw.split(",")) {
    const id = decodeURIComponent(part.trim());
    if (id !== "" && !seen.has(id)) seen.add(id);
    if (seen.size >= MAX_COMPARE_IDS) break;
  }
  return [...seen];
}

export function buildFleetHash(
  status: StatusFilter = "all",
  liveOnly = false,
): string {
  const params = new URLSearchParams();
  if (status !== "all") params.set("status", status);
  if (liveOnly) params.set("live", "1");
  const query = params.toString();
  return query === "" ? "#/" : `#/?${query}`;
}

export function buildSessionHash(
  id: string,
  tab: SessionTab = "transcript",
  messageId?: string,
): string {
  const encoded = encodeURIComponent(id);
  const base =
    tab === "transcript" ? `#/sessions/${encoded}` : `#/sessions/${encoded}/${tab}`;
  if (messageId === undefined || messageId === "") return base;
  return `${base}?m=${encodeURIComponent(messageId)}`;
}

export function buildCompareHash(ids: readonly string[]): string {
  const query = ids.map((id) => encodeURIComponent(id)).join(",");
  return query === "" ? "#/compare" : `#/compare?ids=${query}`;
}

export function buildTimelineHash(): string {
  return "#/timeline";
}
