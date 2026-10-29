/**
 * Hash routing (W2-2 fleet/session + W3-3 compare/timeline + activity log):
 * `#/` fleet, `#/sessions/:id` session stub, `#/compare?ids=a,b,c`
 * compare 2–4 sessions, `#/timeline` global swimlanes, `#/activity` the
 * full activity log (the bottom ticker, paged and filterable).
 *
 * Hand-rolled (no router library) per the no-new-deps rule — plain string
 * prefix parsing. Wave 3 reuses `parseHash`/`buildSessionHash` for
 * drill-down deep links; compare/timeline selection state is hash
 * serializable so back/forward works via the existing hashchange wiring.
 */

import type { DatePreset, IdleBucket, StatusFilter } from "./fleetSort";
import {
  DATE_PRESETS,
  HARNESS_FILTERS,
  IDLE_BUCKETS,
  STATUS_FILTERS,
  parseLocalDate,
} from "./fleetSort";
import type { HarnessFilter } from "./fleetSort";

export interface FleetQuery {
  status?: StatusFilter;
  liveOnly?: boolean;
  /** Harness slice (`opencode` | `pi`); omitted when `"all"`. */
  harness?: HarnessFilter;
  bucket?: IdleBucket;
  since?: DatePreset;
  /** `YYYY-MM-DD` custom lower bound (wins over `since`). */
  from?: string;
  /** 1-based page; omitted from the URL when 1. */
  page?: number;
}

export type Route =
  | ({ view: "fleet" } & FleetQuery)
  | ({ view: "project"; directory: string } & FleetQuery)
  | { view: "session"; id: string; tab: SessionTab; messageId?: string }
  | { view: "compare"; ids: string[] }
  | { view: "timeline" }
  | ({ view: "activity" } & ActivityQuery);

const SESSION_PREFIX = "/sessions/";
const PROJECTS_PREFIX = "/projects/";
const COMPARE_PATH = "/compare";
const TIMELINE_PATH = "/timeline";
const ACTIVITY_PATH = "/activity";
const MAX_COMPARE_IDS = 4;

/** Activity log query: text search + page (both URL-serialized). */
export interface ActivityQuery {
  /** Case-insensitive substring over title/project/id/kind. */
  query?: string;
  /** 1-based page; omitted from the URL when 1. */
  page?: number;
}

/** Page size for the activity log (local view state, not serialized). */
export const ACTIVITY_PAGE_SIZE = 25;

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
  "screen",
] as const;

export type SessionTab = (typeof SESSION_TABS)[number];

export function isSessionTab(value: string): value is SessionTab {
  return (SESSION_TABS as readonly string[]).includes(value);
}

export function parseHash(hash: string): Route {
  const clean = hash.startsWith("#") ? hash.slice(1) : hash;
  if (clean.startsWith(PROJECTS_PREFIX)) {
    const [path, query] = splitQuery(clean);
    const encoded = path.slice(PROJECTS_PREFIX.length);
    if (encoded === "") return fleetRoute(query);
    return {
      view: "project",
      directory: decodeURIComponent(encoded),
      ...fleetQuery(query),
    };
  }
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
  if (path === ACTIVITY_PATH) return { view: "activity", ...activityQuery(query) };
  if (path === "/" || path === "") return fleetRoute(query);
  return { view: "fleet" };
}

/**
 * Activity log query params: `q=<text>` pins a search, `page=<n>` pins a
 * page. Defaults are omitted so bare `#/activity` stays bare.
 */
function activityQuery(query: string): ActivityQuery {
  if (query === "") return {};
  const params = new URLSearchParams(query);
  const q = params.get("q")?.trim() ?? "";
  const page = parsePageParam(query);
  return {
    ...(q === "" ? {} : { query: q }),
    ...(page === undefined ? {} : { page }),
  };
}

export function buildActivityHash(query: ActivityQuery = {}): string {
  const params = new URLSearchParams();
  if (query.query !== undefined && query.query !== "") {
    params.set("q", query.query);
  }
  if (query.page !== undefined && query.page > 1) {
    params.set("page", `${Math.trunc(query.page)}`);
  }
  const qs = params.toString();
  // Hash routes carry the leading # (paths above are #-stripped for parsing).
  return qs === "" ? `#${ACTIVITY_PATH}` : `#${ACTIVITY_PATH}?${qs}`;
}

/**
 * Fleet query params (additive, shareable): `status=<filter>` pins a status
 * chip, `live=1` pins the live-only toggle, `harness=pi|opencode` slices by
 * agent runtime. Unknown values fall back to the unfiltered fleet so
 * hand-typed URLs never strand the operator. Defaults are omitted from the
 * Route so `{ view: "fleet" }` equality holds.
 */
function fleetRoute(query: string): Route {
  return { view: "fleet", ...fleetQuery(query) };
}

/**
 * Shared fleet/project query params. Defaults are omitted so `{ view:
 * "fleet" }` equality holds and bare `#/` stays bare. `from` wins over
 * `since`; unknown values fall back to no filter.
 */
function fleetQuery(query: string): FleetQuery {
  if (query === "") return {};
  const status = parseStatusParam(query);
  const liveOnly = parseLiveParam(query);
  const bucket = parseBucketParam(query);
  const from = parseFromParam(query);
  const since = from === undefined ? parseSinceParam(query) : undefined;
  const page = parsePageParam(query);
  const harness = parseHarnessParam(query);
  return {
    ...(status === "all" ? {} : { status }),
    ...(liveOnly ? { liveOnly: true } : {}),
    ...(harness === "all" ? {} : { harness }),
    ...(bucket === undefined ? {} : { bucket }),
    ...(since === undefined ? {} : { since }),
    ...(from === undefined ? {} : { from }),
    ...(page === undefined ? {} : { page }),
  };
}

export function parseBucketParam(query: string): IdleBucket | undefined {
  const raw =
    new URLSearchParams(query).get("bucket")?.trim().toLowerCase() ?? "";
  if (!(IDLE_BUCKETS as readonly string[]).includes(raw)) return undefined;
  return raw as IdleBucket;
}

export function parseSinceParam(query: string): DatePreset | undefined {
  const raw = new URLSearchParams(query).get("since")?.trim().toLowerCase();
  if (raw === undefined || raw === "all" || raw === "") return undefined;
  if (!(DATE_PRESETS as readonly string[]).includes(raw)) return undefined;
  return raw as DatePreset;
}

export function parseFromParam(query: string): string | undefined {
  const raw = new URLSearchParams(query).get("from")?.trim() ?? "";
  if (raw === "" || parseLocalDate(raw) === null) return undefined;
  return raw;
}

export function parsePageParam(query: string): number | undefined {
  const raw = new URLSearchParams(query).get("page")?.trim() ?? "";
  if (!/^\d+$/.test(raw)) return undefined;
  const page = Number(raw);
  return Number.isSafeInteger(page) && page > 1 ? page : undefined;
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

/** `harness=opencode|pi|all`. Unknown values fall back to `"all"`. */
export function parseHarnessParam(query: string): HarnessFilter {
  const raw =
    new URLSearchParams(query).get("harness")?.trim().toLowerCase() ?? "";
  return (HARNESS_FILTERS as readonly string[]).includes(raw)
    ? (raw as HarnessFilter)
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

function fleetParams(query: FleetQuery): URLSearchParams {
  const params = new URLSearchParams();
  if (query.status !== undefined && query.status !== "all") {
    params.set("status", query.status);
  }
  if (query.liveOnly === true) params.set("live", "1");
  if (query.harness !== undefined && query.harness !== "all") {
    params.set("harness", query.harness);
  }
  if (query.bucket !== undefined && query.bucket !== "active-recent") {
    params.set("bucket", query.bucket);
  }
  if (query.from !== undefined && query.from !== "") {
    params.set("from", query.from);
  } else if (query.since !== undefined && query.since !== "all") {
    params.set("since", query.since);
  }
  if (query.page !== undefined && query.page > 1) {
    params.set("page", `${Math.trunc(query.page)}`);
  }
  return params;
}

function hashWithQuery(path: string, query: FleetQuery): string {
  const params = fleetParams(query).toString();
  return params === "" ? path : `${path}?${params}`;
}

export function buildFleetQueryHash(query: FleetQuery = {}): string {
  return hashWithQuery("#/", query);
}

export function buildFleetHash(
  status: StatusFilter = "all",
  liveOnly = false,
  query: Omit<FleetQuery, "status" | "liveOnly"> = {},
): string {
  return buildFleetQueryHash({ ...query, status, liveOnly });
}

export function buildProjectHash(
  directory: string,
  query: FleetQuery = {},
): string {
  return hashWithQuery(
    `#${PROJECTS_PREFIX}${encodeURIComponent(directory)}`,
    query,
  );
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
