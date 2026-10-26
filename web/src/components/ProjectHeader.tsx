import { ArrowLeft } from "lucide-react";

import type { ProjectHeaderData } from "../lib/projects";
import { formatCompact, formatUSD } from "../lib/format";
import RelativeTime from "./RelativeTime";

export interface ProjectHeaderProps {
  header: ProjectHeaderData;
  nowMs?: number;
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

export default function ProjectHeader({ header, nowMs }: ProjectHeaderProps) {
  const lastActivity = header.last_activity_ms;
  return (
    <section
      className="project-header"
      aria-label={`Project ${header.name}`}
    >
      <nav className="project-breadcrumb" aria-label="Breadcrumb">
        <a href="#/" aria-label="Back to projects">
          <ArrowLeft aria-hidden="true" size={12} />
          <span>Projects</span>
        </a>
        <span aria-hidden="true">/</span>
        <span className="project-breadcrumb-current">{header.name}</span>
      </nav>

      <div className="project-header-body">
        <div className="project-header-ident">
          <h1 className="project-header-name">{header.name}</h1>
          <span className="project-header-path mono" title={header.directory}>
            {header.directory}
          </span>
          <span className="project-header-activity mono">
            last activity{" "}
            {typeof lastActivity === "number" &&
            Number.isFinite(lastActivity) ? (
              <RelativeTime
                updatedRelativeMs={lastActivity}
                nowMs={nowMs}
              />
            ) : (
              "—"
            )}
          </span>
        </div>

        <div className="project-kpis" aria-label="Project KPIs">
          <div className="kpi-card">
            <div className="kpi-label" title="opencode processes alive in this directory">
              Live procs
            </div>
            <div className="kpi-value">{numberText(header.live_procs)}</div>
          </div>
          <div className="kpi-card">
            <div className="kpi-label">Running</div>
            <div className="kpi-value">{numberText(header.running)}</div>
          </div>
          <div className="kpi-card">
            <div className="kpi-label">Waiting input</div>
            <div className="kpi-value">{numberText(header.waiting_input)}</div>
          </div>
          <div className="kpi-card">
            <div className="kpi-label">Sessions</div>
            <div className="kpi-value">{numberText(header.sessions)}</div>
          </div>
          <div className="kpi-card">
            <div className="kpi-label">Tokens</div>
            <div className="kpi-value">{compactText(header.tokens_total)}</div>
          </div>
          <div className="kpi-card">
            <div className="kpi-label" title="Lifetime spend in this project">
              Cost total
            </div>
            <div className="kpi-value">{usdText(header.cost_total)}</div>
          </div>
        </div>
      </div>
    </section>
  );
}
