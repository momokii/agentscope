import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import {
  buildResumeUrl,
  computeReconnectDelayMs,
  createSSEClient,
  deriveSnapshotUrl,
  normalizeSnapshot,
  type Snapshot,
  type StatusEvent,
} from "./sse";

type Listener = (event: Event) => void;

/** Minimal in-memory EventSource stand-in: the client never sees the network. */
class FakeEventSource {
  static instances: FakeEventSource[] = [];

  readonly url: string;
  closed = false;
  private listeners: Array<{ type: string; original: unknown; fn: Listener }> = [];

  constructor(url: string) {
    this.url = url;
    FakeEventSource.instances.push(this);
  }

  addEventListener(
    type: string,
    listener: EventListenerOrEventListenerObject | null,
  ): void {
    if (!listener) return;
    const fn: Listener =
      typeof listener === "function"
        ? (event) => (listener as EventListener)(event)
        : (event) => (listener as EventListenerObject).handleEvent(event);
    this.listeners.push({ type, original: listener, fn });
  }

  removeEventListener(
    type: string,
    listener: EventListenerOrEventListenerObject | null,
  ): void {
    this.listeners = this.listeners.filter(
      (entry) => !(entry.type === type && entry.original === listener),
    );
  }

  close(): void {
    this.closed = true;
  }

  private dispatch(type: string, event: Event): void {
    for (const entry of [...this.listeners]) {
      if (entry.type === type) entry.fn(event);
    }
  }

  open(): void {
    this.dispatch("open", new Event("open"));
  }

  snapshot(snap: Snapshot): void {
    this.dispatch(
      "snapshot",
      new MessageEvent("snapshot", { data: JSON.stringify(snap) }),
    );
  }

  rawMessage(data: string): void {
    this.dispatch("message", new MessageEvent("message", { data }));
  }

  fail(): void {
    this.dispatch("error", new Event("error"));
  }
}

function makeSnapshot(seq: number, ids: string[] = ["ses_a"]): Snapshot {
  return {
    seq,
    sessions: ids.map((id) => ({
      id,
      title: `title ${id}`,
      project: "global",
      directory: "/tmp",
      model: "fixture/provider",
      status: "idle" as const,
      tokens: { in: 100, out: 50, total: 150 },
      cost: 0.001,
      updated_relative_ms: 1000,
      spark: [0.5, 1.0],
      subagent_count: 0,
      pane: { cwd: "/tmp", exited: false },
      orphan: false,
    })),
    kpis: {
      active: 1,
      waiting_input: 0,
      tokens_per_hour: 1000,
      cost_today: 0.5,
      errors: 0,
      sse_connected: true,
    },
    last_poll_ms: 120,
    stale: false,
  };
}

function setupHarness(resyncSeqs: number[] = [10]) {
  const snapshots: Snapshot[] = [];
  const statuses: StatusEvent[] = [];
  let resyncCalls = 0;
  const fetchSnapshot = vi.fn(async (url: string): Promise<Snapshot> => {
    void url;
    const seq = resyncSeqs[Math.min(resyncCalls, resyncSeqs.length - 1)];
    resyncCalls += 1;
    return makeSnapshot(seq, [`ses_resync_${seq}`]);
  });
  const client = createSSEClient({
    url: "/api/events",
    onSnapshot: (snap) => {
      snapshots.push(snap);
    },
    onStatus: (status) => {
      statuses.push({ ...status });
    },
    createEventSource: (url) => new FakeEventSource(url) as unknown as EventSource,
    fetchSnapshot,
    random: () => 1, // deterministic jitter: full backoff step
  });
  const streams = () => FakeEventSource.instances;
  return { client, snapshots, statuses, fetchSnapshot, streams };
}

beforeEach(() => {
  vi.useFakeTimers();
});

afterEach(() => {
  FakeEventSource.instances = [];
  vi.useRealTimers();
  vi.restoreAllMocks();
});

describe("contract helpers", () => {
  it("derives /api/fleet from /api/events and appends the resume cursor", () => {
    expect(deriveSnapshotUrl("/api/events")).toBe("/api/fleet");
    expect(deriveSnapshotUrl("http://x:8080/api/events?foo=1")).toBe(
      "http://x:8080/api/fleet",
    );
    expect(buildResumeUrl("/api/events", 0)).toBe("/api/events");
    expect(buildResumeUrl("/api/events", 42)).toBe("/api/events?lastEventId=42");
    expect(buildResumeUrl("/api/events?a=b", 7)).toBe("/api/events?a=b&lastEventId=7");
  });

  it("backs off immediately, then exponentially with a 30s cap", () => {
    expect(computeReconnectDelayMs(1, 1000, 30000, () => 1)).toBe(0);
    expect(computeReconnectDelayMs(2, 1000, 30000, () => 1)).toBe(1000);
    expect(computeReconnectDelayMs(3, 1000, 30000, () => 1)).toBe(2000);
    expect(computeReconnectDelayMs(4, 1000, 30000, () => 1)).toBe(4000);
    expect(computeReconnectDelayMs(20, 1000, 30000, () => 1)).toBe(30000);
  });

  it("rejects unknown statuses and fleet-shaped payloads normalize", () => {
    expect(() =>
      normalizeSnapshot({ seq: 1, sessions: [{ id: "x", status: "bogus" }], kpis: {} }),
    ).toThrow(/unknown status/);
    const fleet = { seq: 9, sessions: [], kpis: makeSnapshot(9).kpis };
    expect(normalizeSnapshot(fleet)).toMatchObject({ seq: 9, last_poll_ms: 0 });
  });

  it("passes through optional projects and KPI fields, and omits absent projects", () => {
    const base = makeSnapshot(9);
    const withProjects = normalizeSnapshot({
      ...base,
      kpis: { ...base.kpis, cost_total: 12.5, metrics_age_ms: 4000 },
      projects: [
        {
          directory: "/srv/alpha",
          name: "alpha",
          sessions: 3,
          live_procs: 2,
        },
      ],
    });
    expect(withProjects.kpis.cost_total).toBe(12.5);
    expect(withProjects.kpis.metrics_age_ms).toBe(4000);
    expect(withProjects.projects?.[0]?.directory).toBe("/srv/alpha");
    expect(normalizeSnapshot(base).projects).toBeUndefined();
  });
});

describe("sse client", () => {
  it("kill-server-resume: banner, Last-Event-ID resume, full-snapshot re-sync, no duplicates", async () => {
    const { client, snapshots, statuses, fetchSnapshot, streams } = setupHarness([10]);

    expect(statuses[0]?.state).toBe("connecting");
    streams()[0]?.open();
    expect(statuses.at(-1)?.state).toBe("connected");

    // Healthy stream: one emission per frame once the 1Hz window passes.
    streams()[0]?.snapshot(makeSnapshot(1, ["ses_a"]));
    await vi.advanceTimersByTimeAsync(1100);
    streams()[0]?.snapshot(makeSnapshot(2, ["ses_a"]));
    await vi.advanceTimersByTimeAsync(1100);
    streams()[0]?.snapshot(makeSnapshot(3, ["ses_a"]));
    await vi.advanceTimersByTimeAsync(1100);
    expect(snapshots.map((s) => s.seq)).toEqual([1, 2, 3]);

    // Kill the server mid-stream: visible reconnecting state with attempt 1.
    streams()[0]?.fail();
    expect(client.getPhase()).toBe("reconnecting");
    expect(client.getAttempt()).toBe(1);
    expect(statuses.at(-1)).toMatchObject({ state: "reconnecting", attempt: 1 });

    // Immediate first retry: re-sync fetch + resumed stream carry the cursor.
    // The cursor is post-resync (10): the full snapshot already closed the
    // gap, so the new stream resumes from the authoritative seq.
    await vi.advanceTimersByTimeAsync(0);
    expect(fetchSnapshot).toHaveBeenCalledTimes(1);
    expect(fetchSnapshot).toHaveBeenCalledWith("/api/fleet");
    expect(streams()).toHaveLength(2);
    expect(streams()[1]?.url).toContain("lastEventId=10");

    // Re-sync REPLACES state: exactly one emission, full snapshot, new rows.
    expect(snapshots.map((s) => s.seq)).toEqual([1, 2, 3, 10]);
    expect(snapshots.at(-1)?.sessions.map((r) => r.id)).toEqual(["ses_resync_10"]);

    // The resumed stream replaying seq 10 must not double-apply it.
    streams()[1]?.open();
    streams()[1]?.snapshot(makeSnapshot(10, ["ses_resync_10"]));
    await vi.advanceTimersByTimeAsync(1100);
    expect(snapshots.map((s) => s.seq)).toEqual([1, 2, 3, 10]);
    expect(client.getLastSeq()).toBe(10);

    // A genuinely newer frame still applies (replace, not merge).
    streams()[1]?.snapshot(makeSnapshot(11, ["ses_new"]));
    await vi.advanceTimersByTimeAsync(1100);
    expect(snapshots.map((s) => s.seq)).toEqual([1, 2, 3, 10, 11]);
    expect(snapshots.at(-1)?.sessions.map((r) => r.id)).toEqual(["ses_new"]);

    client.close();
  });

  it("backs off 0ms, 1s, 2s, 4s across consecutive drops", async () => {
    const { streams } = setupHarness([1]);
    streams()[0]?.open();

    streams()[0]?.fail(); // attempt 1 → immediate
    await vi.advanceTimersByTimeAsync(0);
    expect(streams()).toHaveLength(2);

    streams()[1]?.fail(); // attempt 2 → 1000ms
    await vi.advanceTimersByTimeAsync(999);
    expect(streams()).toHaveLength(2);
    await vi.advanceTimersByTimeAsync(1);
    expect(streams()).toHaveLength(3);

    streams()[2]?.fail(); // attempt 3 → 2000ms
    await vi.advanceTimersByTimeAsync(1999);
    expect(streams()).toHaveLength(3);
    await vi.advanceTimersByTimeAsync(1);
    expect(streams()).toHaveLength(4);

    streams()[3]?.fail(); // attempt 4 → 4000ms
    await vi.advanceTimersByTimeAsync(3999);
    expect(streams()).toHaveLength(4);
    await vi.advanceTimersByTimeAsync(1);
    expect(streams()).toHaveLength(5);
  });

  it("caps downstream emission at 1Hz: burst of 50 frames → 2 emissions, latest wins", async () => {
    const { snapshots, streams } = setupHarness();
    streams()[0]?.open();

    for (let seq = 1; seq <= 50; seq += 1) {
      streams()[0]?.snapshot(makeSnapshot(seq, [`ses_${seq}`]));
    }
    // No time passed: exactly the first frame emitted, the other 49 collapsed.
    expect(snapshots.map((s) => s.seq)).toEqual([1]);

    await vi.advanceTimersByTimeAsync(1000);
    expect(snapshots.map((s) => s.seq)).toEqual([1, 50]);

    await vi.advanceTimersByTimeAsync(5000);
    expect(snapshots).toHaveLength(2); // nothing pending, never floods
  });

  it("emits exactly one onSnapshot per applied frame with atomic replace (no scroll-yank)", async () => {
    const { snapshots, streams } = setupHarness();
    // Consumer-visible invariant: the view REPLACES its state on every call
    // (assignment, never a merge), so a tick cannot yank scroll or duplicate rows.
    let current: Snapshot | null = null;
    let calls = 0;
    streams()[0]?.open();

    for (const [seq, id] of [[1, "a"], [2, "b"], [3, "c"]] as const) {
      streams()[0]?.snapshot(makeSnapshot(seq, [id]));
      current = snapshots.at(-1) ?? null; // consumer replaces state here
      calls += 1;
      await vi.advanceTimersByTimeAsync(1100);
    }

    expect(calls).toBe(3);
    expect(snapshots).toHaveLength(3);
    expect(snapshots.map((s) => s.seq)).toEqual([1, 2, 3]);
    // Each emission is the complete new state — never an accumulation.
    expect(snapshots[0]?.sessions.map((r) => r.id)).toEqual(["a"]);
    expect(snapshots[1]?.sessions.map((r) => r.id)).toEqual(["b"]);
    expect(snapshots[2]?.sessions.map((r) => r.id)).toEqual(["c"]);
    expect(current).toBe(snapshots[2]);
  });

  it("drops malformed frames without killing the stream", async () => {
    const { snapshots, streams } = setupHarness();
    const warn = vi.spyOn(console, "warn").mockImplementation(() => undefined);
    streams()[0]?.open();

    streams()[0]?.rawMessage("not-json{{{");
    streams()[0]?.snapshot(makeSnapshot(1, ["ses_a"]));
    await vi.advanceTimersByTimeAsync(1100);

    expect(snapshots.map((s) => s.seq)).toEqual([1]);
    expect(warn).toHaveBeenCalled();
  });

  it("close() kills sockets and timers: nothing pending, nothing further emitted", async () => {
    const { client, snapshots, statuses, streams } = setupHarness();
    streams()[0]?.open();
    streams()[0]?.snapshot(makeSnapshot(1, ["ses_a"]));
    streams()[0]?.snapshot(makeSnapshot(2, ["ses_b"])); // leaves a pending emit timer
    streams()[0]?.fail(); // leaves a pending reconnect timer

    client.close();

    expect(streams()[0]?.closed).toBe(true);
    expect(vi.getTimerCount()).toBe(0);
    expect(statuses.at(-1)?.state).toBe("closed");
    expect(client.getPhase()).toBe("closed");

    // Dead stream chatter and time passing must not emit anything further.
    streams()[0]?.snapshot(makeSnapshot(99, ["ses_ghost"]));
    await vi.advanceTimersByTimeAsync(60_000);
    expect(snapshots.map((s) => s.seq)).toEqual([1]);
    expect(vi.getTimerCount()).toBe(0);
    expect(FakeEventSource.instances).toHaveLength(1); // never reconnected
  });
});
