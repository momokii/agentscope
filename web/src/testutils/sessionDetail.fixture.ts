/**
 * SessionDetail fixture (W3-1): hand-shaped EXACTLY like
 * `opencode export` output (`info` + `messages[]` with `info`/`parts[]`).
 *
 * PROVENANCE: part-type shapes (`text`/`reasoning`/`tool`/`step-start`/
 * `step-finish`) were cribbed read-only from `opencode export <ses>
 * --sanitize` on this machine (Oct 2026) and then re-authored by hand with
 * toy content. NO raw export output is committed: directory is a /tmp fake,
 * counts are toys, costs are toys, no secrets, no real ids/titles.
 * Raw export also showed `file`/`patch`/`compaction` parts in the wild;
 * this fixture covers the four hot paths (W3-2 may extend, never real data).
 *
 * TOKEN SEMANTICS (toy, self-consistent): real export reports CUMULATIVE
 * snapshots per assistant message; this fixture instead carries PER-TURN
 * deltas so the export-parity test can assert
 * `rollUp(assistant tokens) === info.tokens` and `sum(costs) === info.cost`
 * per session. Main: 1500+2350+1600+3120=8570 / 0.0214. Child explore:
 * 910 / 0.0021. Child plan: 760 / 0.0011. The child turns are
 * timestamped BETWEEN main turns so the interleave test has real
 * crossings to assert (explore between a2 and u2, plan between u3/a4).
 *
 * TOOL I/O (W3-2): tool `state.input`/`state.output` carry toy command
 * strings and excerpts (never real paths/commands); every tool state with
 * a duration also carries `time` so Timeline/Tools math stays derivable.
 */

import type { ExportSession } from "../lib/transcript";
import type {
  SessionDataProvider,
  SessionDetailData,
} from "../lib/sessionProvider";

export type { SessionDataProvider, SessionDetailData } from "../lib/sessionProvider";

/**
 * Test-only provider: the view's default is `liveProvider` from
 * `lib/sessionProvider.ts`; tests pass this fixture explicitly. Unknown id
 * → null (the view renders the empty state, never fabricated detail).
 */

const T = 1_700_000_000_000;

const MAIN_ID = "ses_fixture_main01";
const SUB_ID = "ses_fixture_sub01";
const SUB2_ID = "ses_fixture_sub02";

export const FIXTURE_MAIN_ID = MAIN_ID;
export const FIXTURE_SUB_ID = SUB_ID;
export const FIXTURE_SUB2_ID = SUB2_ID;

const mainDetail: ExportSession = {
  info: {
    id: MAIN_ID,
    slug: "fd9f1xture-slug-0001",
    projectID: "prj_fixture0001",
    directory: "/tmp/fake-proj",
    title: "Add dashboard drill-down",
    agent: "build",
    model: { id: "toy-model", providerID: "toy-provider", variant: "default" },
    cost: 0.0214,
    tokens: {
      input: 6300,
      output: 1400,
      reasoning: 470,
      cache: { read: 1800, write: 300 },
      total: 8570,
    },
    time: { created: T, updated: T + 200_000 },
    parentID: null,
  },
  messages: [
    {
      info: {
        id: "msg_fix_u1",
        sessionID: MAIN_ID,
        role: "user",
        time: { created: T },
        agent: "build",
      },
      parts: [
        {
          type: "text",
          id: "prt_fix_u1t",
          messageID: "msg_fix_u1",
          sessionID: MAIN_ID,
          text: "Add a transcript view with tail follow and pinned history for long sessions.",
        },
      ],
    },
    {
      info: {
        id: "msg_fix_a1",
        sessionID: MAIN_ID,
        role: "assistant",
        time: { created: T + 10_000, completed: T + 45_000 },
        agent: "build",
        modelID: "toy-model",
        providerID: "toy-provider",
        tokens: {
          input: 1200,
          output: 200,
          reasoning: 100,
          cache: { read: 400, write: 0 },
          total: 1500,
        },
        cost: 0.0042,
      },
      parts: [
        { type: "step-start", id: "prt_fix_a1s", messageID: "msg_fix_a1", sessionID: MAIN_ID },
        {
          type: "reasoning",
          id: "prt_fix_a1r",
          messageID: "msg_fix_a1",
          sessionID: MAIN_ID,
          text: "Plan the windowing approach first, then wire the tail-follow state.",
          time: { start: T + 10_000, end: T + 12_000 },
        },
        {
          type: "tool",
          id: "prt_fix_a1read",
          messageID: "msg_fix_a1",
          sessionID: MAIN_ID,
          tool: "read",
          callID: "call_fix_0001",
          state: {
            status: "completed",
            title: "Read routing module",
            time: { start: T + 13_000, end: T + 14_000 },
          },
        },
        {
          type: "tool",
          id: "prt_fix_a1bash",
          messageID: "msg_fix_a1",
          sessionID: MAIN_ID,
          tool: "bash",
          callID: "call_fix_0002",
          state: {
            status: "completed",
            title: "List view directory",
            time: { start: T + 15_000, end: T + 18_000 },
          },
        },
        {
          type: "step-finish",
          id: "prt_fix_a1f",
          messageID: "msg_fix_a1",
          sessionID: MAIN_ID,
          reason: "tool-calls",
          tokens: {
            input: 1200,
            output: 200,
            reasoning: 100,
            cache: { read: 400, write: 0 },
            total: 1500,
          },
          cost: 0.0042,
        },
      ],
    },
    {
      info: {
        id: "msg_fix_a2",
        sessionID: MAIN_ID,
        role: "assistant",
        time: { created: T + 60_000, completed: T + 82_000 },
        agent: "build",
        modelID: "toy-model",
        providerID: "toy-provider",
        tokens: {
          input: 1800,
          output: 400,
          reasoning: 150,
          cache: { read: 600, write: 200 },
          total: 2350,
        },
        cost: 0.0061,
      },
      parts: [
        { type: "step-start", id: "prt_fix_a2s", messageID: "msg_fix_a2", sessionID: MAIN_ID },
        {
          type: "reasoning",
          id: "prt_fix_a2r",
          messageID: "msg_fix_a2",
          sessionID: MAIN_ID,
          text: "The first edit used a stale anchor string from an older revision.",
          time: { start: T + 60_000, end: T + 63_000 },
        },
        {
          type: "tool",
          id: "prt_fix_a2edit",
          messageID: "msg_fix_a2",
          sessionID: MAIN_ID,
          tool: "edit",
          callID: "call_fix_0003",
          state: {
            status: "error",
            output: "oldString not found in content",
            title: "Apply transcript styles",
            time: { start: T + 64_000, end: T + 65_000 },
          },
        },
        {
          type: "text",
          id: "prt_fix_a2t",
          messageID: "msg_fix_a2",
          sessionID: MAIN_ID,
          text: "The edit failed on a stale anchor; retrying with more context.",
        },
        {
          type: "step-finish",
          id: "prt_fix_a2f",
          messageID: "msg_fix_a2",
          sessionID: MAIN_ID,
          reason: "stop",
          tokens: {
            input: 1800,
            output: 400,
            reasoning: 150,
            cache: { read: 600, write: 200 },
            total: 2350,
          },
          cost: 0.0061,
        },
      ],
    },
    {
      info: {
        id: "msg_fix_u2",
        sessionID: MAIN_ID,
        role: "user",
        time: { created: T + 120_000 },
        agent: "build",
      },
      parts: [
        {
          type: "text",
          id: "prt_fix_u2t",
          messageID: "msg_fix_u2",
          sessionID: MAIN_ID,
          text: "Also add per-turn cards with token math that link back to the transcript.",
        },
      ],
    },
    {
      info: {
        id: "msg_fix_a3",
        sessionID: MAIN_ID,
        role: "assistant",
        time: { created: T + 130_000, completed: T + 148_000 },
        agent: "build",
        modelID: "toy-model",
        providerID: "toy-provider",
        tokens: {
          input: 1200,
          output: 300,
          reasoning: 100,
          cache: { read: 500, write: 0 },
          total: 1600,
        },
        cost: 0.0033,
      },
      parts: [
        { type: "step-start", id: "prt_fix_a3s", messageID: "msg_fix_a3", sessionID: MAIN_ID },
        {
          type: "reasoning",
          id: "prt_fix_a3r",
          messageID: "msg_fix_a3",
          sessionID: MAIN_ID,
          text: "Cards share anchor ids with transcript rows for jump-to-turn.",
          time: { start: T + 130_000, end: T + 131_000 },
        },
        {
          type: "text",
          id: "prt_fix_a3t",
          messageID: "msg_fix_a3",
          sessionID: MAIN_ID,
          text: "Done — turn cards show input, tools, tokens, duration, and cost.",
        },
        {
          type: "step-finish",
          id: "prt_fix_a3f",
          messageID: "msg_fix_a3",
          sessionID: MAIN_ID,
          reason: "stop",
          tokens: {
            input: 1200,
            output: 300,
            reasoning: 100,
            cache: { read: 500, write: 0 },
            total: 1600,
          },
          cost: 0.0033,
        },
      ],
    },
    {
      info: {
        id: "msg_fix_u3",
        sessionID: MAIN_ID,
        role: "user",
        time: { created: T + 160_000 },
        agent: "build",
      },
      parts: [
        {
          type: "text",
          id: "prt_fix_u3t",
          messageID: "msg_fix_u3",
          sessionID: MAIN_ID,
          text: "The suite is red — rerun tests, fix the failures, and show me the slowest tools.",
        },
      ],
    },
    {
      info: {
        id: "msg_fix_a4",
        sessionID: MAIN_ID,
        role: "assistant",
        time: { created: T + 170_000, completed: T + 200_000 },
        agent: "build",
        modelID: "toy-model",
        providerID: "toy-provider",
        tokens: {
          input: 2100,
          output: 500,
          reasoning: 120,
          cache: { read: 300, write: 100 },
          total: 3120,
        },
        cost: 0.0078,
      },
      parts: [
        { type: "step-start", id: "prt_fix_a4s", messageID: "msg_fix_a4", sessionID: MAIN_ID },
        {
          type: "tool",
          id: "prt_fix_a4bash1",
          messageID: "msg_fix_a4",
          sessionID: MAIN_ID,
          tool: "bash",
          callID: "call_fix_0005",
          state: {
            status: "error",
            title: "Run full test suite",
            input: { command: "npm test -- --run" },
            output: "FAIL src/views/SessionDetail.test.tsx > 2 failed | Duration 41.2s",
            time: { start: T + 171_000, end: T + 183_000 },
          },
        },
        {
          type: "tool",
          id: "prt_fix_a4read",
          messageID: "msg_fix_a4",
          sessionID: MAIN_ID,
          tool: "read",
          callID: "call_fix_0006",
          state: {
            status: "completed",
            title: "Read failing spec",
            input: { path: "/tmp/fake-proj/web/src/views/SessionDetail.test.tsx", offset: 90 },
            output: "expect(cards).toHaveLength(4)",
            time: { start: T + 184_000, end: T + 184_500 },
          },
        },
        {
          type: "tool",
          id: "prt_fix_a4bash2",
          messageID: "msg_fix_a4",
          sessionID: MAIN_ID,
          tool: "bash",
          callID: "call_fix_0007",
          state: {
            status: "completed",
            title: "Rerun focused spec",
            input: { command: "npm test -- --run src/views/SessionDetail.test.tsx" },
            output: "Test Files 1 passed | Tests 9 passed | Duration 8.1s",
            time: { start: T + 185_000, end: T + 193_000 },
          },
        },
        {
          type: "tool",
          id: "prt_fix_a4edit",
          messageID: "msg_fix_a4",
          sessionID: MAIN_ID,
          tool: "edit",
          callID: "call_fix_0008",
          state: {
            status: "completed",
            title: "Fix stale turn count",
            input: { path: "/tmp/fake-proj/web/src/views/SessionDetail.test.tsx", oldString: "toHaveLength(4)" },
            output: "applied 1 edit",
            time: { start: T + 193_100, end: T + 197_100 },
          },
        },
        {
          type: "tool",
          id: "prt_fix_a4grep",
          messageID: "msg_fix_a4",
          sessionID: MAIN_ID,
          tool: "grep",
          callID: "call_fix_0009",
          state: {
            status: "error",
            title: "Search stale anchors",
            input: { pattern: "toHaveLength\\(4\\)", include: "*.tsx" },
            output: "exit 2: invalid regex flag",
            time: { start: T + 197_200, end: T + 199_200 },
          },
        },
        {
          type: "text",
          id: "prt_fix_a4t",
          messageID: "msg_fix_a4",
          sessionID: MAIN_ID,
          text: "Suite is green again; the full run was the slowest tool at 12s.",
        },
        {
          type: "step-finish",
          id: "prt_fix_a4f",
          messageID: "msg_fix_a4",
          sessionID: MAIN_ID,
          reason: "stop",
          tokens: {
            input: 2100,
            output: 500,
            reasoning: 120,
            cache: { read: 300, write: 100 },
            total: 3120,
          },
          cost: 0.0078,
        },
      ],
    },
  ],
};

const subDetail: ExportSession = {
  info: {
    id: SUB_ID,
    slug: "fd9f1xture-slug-0002",
    projectID: "prj_fixture0001",
    directory: "/tmp/fake-proj",
    title: "Research windowing strategies",
    agent: "explore",
    model: { id: "toy-model", providerID: "toy-provider", variant: "default" },
    cost: 0.0021,
    tokens: {
      input: 700,
      output: 150,
      reasoning: 60,
      cache: { read: 200, write: 0 },
      total: 910,
    },
    time: { created: T + 90_000, updated: T + 107_000 },
    parentID: MAIN_ID,
  },
  messages: [
    {
      info: {
        id: "msg_fix_cu1",
        sessionID: SUB_ID,
        role: "user",
        time: { created: T + 90_000 },
        agent: "explore",
      },
      parts: [
        {
          type: "text",
          id: "prt_fix_cu1t",
          messageID: "msg_fix_cu1",
          sessionID: SUB_ID,
          text: "Research windowing strategies for rendering long message lists.",
        },
      ],
    },
    {
      info: {
        id: "msg_fix_ca1",
        sessionID: SUB_ID,
        role: "assistant",
        time: { created: T + 95_000, completed: T + 107_000 },
        agent: "explore",
        modelID: "toy-model",
        providerID: "toy-provider",
        tokens: {
          input: 700,
          output: 150,
          reasoning: 60,
          cache: { read: 200, write: 0 },
          total: 910,
        },
        cost: 0.0021,
      },
      parts: [
        { type: "step-start", id: "prt_fix_ca1s", messageID: "msg_fix_ca1", sessionID: SUB_ID },
        {
          type: "reasoning",
          id: "prt_fix_ca1r",
          messageID: "msg_fix_ca1",
          sessionID: SUB_ID,
          text: "Fixed estimate plus overscan keeps scroll height honest.",
          time: { start: T + 95_000, end: T + 96_000 },
        },
        {
          type: "tool",
          id: "prt_fix_ca1grep",
          messageID: "msg_fix_ca1",
          sessionID: SUB_ID,
          tool: "grep",
          callID: "call_fix_0004",
          state: {
            status: "completed",
            title: "Search windowing helpers",
            time: { start: T + 97_000, end: T + 99_000 },
          },
        },
        {
          type: "step-finish",
          id: "prt_fix_ca1f",
          messageID: "msg_fix_ca1",
          sessionID: SUB_ID,
          reason: "tool-calls",
          tokens: {
            input: 700,
            output: 150,
            reasoning: 60,
            cache: { read: 200, write: 0 },
            total: 910,
          },
          cost: 0.0021,
        },
      ],
    },
  ],
};

const subDetail2: ExportSession = {
  info: {
    id: SUB2_ID,
    slug: "fd9f1xture-slug-0003",
    projectID: "prj_fixture0001",
    directory: "/tmp/fake-proj",
    title: "Plan the cost tab math",
    agent: "plan",
    model: { id: "toy-model-mini", providerID: "toy-provider", variant: "default" },
    cost: 0.0011,
    tokens: {
      input: 500,
      output: 120,
      reasoning: 40,
      cache: { read: 100, write: 0 },
      total: 760,
    },
    time: { created: T + 165_000, updated: T + 180_000 },
    parentID: MAIN_ID,
  },
  messages: [
    {
      info: {
        id: "msg_fix_du1",
        sessionID: SUB2_ID,
        role: "user",
        time: { created: T + 165_000 },
        agent: "plan",
      },
      parts: [
        {
          type: "text",
          id: "prt_fix_du1t",
          messageID: "msg_fix_du1",
          sessionID: SUB2_ID,
          text: "Plan the cost tab math so every figure traces to the info block.",
        },
      ],
    },
    {
      info: {
        id: "msg_fix_da1",
        sessionID: SUB2_ID,
        role: "assistant",
        time: { created: T + 168_000, completed: T + 180_000 },
        agent: "plan",
        modelID: "toy-model-mini",
        providerID: "toy-provider",
        tokens: {
          input: 500,
          output: 120,
          reasoning: 40,
          cache: { read: 100, write: 0 },
          total: 760,
        },
        cost: 0.0011,
      },
      parts: [
        { type: "step-start", id: "prt_fix_da1s", messageID: "msg_fix_da1", sessionID: SUB2_ID },
        {
          type: "tool",
          id: "prt_fix_da1read",
          messageID: "msg_fix_da1",
          sessionID: SUB2_ID,
          tool: "read",
          callID: "call_fix_0010",
          state: {
            status: "completed",
            title: "Read transcript math",
            input: { path: "/tmp/fake-proj/web/src/lib/transcript.ts" },
            output: "rollUpTokens + messageCost",
            time: { start: T + 169_000, end: T + 170_000 },
          },
        },
        {
          type: "tool",
          id: "prt_fix_da1bash",
          messageID: "msg_fix_da1",
          sessionID: SUB2_ID,
          tool: "bash",
          callID: "call_fix_0011",
          state: {
            status: "completed",
            title: "Check mini-model pricing",
            input: { command: "toy-bill --model toy-model-mini" },
            output: "$0.0011 for 760 tokens",
            time: { start: T + 171_000, end: T + 173_000 },
          },
        },
        {
          type: "step-finish",
          id: "prt_fix_da1f",
          messageID: "msg_fix_da1",
          sessionID: SUB2_ID,
          reason: "stop",
          tokens: {
            input: 500,
            output: 120,
            reasoning: 40,
            cache: { read: 100, write: 0 },
            total: 760,
          },
          cost: 0.0011,
        },
      ],
    },
  ],
};

const DETAILS: Record<string, SessionDetailData> = {
  [MAIN_ID]: { detail: mainDetail, children: [subDetail, subDetail2] },
  [SUB_ID]: { detail: subDetail, children: [] },
  [SUB2_ID]: { detail: subDetail2, children: [] },
};

export function fixtureDetail(id: string): SessionDetailData | null {
  return DETAILS[id] ?? null;
}

export const fixtureProvider: SessionDataProvider = {
  getSessionDetail: fixtureDetail,
};

export function listFixtureIds(): string[] {
  return Object.keys(DETAILS);
}
