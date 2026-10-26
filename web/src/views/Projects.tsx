import { useMemo, useState } from "react";
import { Activity, Pause, Play, Search } from "lucide-react";

import type { ConnectionPhase, KPIs, ProjectSummary } from "../lib/sse";
import { formatAge, formatCompact, formatUSD } from "../lib/format";
import {
  filterProjects,
  projectDisplayName,
  projectIsLive,
  sortProjects,
} from "../lib/projects";
import { buildProjectHash } from "../lib/routing";
import KpiStrip from "../components/KpiStrip";
import RelativeTime from "../components/RelativeTime";

export interface ProjectsProps {
  projects: readonly ProjectSummary[];
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

function procsText(project: ProjectSummary): string {
  const procs = project.live_procs;
  if (typeof procs !== "number" || !Number.isFinite(procs)) return "— procs";
  if (procs === 0) return "no procs";
  return `${procs} proc${procs === 1 ? "" : "s"}`;
}

export default function Projects({
  projects,
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
}: ProjectsProps) {
  const [query, setQuery] = useState("");

  const rows = useMemo(
    () => sortProjects(filterProjects(projects, query)),
    [projects, query],
  );

  const totals = useMemo(() => {
    let live_procs = 0;
    let running = 0;
    let waiting_input = 0;
    let live_projects = 0;
    for (const project of projects) {
      live_procs += project.live_procs ?? 0;
      running += project.running ?? 0;
      waiting_input += project.waiting_input ?? 0;
      if (projectIsLive(project)) live_projects += 1;
    }
    return { live_procs, running, waiting_input, live_projects };
  }, [projects]);

  const resetQuery = () => setQuery("");

  return (
    <div className="projects">
      <div className="fleet-toolbar">
        <span className="fleet-title">
          Projects <span className="count">({projects.length})</span>
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
            onChange={(event) => setQuery(event.target.value)}
            placeholder="Filter name or path…"
            aria-label="Filter projects"
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

      {kpis !== undefined && (
        <KpiStrip
          kpis={kpis}
          prevKpis={prevKpis}
          phase={live ? phase : "closed"}
          lastSeq={lastSeq}
        />
      )}

      {projects.length === 0 ? (
        <div className="empty-state">
          <p>No projects yet.</p>
          <p>Sessions appear here once opencode runs in a directory.</p>
        </div>
      ) : rows.length === 0 ? (
        <div className="empty-state">
          <p>No projects match “{query.trim()}”.</p>
          <button type="button" className="empty-reset" onClick={resetQuery}>
            Show all projects
          </button>
        </div>
      ) : (
        <ul className="projects-list" aria-label="Projects ranked by activity">
          {rows.map((project) => {
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
                    title={`${procsText(project)} alive in this directory`}
                  >
                    <span aria-hidden="true">{isLive ? "●" : "◌"}</span>
                    <span>{procsText(project)}</span>
                  </span>

                  <span className="project-ident">
                    <span className="project-name">{name}</span>
                    <span className="project-path mono">{project.directory}</span>
                  </span>

                  <span className="project-metric">
                    <span className="project-metric-label">Running</span>
                    <span className="project-metric-value mono">
                      {numberText(project.running)}
                    </span>
                  </span>
                  <span className="project-metric">
                    <span className="project-metric-label">Waiting</span>
                    <span className="project-metric-value mono">
                      {numberText(project.waiting_input)}
                    </span>
                  </span>
                  <span className="project-metric">
                    <span className="project-metric-label">Sessions</span>
                    <span className="project-metric-value mono">
                      {numberText(project.sessions)}
                    </span>
                  </span>
                  <span className="project-metric">
                    <span className="project-metric-label">Tokens</span>
                    <span className="project-metric-value mono">
                      {compactText(project.tokens_total)}
                    </span>
                  </span>
                  <span className="project-metric">
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
    </div>
  );
}
