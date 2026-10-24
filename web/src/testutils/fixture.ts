/**
 * Fixture Snapshot mirroring the frozen Go wire shape byte-for-byte:
 * snake_case keys per internal/model SessionRow/KPIs/Snapshot json tags
 * (seq, sessions, kpis{active,waiting_input,tokens_per_hour,cost_today,
 * errors,sse_connected}, last_poll_ms, stale; row fields per SessionRow).
 * One row per status so badge/sort/group tests cover the full matrix.
 */

import type { SessionRow, Snapshot } from "../lib/sse";

let seq = 0;

export function makeRow(overrides: Partial<SessionRow> = {}): SessionRow {
  seq += 1;
  return {
    id: `ses_fixture${seq}`,
    title: `fixture session ${seq}`,
    project: "opencode-dashboard",
    directory: "/home/kelanach/opencode-dashboard",
    model: "anthropic/claude-opus",
    status: "idle",
    tokens: { in: 1000, out: 500, total: 1500 },
    cost: 0.0123,
    updated_relative_ms: 30_000,
    spark: [1, 2, 3, 4, 5],
    subagent_count: 0,
    pane: { cwd: "/home/kelanach/opencode-dashboard", exited: false },
    orphan: false,
    ...overrides,
  };
}

/** Five rows spanning every status; ages span Today/Yesterday/Older. */
export function buildFixtureSnapshot(
  overrides: Partial<Snapshot> = {},
): Snapshot {
  return {
    seq: 42,
    sessions: [
      makeRow({
        id: "ses_running1",
        title: "Implement fleet view",
        status: "running",
        updated_relative_ms: 5_000,
        spark: [4, 8, 6, 12, 9, 15],
        subagent_count: 2,
        tokens: { in: 9000, out: 3000, total: 12000 },
        cost: 0.184,
        model: "anthropic/claude-sonnet",
      }),
      makeRow({
        id: "ses_waiting1",
        title: "Review plan defaults",
        project: "global",
        status: "waiting-input",
        updated_relative_ms: 90_000,
        spark: [2, 2, 2, 2],
        tokens: { in: 4000, out: 1200, total: 5200 },
        cost: 0.071,
      }),
      makeRow({
        id: "ses_idle1",
        title: "Refactor collector poll loop",
        status: "idle",
        updated_relative_ms: 3_600_000,
        spark: [],
        tokens: { in: 2000, out: 800, total: 2800 },
        cost: 0.033,
      }),
      makeRow({
        id: "ses_error1",
        title: "Fix auth mount leak",
        status: "error",
        updated_relative_ms: 90_000_000,
        spark: [9, 1, 1, 1],
        subagent_count: 1,
        tokens: { in: 7000, out: 700, total: 7700 },
        cost: 0.102,
        model: "openai/gpt-5",
      }),
      makeRow({
        id: "ses_archived1",
        title: "Old spike",
        status: "archived",
        updated_relative_ms: 200_000_000,
        spark: [1, 1, 1],
        pane: { cwd: "", exited: true },
        orphan: true,
        tokens: { in: 500, out: 100, total: 600 },
        cost: 0.004,
      }),
    ],
    kpis: {
      active: 1,
      waiting_input: 1,
      tokens_per_hour: 12500,
      cost_today: 12.4,
      errors: 1,
      sse_connected: true,
    },
    last_poll_ms: 210,
    stale: false,
    ...overrides,
  };
}
