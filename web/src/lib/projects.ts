import type { ProjectSummary, SessionRow, Snapshot } from "./sse";

export function projectNameFromDirectory(directory: string): string {
  const trimmed = directory.replace(/\/+$/, "");
  if (trimmed === "") return directory;
  const slash = trimmed.lastIndexOf("/");
  return slash >= 0 ? trimmed.slice(slash + 1) : trimmed;
}

export function projectDisplayName(project: ProjectSummary): string {
  return project.name ?? projectNameFromDirectory(project.directory);
}

export function sortProjects(
  projects: readonly ProjectSummary[],
): ProjectSummary[] {
  return [...projects].sort((a, b) => {
    const procs = (b.live_procs ?? 0) - (a.live_procs ?? 0);
    if (procs !== 0) return procs;
    const running = (b.running ?? 0) - (a.running ?? 0);
    if (running !== 0) return running;
    return (a.last_activity_ms ?? Number.POSITIVE_INFINITY) -
      (b.last_activity_ms ?? Number.POSITIVE_INFINITY);
  });
}

export function filterProjects(
  projects: readonly ProjectSummary[],
  query: string,
): ProjectSummary[] {
  const needle = query.trim().toLowerCase();
  if (needle === "") return [...projects];
  return projects.filter(
    (project) =>
      projectDisplayName(project).toLowerCase().includes(needle) ||
      project.directory.toLowerCase().includes(needle),
  );
}

export function projectIsLive(
  project: Pick<ProjectSummary, "live_procs" | "running">,
): boolean {
  return (project.live_procs ?? 0) > 0 || (project.running ?? 0) > 0;
}

export function hasProjects(
  snapshot: Snapshot | null,
): snapshot is Snapshot & { projects: ProjectSummary[] } {
  return snapshot !== null && Array.isArray(snapshot.projects);
}

export interface ProjectHeaderData {
  directory: string;
  name: string;
  sessions?: number;
  running?: number;
  waiting_input?: number;
  tokens_total?: number;
  cost_total?: number;
  live_procs?: number;
  last_activity_ms?: number;
}

function summarizeRows(
  directory: string,
  rows: readonly SessionRow[],
): ProjectHeaderData {
  let running = 0;
  let waiting_input = 0;
  let tokens_total = 0;
  let cost_total = 0;
  let last_activity_ms = Number.POSITIVE_INFINITY;
  for (const row of rows) {
    if (row.status === "running") running += 1;
    if (row.status === "waiting-input") waiting_input += 1;
    tokens_total += row.tokens.total;
    cost_total += row.cost;
    last_activity_ms = Math.min(last_activity_ms, row.updated_relative_ms);
  }
  return {
    directory,
    name: projectNameFromDirectory(directory),
    sessions: rows.length,
    running,
    waiting_input,
    tokens_total,
    cost_total,
    last_activity_ms: Number.isFinite(last_activity_ms)
      ? last_activity_ms
      : undefined,
  };
}

/**
 * Project KPI header for drill-in. Prefers the frozen `projects` summary;
 * when an older snapshot omits it, derives counts/cost from the session rows
 * and leaves `live_procs` undefined so the UI can render "—" honestly.
 */
export function resolveProjectHeader(
  snapshot: Snapshot | null,
  directory: string,
): ProjectHeaderData {
  const project = snapshot?.projects?.find(
    (item) => item.directory === directory,
  );
  if (project) return { ...project, name: projectDisplayName(project) };
  const rows =
    snapshot?.sessions.filter((row) => row.directory === directory) ?? [];
  return summarizeRows(directory, rows);
}
