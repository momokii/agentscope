import { useMemo, useState } from "react";
import { Activity, Pause, Play, Search } from "lucide-react";

import type {
  ConnectionPhase,
  KPIs,
  ProjectSummary,
  SessionRow,
} from "../lib/sse";
import { formatAge, formatCompact, formatUSD } from "../lib/format";
import {
  DATE_PRESETS,
  DATE_PRESET_LABELS,
  matchesDateFilter,
} from "../lib/fleetSort";
import type { DatePreset } from "../lib/fleetSort";
import {
  filterProjects,
  projectDisplayName,
  projectIsLive,
  sortProjects,
} from "../lib/projects";
import { buildProjectHash } from "../lib/routing";
import DataGuide from "../components/DataGuide";
import RefreshButton from "../components/RefreshButton";
import KpiStrip, { costModeFor } from "../components/KpiStrip";
import Pagination from "../components/Pagination";
import RelativeTime from "../components/RelativeTime";

export interface ProjectsProps {
  projects: readonly ProjectSummary[];
  /** Fleet sessions (for date-scoped KPI counts on the landing). */
  sessions?: readonly SessionRow[];
  nowMs?: number;
  live?: boolean;
  onToggleLive?: () => void;
  phase?: ConnectionPhase;
  attempt?: number;
  stale?: boolean;
  snapshotAgeMs?: number | null;
  kpis?: KPIs;
  prevKpis?: KPIs | null;
  lastSeq?: number;
  /** Manual re-pull of /api/fleet through the snapshot pipeline. */
  onRefresh?: () => void;
  /** True while a manual refresh is in flight. */
  refreshing?: boolean;
}

function numberText(value: number | undefined): string {
  return typeof value === "number" && Number.isFinite(value) ? `${value}` : "—";
}

function compactText(value: number | undefined): string {
  return typeof value === "number" && Number.isFinite(value)
    ? formatCompact(value)
    : "—";
}

function usdText(value: number | undefined): string {
  return typeof value === "number" && Number.isFinite(value)
    ? formatUSD(value)
    : "—";
}

/** Default cards per landing page (operator-selectable 5–50). */
const PROJECTS_DEFAULT_SIZE = 10;

const PANES_TITLE =
  "OpenCode panes in this directory (presence signal; may briefly include a pane whose process just exited)";

function livePanes(project: ProjectSummary): number | undefined {
  const value = (project as ProjectSummary & { live_panes?: unknown }).live_panes;
  return typeof value === "number" && Number.isFinite(value) ? value : undefined;
}

function procsText(project: ProjectSummary): string {
  const panes = livePanes(project);
  if (panes !== undefined) {
    return panes === 0 ? "no panes" : `${panes} pane${panes === 1 ? "" : "s"}`;
  }
  const procs = project.live_procs;
  if (typeof procs !== "number" || !Number.isFinite(procs)) return "— procs";
  if (procs === 0) return "no procs";
  return `${procs} proc${procs === 1 ? "" : "s"}`;
}

function presenceTitle(project: ProjectSummary): string {
  return livePanes(project) !== undefined
    ? PANES_TITLE
    : `${procsText(project)} alive in this directory`;
}

export default function Projects({
  projects,
  sessions = [],
  nowMs,
  live = true,
  onToggleLive,
  phase = "connected",
  attempt = 0,
  stale = false,
  snapshotAgeMs = null,
  kpis,
  prevKpis = null,
  lastSeq = 0,
  onRefresh,
  refreshing = false,
}: ProjectsProps) {
  const [query, setQuery] = useState("");
  const [since, setSince] = useState<DatePreset>("all");
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(PROJECTS_DEFAULT_SIZE);
  const now = nowMs ?? Date.now();

  const rows = useMemo(() => {
    const searched = filterProjects(projects, query);
    const dated =
      since === "all"
        ? searched
        : searched.filter((p) =>
            matchesDateFilter(p.last_activity_ms ?? Number.POSITIVE_INFINITY, { preset: since, from: null }, now),
          );
    return sortProjects(dated);
  }, [projects, query, since, now]);

  const pageCount = Math.max(1, Math.ceil(rows.length / pageSize));
  const safePage = Math.min(Math.max(1, page), pageCount);
  const pageRows = useMemo(
    () =>
      rows.slice((safePage - 1) * pageSize, safePage * pageSize),
    [rows, safePage, pageSize],
  );
  const changePageSize = (size: number) => {
    setPageSize(size);
    setPage(1);
  };

  // Landing totals follow the search + active-since filters (like the
  // cards below): a 24h slice shrinks these numbers instead of showing
  // the all-time fleet next to a filtered list.
  const totals = useMemo(() => {
    let live_procs = 0;
    let running = 0;
    let waiting_input = 0;
    let live_projects = 0;
    for (const project of rows) {
      live_procs += project.live_procs ?? 0;
      running += project.running ?? 0;
      waiting_input += project.waiting_input ?? 0;
      if (projectIsLive(project)) live_projects += 1;
    }
    return { live_procs, running, waiting_input, live_projects };
  }, [rows]);

  /**
   * Landing KPI scope: Active/Waiting/Errors count sessions in the visible
   * (searched + dated) projects and the active-since window — the global
   * numbers yield to the selected time slice. Throughput, costs, and live
   * presence stay global measurements.
   */
  const scopedKpis = useMemo((): KPIs | null => {
    if (kpis === undefined) return null;
    const dirs = new Set(rows.map((p) => p.directory));
    let active = 0;
    let waiting = 0;
    let errors = 0;
    for (const row of sessions) {
      if (!dirs.has(row.directory)) continue;
      if (!matchesDateFilter(row.updated_relative_ms, { preset: since, from: null }, now)) continue;
      if (row.status === "running") active++;
      else if (row.status === "waiting-input") waiting++;
      else if (row.status === "error") errors++;
    }
    return { ...kpis, active, waiting_input: waiting, errors };
  }, [kpis, rows, sessions, since, now]);

  const resetQuery = () => {
    setQuery("");
    setSince("all");
    setPage(1);
  };
  const changeSince = (preset: DatePreset) => {
    setSince(preset);
    setPage(1);
  };
  const changeQuery = (value: string) => {
    setQuery(value);
    setPage(1);
  };

  return (
    <div className="projects">
      <div className="fleet-toolbar">
        <span className="fleet-title">
          Projects <span className="count">({rows.length})</span>
        </span>
        <span className="projects-summary mono">
          <span className={totals.live_procs > 0 ? "projects-summary-live" : ""}>
            <span aria-hidden="true">
              {totals.live_procs > 0 ? "●" : "◌"}
            </span>{" "}
            {totals.live_procs} procs
          </span>
          <span>{totals.live_projects} live projects</span>
          <span>{totals.running} running</span>
          <span>{totals.waiting_input} waiting</span>
        </span>
        {!live && (
          <span className="state-chip state-chip-paused">
            <span aria-hidden="true">‖</span>
            <span>Paused</span>
          </span>
        )}
        {stale && (
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
            placeholder="Filter name or path…"
            aria-label="Filter projects"
          />
        </label>
        <div
          className="filter-bar projects-since"
          role="group"
          aria-label="Filter projects by last activity"
        >
          {DATE_PRESETS.map((preset) => {
            const selected = since === preset;
            return (
              <button
                key={preset}
                type="button"
                className={selected ? "filter-chip filter-chip-on" : "filter-chip"}
                aria-pressed={selected}
                onClick={() => changeSince(preset)}
              >
                {selected && <span aria-hidden="true">✓</span>}
                <span>{DATE_PRESET_LABELS[preset]}</span>
              </button>
            );
          })}
        </div>
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
          {live ? (
            <Play aria-hidden="true" size={12} />
          ) : (
            <Pause aria-hidden="true" size={12} />
          )}
          <span>{live ? "Live" : "Paused"}</span>
        </button>
      </div>

      {phase === "reconnecting" && (
        <div className="reconnect-banner" role="status">
          <span className="dot" aria-hidden="true" />
          <span>reconnecting… attempt {attempt}</span>
        </div>
      )}

      {scopedKpis !== null && (
        <KpiStrip
          kpis={scopedKpis}
          prevKpis={prevKpis}
          phase={live ? phase : "closed"}
          lastSeq={lastSeq}
          frameAgeMs={snapshotAgeMs}
          costMode={costModeFor({ preset: since, from: null })}
        />
      )}

      {projects.length === 0 ? (
        <div className="empty-state">
          <p>No projects yet.</p>
          <p>Sessions appear here once opencode runs in a directory.</p>
        </div>
      ) : rows.length === 0 ? (
        <div className="empty-state">
          <p>
            No projects match
            {query.trim() !== "" && <> “{query.trim()}”</>}
            {since !== "all" && <> active {DATE_PRESET_LABELS[since].toLowerCase()}</>}.
          </p>
          <button type="button" className="empty-reset" onClick={resetQuery}>
            Show all projects
          </button>
        </div>
      ) : (
        <ul className="projects-list" aria-label="Projects ranked by activity">
          {pageRows.map((project) => {
            const isLive = projectIsLive(project);
            const name = projectDisplayName(project);
            const lastActivity = project.last_activity_ms;
            return (
              <li key={project.directory}>
                <a
                  className={
                    isLive ? "project-card project-card-live" : "project-card"
                  }
                  href={buildProjectHash(project.directory)}
                  data-directory={project.directory}
                  data-live={isLive ? "true" : "false"}
                  title={project.directory}
                  aria-label={`Open project ${name} at ${project.directory}`}
                >
                  <span
                    className={
                      isLive
                        ? "project-procs project-procs-live"
                        : "project-procs"
                    }
                    title={presenceTitle(project)}
                  >
                    <span aria-hidden="true">{isLive ? "●" : "◌"}</span>
                    <span>{procsText(project)}</span>
                  </span>

                  <span className="project-ident">
                    <span className="project-name">{name}</span>
                    <span className="project-path mono">{project.directory}</span>
                  </span>

                  <span
                    className="project-metric"
                    title="Sessions generating now in this directory"
                  >
                    <span className="project-metric-label">Running</span>
                    <span className="project-metric-value mono">
                      {numberText(project.running)}
                    </span>
                  </span>
                  <span
                    className="project-metric"
                    title="Sessions heuristically waiting on you in this directory"
                  >
                    <span className="project-metric-label">Waiting</span>
                    <span className="project-metric-value mono">
                      {numberText(project.waiting_input)}
                    </span>
                  </span>
                  <span
                    className="project-metric"
                    title="Total sessions ever opened in this directory"
                  >
                    <span className="project-metric-label">Sessions</span>
                    <span className="project-metric-value mono">
                      {numberText(project.sessions)}
                    </span>
                  </span>
                  <span
                    className="project-metric"
                    title="Lifetime tokens in this directory, both harnesses (incl. subagent roll-up)"
                  >
                    <span className="project-metric-label">Tokens</span>
                    <span className="project-metric-value mono">
                      {compactText(project.tokens_total)}
                    </span>
                  </span>
                  <span
                    className="project-metric"
                    title="Lifetime spend in this directory, both harnesses"
                  >
                    <span className="project-metric-label">Cost total</span>
                    <span className="project-metric-value mono">
                      {usdText(project.cost_total)}
                    </span>
                  </span>

                  <span className="project-activity">
                    {typeof lastActivity === "number" &&
                    Number.isFinite(lastActivity) ? (
                      <RelativeTime
                        updatedRelativeMs={lastActivity}
                        nowMs={nowMs}
                      />
                    ) : (
                      <span className="mono">—</span>
                    )}
                    <span
                      className={
                        isLive
                          ? "project-live-flag project-live-flag-on"
                          : "project-live-flag"
                      }
                    >
                      <Activity aria-hidden="true" size={12} />
                      <span>{isLive ? "LIVE" : "QUIET"}</span>
                    </span>
                  </span>
                </a>
              </li>
            );
          })}
        </ul>
      )}
      {rows.length > 0 && (
        <Pagination
          page={safePage}
          totalPages={pageCount}
          total={rows.length}
          onPageChange={setPage}
          pageSize={pageSize}
          onPageSizeChange={changePageSize}
        />
      )}
    </div>
  );
}
