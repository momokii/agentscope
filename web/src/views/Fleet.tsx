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
  RECENCY_LABELS,
  applyFleetFilters,
  countByIdleBucket,
  countByStatus,
  groupFleetRows,
  isExited,
  paginateRows,
  sortFleetRows,
} from "../lib/fleetSort";
import type {
  DateFilter,
  IdleBucket,
  StatusFilter,
} from "../lib/fleetSort";
import type { FleetQuery } from "../lib/routing";
import { formatAge, formatCompact, formatUSD } from "../lib/format";
import { resolveProjectHeader } from "../lib/projects";
import KpiStrip from "../components/KpiStrip";
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
  /** Drill-in directory; null/undefined renders the flat fleet. */
  projectDirectory?: string | null;
  bucket?: IdleBucket;
  date?: DateFilter;
  page?: number;
  /** Hash-serialized query patch (bucket/date/page/project filters). */
  onQueryChange?: (patch: FleetQuery) => void;
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
      <div className="session-title">{row.title}</div>
      <div className="session-cwd mono">
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
  projectDirectory = null,
  bucket = "all",
  date = DEFAULT_DATE_FILTER,
  page = 1,
  onQueryChange,
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

  /** Per-status counts over the scoped snapshot — one pass per tick. */
  const statusCounts = useMemo(() => countByStatus(scopeRows), [scopeRows]);

  const bucketCounts = useMemo(
    () => countByIdleBucket(scopeRows),
    [scopeRows],
  );

  const rows = useMemo(() => {
    if (!snapshot) return [];
    const pinned = new Set(pins);
    return sortFleetRows(
      applyFleetFilters(snapshot.sessions, query, statusFilter, liveOnly, {
        bucket,
        date,
        directory: projectDirectory,
        nowMs: now,
      }),
    ).filter((row) => !pinned.has(row.id));
  }, [
    snapshot,
    query,
    statusFilter,
    liveOnly,
    pins,
    bucket,
    date,
    projectDirectory,
    now,
  ]);

  const pageSlice = useMemo(
    () => paginateRows(rows, page),
    [rows, page],
  );

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

  const kpis = snapshot?.kpis ?? zeroKpis();

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
        <SessionCell row={row} />
        <td>
          <span className="model-chip">{row.model}</span>
        </td>
        <td className="mono">{formatCompact(row.tokens.total)}</td>
        <td>
          <Sparkline data={row.spark} />
        </td>
        <td className="mono">{formatUSD(row.cost)}</td>
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
        <td colSpan={9}>
          ↳ {row.subagent_count} child session
          {row.subagent_count === 1 ? "" : "s"} · tokens and cost include the
          subagent roll-up
        </td>
      </tr>,
    ];
  };

  return (
    <div className="fleet">
      {projectDirectory !== null && (
        <ProjectHeader
          header={resolveProjectHeader(snapshot, projectDirectory)}
          nowMs={now}
        />
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

      <StatusFilterBar
        counts={statusCounts.counts}
        total={statusCounts.total}
        value={statusFilter}
        liveOnly={liveOnly}
        onSelect={(status) => onFilterChange?.(status, liveOnly)}
        onToggleLiveOnly={() => onFilterChange?.(statusFilter, !liveOnly)}
      />

      <RecencyFilterBar
        counts={bucketCounts}
        bucket={bucket}
        onSelectBucket={changeBucket}
        date={date}
        onChangeDate={changeDate}
      />

      <KpiStrip
        kpis={kpis}
        prevKpis={prevSnapshot?.kpis ?? null}
        phase={live ? phase : "closed"}
        lastSeq={snapshot?.seq ?? 0}
      />

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
                    <td colSpan={9}>Pinned</td>
                  </tr>
                  {pinnedRows.flatMap((row) => renderRow(row))}
                </Fragment>
              )}
              {groups.map((group) => (
                <Fragment key={group.key}>
                  <tr key={`group-${group.key}`} className="group-header">
                    <td colSpan={9}>{RECENCY_LABELS[group.key]}</td>
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
        />
      )}
    </div>
  );
}
