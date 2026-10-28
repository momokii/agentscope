/**
 * Fleet view (W2-1): fleet table consuming a Snapshot prop.
 *
 * - Rows sorted running→exited (see lib/fleetSort), grouped Today/Yesterday/Older.
 * - Status badge: glyph + wash + label (StatusBadge), never color alone.
 * - LIVE toggle + paused/stale chips driven by client status + Snapshot.stale.
 * - Children are pure: Fleet owns search query + subagent expansion state.
 */

import { Fragment, useMemo, useState } from "react";
import { Pause, Pin, Play, Search } from "lucide-react";

import type { SessionRow, Snapshot } from "../lib/sse";
import type { ConnectionPhase } from "../lib/sse";
import {
  DEFAULT_DATE_FILTER,
  DEFAULT_IDLE_BUCKET,
  IDLE_BUCKET_LABELS,
  PAGE_SIZE,
  RECENCY_LABELS,
  applyFleetFilters,
  countByHarness,
  countByIdleBucket,
  countByStatus,
  groupFleetRows,
  isExited,
  matchesDateFilter,
  matchesIdleBucket,
  paginateRows,
  sortFleetRows,
} from "../lib/fleetSort";
import { sumSparks } from "../lib/rate";
import type {
  DateFilter,
  HarnessFilter,
  IdleBucket,
  StatusFilter,
} from "../lib/fleetSort";
import type { FleetQuery } from "../lib/routing";
import { formatAge, formatCompact, formatUSD } from "../lib/format";
import { resolveProjectHeader } from "../lib/projects";
import HarnessBadge from "../components/HarnessBadge";
import DataGuide from "../components/DataGuide";
import RefreshButton from "../components/RefreshButton";
import HarnessFilterBar, {
  HARNESS_FILTER_LABELS,
} from "../components/HarnessFilterBar";
import ProjectInsight from "../components/ProjectInsight";
import RateChart from "../components/RateChart";
import KpiStrip, { costModeFor } from "../components/KpiStrip";
import Pagination from "../components/Pagination";
import ProjectHeader from "../components/ProjectHeader";
import RecencyFilterBar from "../components/RecencyFilterBar";
import RelativeTime from "../components/RelativeTime";
import Sparkline from "../components/Sparkline";
import StatusBadge from "../components/StatusBadge";
import StatusDistribution from "../components/StatusDistribution";
import StatusFilterBar, {
  STATUS_FILTER_LABELS,
} from "../components/StatusFilterBar";
import type { KPIs } from "../lib/sse";

export interface FleetProps {
  snapshot: Snapshot | null;
  /** Previous snapshot for KPI deltas; null on first tick. */
  prevSnapshot: Snapshot | null;
  phase: ConnectionPhase;
  attempt: number;
  live: boolean;
  /** Wall-clock ms since the last applied snapshot frame (stale chip). */
  snapshotAgeMs: number | null;
  onToggleLive: () => void;
  /** Deep-link selection: hash route `#/sessions/:id` highlights this row. */
  selectedId: string | null;
  /** Pinned session ids in quick-switch order (slots 1–9). */
  pins: readonly string[];
  onTogglePin: (id: string) => void;
  /** Row click / CmdBar Enter: sets the hash route to this session. */
  onSelect: (id: string) => void;
  /** Injectable clock for tests; defaults to Date.now(). */
  nowMs?: number;
  /**
   * Status chip + live-only toggle, hash-driven from App (shareable links,
   * back/forward). Defaults keep the unfiltered fleet for existing callers.
   */
  statusFilter?: StatusFilter;
  liveOnly?: boolean;
  onFilterChange?: (status: StatusFilter, liveOnly: boolean) => void;
  /** Harness slice, hash-driven from App (shareable links). */
  harnessFilter?: HarnessFilter;
  /** Drill-in directory; null/undefined renders the flat fleet. */
  projectDirectory?: string | null;
  bucket?: IdleBucket;
  date?: DateFilter;
  page?: number;
  /** Hash-serialized query patch (bucket/date/page/project filters). */
  onQueryChange?: (patch: FleetQuery) => void;
  /** Manual re-pull of /api/fleet through the snapshot pipeline. */
  onRefresh?: () => void;
  /** True while a manual refresh is in flight. */
  refreshing?: boolean;
}

function zeroKpis(): KPIs {
  return {
    active: 0,
    waiting_input: 0,
    tokens_per_hour: 0,
    cost_today: 0,
    errors: 0,
    sse_connected: false,
  };
}

function SessionCell({ row }: { row: SessionRow }) {
  const cwd = row.pane.cwd !== "" ? row.pane.cwd : row.directory;
  return (
    <td>
      <div className="session-title" title={row.title}>
        {row.title}
      </div>
      <div
        className="session-cwd mono"
        title={`project ${row.project} · ${row.directory}${
          row.pane.cwd !== "" ? ` · live pane at ${row.pane.cwd}` : ""
        }`}
      >
        {row.project} · {cwd}
        {isExited(row) ? " · exited" : ""}
      </div>
    </td>
  );
}

function SubagentCell({
  row,
  expanded,
  onToggle,
}: {
  row: SessionRow;
  expanded: boolean;
  onToggle: () => void;
}) {
  if (row.subagent_count <= 0) return <td />;
  return (
    <td>
      <button
        type="button"
        className="subagent-chip"
        aria-expanded={expanded}
        aria-label={`${expanded ? "Collapse" : "Expand"} ${row.subagent_count} subagents for ${row.title}`}
        title="Child sessions (parent_id link); row tokens/cost include this roll-up"
        onClick={onToggle}
      >
        <span aria-hidden="true">↳</span>
        <span className="count">{row.subagent_count}</span>
      </button>
    </td>
  );
}

export default function Fleet({
  snapshot,
  prevSnapshot,
  phase,
  attempt,
  live,
  snapshotAgeMs,
  onToggleLive,
  selectedId,
  pins,
  onTogglePin,
  onSelect,
  nowMs,
  statusFilter = "all",
  liveOnly = false,
  onFilterChange,
  harnessFilter = "all",
  projectDirectory = null,
  bucket = "all",
  date = DEFAULT_DATE_FILTER,
  page = 1,
  onQueryChange,
  onRefresh,
  refreshing = false,
}: FleetProps) {
  const [query, setQuery] = useState("");
  const [expanded, setExpanded] = useState<ReadonlySet<string>>(new Set());
  const now = nowMs ?? Date.now();

  const scopeRows = useMemo(
    () =>
      projectDirectory === null
        ? (snapshot?.sessions ?? [])
        : (snapshot?.sessions ?? []).filter(
            (row) => row.directory === projectDirectory,
          ),
    [snapshot, projectDirectory],
  );

  /**
   * Faceted scopes: every chip bar counts the rows left by ALL OTHER
   * filters, so counts stay informative instead of mirroring the
   * selection — while the KPI strip follows the operator's time window
   * (a 24h filter really does shrink Active/Waiting/Errors).
   *
   * - statusScope: everything except the status chip + live-only toggle.
   *   Feeds StatusDistribution, the status chips, and the KPI counts.
   * - harnessScope: everything except the harness slice.
   * - bucketScope: everything except the recency bucket + date window.
   * - projectScope: directory + date + bucket + search (never status /
   *   harness / live-only — the header describes the project's window
   *   and the insight legend shows its harness split).
   */
  const statusScope = useMemo(
    () =>
      applyFleetFilters(scopeRows, query, "all", false, {
        bucket,
        date,
        harness: harnessFilter,
        nowMs: now,
      }),
    [scopeRows, query, bucket, date, harnessFilter, now],
  );
  const harnessScope = useMemo(
    () =>
      applyFleetFilters(scopeRows, query, statusFilter, liveOnly, {
        bucket,
        date,
        nowMs: now,
      }),
    [scopeRows, query, statusFilter, liveOnly, bucket, date, now],
  );
  const bucketScope = useMemo(
    () =>
      applyFleetFilters(scopeRows, query, statusFilter, liveOnly, {
        bucket: "all",
        date,
        harness: harnessFilter,
        nowMs: now,
      }),
    [scopeRows, query, statusFilter, liveOnly, date, harnessFilter, now],
  );

  /** Per-status counts over the status scope — one pass per tick. */
  const statusCounts = useMemo(
    () => countByStatus(statusScope),
    [statusScope],
  );

  /** Per-harness counts over the harness scope — one pass per tick. */
  const harnessCounts = useMemo(
    () => countByHarness(harnessScope),
    [harnessScope],
  );

  const bucketCounts = useMemo(
    () => countByIdleBucket(bucketScope),
    [bucketScope],
  );

  /** KPI strip scope: Active/Waiting/Errors describe the status scope;
   * throughput, costs, and live presence stay global measurements. */
  const scopedKpis = useMemo((): KPIs | null => {
    if (!snapshot) return null;
    let active = 0;
    let waiting = 0;
    let errors = 0;
    for (const row of statusScope) {
      if (row.status === "running") active++;
      else if (row.status === "waiting-input") waiting++;
      else if (row.status === "error") errors++;
    }
    return { ...snapshot.kpis, active, waiting_input: waiting, errors };
  }, [snapshot, statusScope]);

  const rows = useMemo(() => {
    if (!snapshot) return [];
    const pinned = new Set(pins);
    return sortFleetRows(
      applyFleetFilters(snapshot.sessions, query, statusFilter, liveOnly, {
        bucket,
        date,
        directory: projectDirectory,
        harness: harnessFilter,
        nowMs: now,
      }),
    ).filter((row) => !pinned.has(row.id));
  }, [
    snapshot,
    query,
    statusFilter,
    liveOnly,
    harnessFilter,
    pins,
    bucket,
    date,
    projectDirectory,
    now,
  ]);

  // Local view preference (not URL state): operator-selectable page size.
  const [pageSize, setPageSize] = useState(PAGE_SIZE);
  const pageSlice = useMemo(
    () => paginateRows(rows, page, pageSize),
    [rows, page, pageSize],
  );
  const changePageSize = (size: number) => {
    setPageSize(size);
    onQueryChange?.({ page: 1 });
  };

  const changeQuery = (value: string) => {
    setQuery(value);
    if (page > 1) onQueryChange?.({ page: 1 });
  };

  const changeBucket = (next: IdleBucket) => {
    onQueryChange?.({ bucket: next, page: 1 });
  };

  const changeDate = (next: DateFilter) => {
    const resetBucket =
      next.from !== null || next.preset !== "all"
        ? bucket === DEFAULT_IDLE_BUCKET
          ? { bucket: "all" as IdleBucket }
          : {}
        : {};
    onQueryChange?.({
      since: next.preset === "all" ? undefined : next.preset,
      from: next.from ?? undefined,
      page: 1,
      ...resetBucket,
    });
  };

  const resetFilters = () => {
    setQuery("");
    onFilterChange?.("all", false);
    onQueryChange?.({
      bucket: DEFAULT_IDLE_BUCKET,
      harness: undefined,
      since: undefined,
      from: undefined,
      page: 1,
    });
  };

  const activeFilterBits: string[] = [];
  {
    const needle = query.trim();
    if (needle !== "") activeFilterBits.push(`“${needle}”`);
    if (statusFilter !== "all")
      activeFilterBits.push(`status ${STATUS_FILTER_LABELS[statusFilter]}`);
    if (liveOnly) activeFilterBits.push("live only");
    if (harnessFilter !== "all")
      activeFilterBits.push(`harness ${HARNESS_FILTER_LABELS[harnessFilter]}`);
    if (bucket !== "all" && bucket !== DEFAULT_IDLE_BUCKET)
      activeFilterBits.push(`bucket ${IDLE_BUCKET_LABELS[bucket]}`);
    if (date.from !== null && date.from !== undefined) {
      activeFilterBits.push(`from ${date.from}`);
    } else if (date.preset !== "all") {
      activeFilterBits.push(`last ${date.preset}`);
    }
  }
  const filterActive = activeFilterBits.length > 0;

  /** Pinned section: pin-order slots 1–9, only ids present in the snapshot. */
  const pinnedRows = useMemo(() => {
    if (!snapshot) return [];
    const byId = new Map(snapshot.sessions.map((row) => [row.id, row]));
    return pins
      .map((id) => byId.get(id))
      .filter((row): row is SessionRow => row !== undefined)
      .filter(
        (row) =>
          projectDirectory === null || row.directory === projectDirectory,
      );
  }, [snapshot, pins, projectDirectory]);

  const groups = useMemo(
    () => groupFleetRows(pageSlice.rows, now),
    [pageSlice.rows, now],
  );

  const toggleExpanded = (id: string) => {
    setExpanded((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  };

  const kpis = scopedKpis ?? zeroKpis();

  /** Previous-tick KPIs under the same status scope, so deltas compare
   * like with like when a date/bucket filter is active. */
  const prevKpis = useMemo((): KPIs | null => {
    if (!prevSnapshot) return null;
    const prevScope = applyFleetFilters(
      prevSnapshot.sessions.filter(
        (row) =>
          projectDirectory === null || row.directory === projectDirectory,
      ),
      query,
      "all",
      false,
      { bucket, date, harness: harnessFilter, nowMs: now },
    );
    let active = 0;
    let waiting = 0;
    let errors = 0;
    for (const row of prevScope) {
      if (row.status === "running") active++;
      else if (row.status === "waiting-input") waiting++;
      else if (row.status === "error") errors++;
    }
    return { ...prevSnapshot.kpis, active, waiting_input: waiting, errors };
  }, [prevSnapshot, query, date, harnessFilter, now]);

  const renderRow = (row: SessionRow) => {
    const isOpen = expanded.has(row.id);
    const isSelected = selectedId === row.id;
    const pinIndex = pins.indexOf(row.id);
    const main = (
      <tr
        key={row.id}
        id={`row-${row.id}`}
        data-session-id={row.id}
        data-selected={isSelected ? "true" : "false"}
        className={isSelected ? "fleet-row fleet-row-selected" : "fleet-row"}
        onClick={() => onSelect(row.id)}
      >
        <td>
          <StatusBadge status={row.status} />
        </td>
        <td>
          <HarnessBadge harness={row.harness} />
        </td>
        <SessionCell row={row} />
        <td>
          <span className="model-chip" title={`Model provider/model-id: ${row.model}`}>
            {row.model}
          </span>
        </td>
        <td
          className="mono"
          title={`Cumulative tokens incl. subagent roll-up (${formatCompact(row.tokens.total)} total)`}
        >
          {formatCompact(row.tokens.total)}
        </td>
        <td title="Tokens/min per poll tick, newest last">
          <Sparkline data={row.spark} />
        </td>
        <td
          className="mono"
          title={`Cumulative USD cost incl. subagent roll-up (${formatUSD(row.cost)})`}
        >
          {formatUSD(row.cost)}
        </td>
        <td>
          <RelativeTime
            updatedRelativeMs={row.updated_relative_ms}
            nowMs={nowMs}
          />
        </td>
        <SubagentCell
          row={row}
          expanded={isOpen}
          onToggle={() => toggleExpanded(row.id)}
        />
        <td>
          <button
            type="button"
            className={
              pinIndex >= 0 ? "pin-toggle pin-toggle-on" : "pin-toggle"
            }
            aria-pressed={pinIndex >= 0}
            aria-label={`${pinIndex >= 0 ? "Unpin" : "Pin"} session ${row.title}`}
            title="Pin to the top section (keys 1–9 jump to pins)"
            onClick={(event) => {
              event.stopPropagation();
              onTogglePin(row.id);
            }}
          >
            <Pin aria-hidden="true" size={12} />
            {pinIndex >= 0 && (
              <span className="pin-badge mono">{pinIndex + 1}</span>
            )}
          </button>
        </td>
      </tr>
    );
    if (!isOpen) return [main];
    return [
      main,
      <tr key={`${row.id}-sub`} className="subagent-detail">
        <td colSpan={10}>
          ↳ {row.subagent_count} child session
          {row.subagent_count === 1 ? "" : "s"} · tokens and cost include the
          subagent roll-up
        </td>
      </tr>,
    ];
  };

  /**
   * Project member rows: directory + date + bucket + search scoped (never
   * the status/harness/live-only slices — the header describes the
   * project's window and the insight legend shows its harness split).
   * Powers the header aggregates and the insight section alike.
   */
  const projectRows = useMemo(() => {
    if (projectDirectory === null || !snapshot) return [];
    const needle = query.trim().toLowerCase();
    return snapshot.sessions.filter(
      (row) =>
        row.directory === projectDirectory &&
        matchesIdleBucket(row, bucket) &&
        matchesDateFilter(row.updated_relative_ms, date, now) &&
        (needle === "" ||
          [row.id, row.title, row.project, row.directory, row.model].some(
            (field) => field.toLowerCase().includes(needle),
          )),
    );
  }, [snapshot, projectDirectory, bucket, date, query, now]);

  /** Project header aggregates recomputed over the same date/bucket window
   * (live presence stays global runtime truth). Tokens/cost switch to
   * trailing-window sums from the snapshot's per-session map under a 24h
   * / 7d / 30d preset (labels name the window); today/custom/all keep
   * lifetime totals (per-session midnight attribution doesn't exist). */
  const projectWindow = useMemo((): "24h" | "7d" | "30d" | null => {
    if (date.from !== null && date.from !== undefined) return null;
    if (date.preset === "24h" || date.preset === "7d" || date.preset === "30d") {
      return date.preset;
    }
    return null;
  }, [date]);
  const projectHeader = useMemo(() => {
    if (projectDirectory === null) return null;
    const base = resolveProjectHeader(snapshot, projectDirectory);
    let running = 0;
    let waiting = 0;
    let tokens = 0;
    let cost = 0;
    let lastActivity: number | undefined;
    const windows = snapshot?.session_windows;
    for (const row of projectRows) {
      if (row.status === "running") running++;
      else if (row.status === "waiting-input") waiting++;
      if (projectWindow !== null && windows !== undefined) {
        const w = windows[row.id];
        if (w !== undefined) {
          if (projectWindow === "24h") {
            tokens += w.tokens_24h;
            cost += w.cost_24h;
          } else if (projectWindow === "7d") {
            tokens += w.tokens_7d;
            cost += w.cost_7d;
          } else {
            tokens += w.tokens_30d;
            cost += w.cost_30d;
          }
        }
      } else {
        tokens += row.tokens.total;
        cost += row.cost;
      }
      if (
        lastActivity === undefined ||
        row.updated_relative_ms < lastActivity
      ) {
        lastActivity = row.updated_relative_ms;
      }
    }
    return {
      header: {
        ...base,
        sessions: projectRows.length,
        running,
        waiting_input: waiting,
        tokens_total: tokens,
        cost_total: cost,
        last_activity_ms: lastActivity,
      },
      tokensLabel: projectWindow === null ? "Tokens" : `Tokens ${projectWindow}`,
      costLabel: projectWindow === null ? "Cost total" : `Cost ${projectWindow}`,
    };
  }, [snapshot, projectDirectory, projectRows, projectWindow]);

  /** Fleet-wide token rate (element-wise spark sums, live data only). */
  const fleetRate = useMemo(
    () => (snapshot ? sumSparks(snapshot.sessions) : []),
    [snapshot],
  );

  return (
    <div className="fleet">
      {projectDirectory !== null && projectHeader !== null && (
        <ProjectHeader
          header={projectHeader.header}
          tokensLabel={projectHeader.tokensLabel}
          costLabel={projectHeader.costLabel}
          nowMs={now}
        />
      )}
      {projectDirectory !== null && projectRows.length > 0 && (
        <ProjectInsight rows={projectRows} />
      )}
      <div className="fleet-toolbar">
        <span className="fleet-title">
          {projectDirectory !== null ? "Sessions" : "Fleet"}{" "}
          <span className="count">({rows.length})</span>
        </span>
        {!live && (
          <span className="state-chip state-chip-paused">
            <span aria-hidden="true">‖</span>
            <span>Paused</span>
          </span>
        )}
        {snapshot?.stale && (
          <span className="state-chip state-chip-stale">
            <span aria-hidden="true">◌</span>
            <span>
              Stale
              {snapshotAgeMs !== null
                ? ` · last update ${formatAge(snapshotAgeMs)}`
                : ""}
            </span>
          </span>
        )}
        <label className="fleet-search">
          <Search aria-hidden="true" />
          <input
            type="search"
            value={query}
            onChange={(event) => changeQuery(event.target.value)}
            placeholder="Filter id, title, cwd, model…"
            aria-label="Filter fleet sessions"
          />
        </label>
        <DataGuide />
        <RefreshButton onRefresh={onRefresh} refreshing={refreshing} />
        <button
          type="button"
          className="live-toggle"
          aria-pressed={live}
          aria-label={live ? "Pause live updates" : "Resume live updates"}
          onClick={onToggleLive}
        >
          <span className="dot" aria-hidden="true" />
          {live ? <Play aria-hidden="true" size={12} /> : <Pause aria-hidden="true" size={12} />}
          <span>{live ? "Live" : "Paused"}</span>
        </button>
      </div>

      {phase === "reconnecting" && (
        <div className="reconnect-banner" role="status">
          <span className="dot" aria-hidden="true" />
          <span>
            reconnecting… attempt {attempt} · showing last snapshot (seq{" "}
            {snapshot?.seq ?? 0})
          </span>
        </div>
      )}

      {snapshot && projectDirectory === null && (
        <RateChart values={fleetRate} label="Fleet" />
      )}

      <StatusFilterBar
        counts={statusCounts.counts}
        total={statusCounts.total}
        value={statusFilter}
        liveOnly={liveOnly}
        onSelect={(status) => onFilterChange?.(status, liveOnly)}
        onToggleLiveOnly={() => onFilterChange?.(statusFilter, !liveOnly)}
      />

      <HarnessFilterBar
        counts={harnessCounts}
        value={harnessFilter}
        onSelect={(harness) => onQueryChange?.({ harness, page: 1 })}
      />

      <RecencyFilterBar
        counts={bucketCounts}
        bucket={bucket}
        onSelectBucket={changeBucket}
        date={date}
        onChangeDate={changeDate}
      />

      {/* Fleet-wide strip only: the project drill-in reports scoped
          numbers via ProjectHeader + ProjectInsight instead. */}
      {projectDirectory === null && (
        <KpiStrip
          kpis={kpis}
          prevKpis={prevKpis}
          phase={live ? phase : "closed"}
          lastSeq={snapshot?.seq ?? 0}
          frameAgeMs={snapshotAgeMs}
          costMode={costModeFor(date)}
        />
      )}

      <StatusDistribution
        counts={statusCounts.counts}
        total={statusCounts.total}
      />

      {!snapshot ? (
        <div className="empty-state">
          Waiting for the first snapshot… (GET /api/events)
        </div>
      ) : rows.length === 0 ? (
        <div className="empty-state">
          {filterActive ? (
            <>
              <p>No sessions match {activeFilterBits.join(" · ")}.</p>
              <button
                type="button"
                className="empty-reset"
                onClick={resetFilters}
              >
                Show all sessions
              </button>
            </>
          ) : (
            "No sessions in this fleet."
          )}
        </div>
      ) : (
        <div className="fleet-table-wrap">
          <table className="fleet-table">
            <thead>
              <tr>
                <th scope="col">Status</th>
                <th scope="col">Harness</th>
                <th scope="col">Session</th>
                <th scope="col">Model</th>
                <th scope="col">Tokens</th>
                <th scope="col">Trend</th>
                <th scope="col">Cost</th>
                <th scope="col">Updated</th>
                <th scope="col">Sub</th>
                <th scope="col">Pin</th>
              </tr>
            </thead>
            <tbody>
              {pinnedRows.length > 0 && (
                <Fragment key="group-pinned">
                  <tr className="group-header">
                    <td colSpan={10}>Pinned</td>
                  </tr>
                  {pinnedRows.flatMap((row) => renderRow(row))}
                </Fragment>
              )}
              {groups.map((group) => (
                <Fragment key={group.key}>
                  <tr key={`group-${group.key}`} className="group-header">
                    <td colSpan={10}>{RECENCY_LABELS[group.key]}</td>
                  </tr>
                  {group.rows.flatMap((row) => renderRow(row))}
                </Fragment>
              ))}
            </tbody>
          </table>
        </div>
      )}

      {rows.length > 0 && (
        <Pagination
          page={pageSlice.page}
          totalPages={pageSlice.totalPages}
          total={pageSlice.total}
          onPageChange={(next) => onQueryChange?.({ page: next })}
          pageSize={pageSize}
          onPageSizeChange={changePageSize}
        />
      )}
    </div>
  );
}
