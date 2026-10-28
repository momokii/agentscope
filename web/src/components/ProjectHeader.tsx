import { ArrowLeft } from "lucide-react";

import type { ProjectHeaderData } from "../lib/projects";
import { formatCompact, formatUSD } from "../lib/format";
import RelativeTime from "./RelativeTime";

export interface ProjectHeaderProps {
  header: ProjectHeaderData;
  nowMs?: number;
  /** Windowed labels when the drill-in follows a date preset
   * ("Tokens 24h" / "Cost 24h"); lifetime labels otherwise. */
  tokensLabel?: string;
  costLabel?: string;
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

const PANES_TITLE =
  "OpenCode panes in this directory (presence signal; may briefly include a pane whose process just exited)";

function livePanes(
  header: ProjectHeaderData & { live_panes?: unknown },
): number | undefined {
  const value = header.live_panes;
  return typeof value === "number" && Number.isFinite(value) ? value : undefined;
}

export default function ProjectHeader({
  header,
  nowMs,
  tokensLabel = "Tokens",
  costLabel = "Cost total",
}: ProjectHeaderProps) {
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
            <div
              className="kpi-label"
              title="opencode processes attributed by /proc; 0 when the container cannot read /proc/<pid>/cwd"
            >
              Live procs
            </div>
            <div className="kpi-value">{numberText(header.live_procs)}</div>
          </div>
          <div className="kpi-card">
            <div className="kpi-label" title={PANES_TITLE}>
              Live panes
            </div>
            <div className="kpi-value">{numberText(livePanes(header))}</div>
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
            <div
              className="kpi-label"
              title={
                tokensLabel === "Tokens"
                  ? "Lifetime tokens in this project (incl. subagent roll-up)"
                  : `Message tokens in this project in the trailing window (both harnesses)`
              }
            >
              {tokensLabel}
            </div>
            <div className="kpi-value">{compactText(header.tokens_total)}</div>
          </div>
          <div className="kpi-card">
            <div
              className="kpi-label"
              title={
                costLabel === "Cost total"
                  ? "Lifetime spend in this project"
                  : `Message spend in this project in the trailing window (both harnesses)`
              }
            >
              {costLabel}
            </div>
            <div className="kpi-value">{usdText(header.cost_total)}</div>
          </div>
        </div>
      </div>
    </section>
  );
}
