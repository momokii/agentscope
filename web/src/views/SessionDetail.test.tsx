import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";

afterEach(() => {
  cleanup();
  window.location.hash = "#/";
});

import SessionDetail, { TranscriptTab, TurnsTab } from "./SessionDetail";
import { buildTurns, flattenTurns, WINDOW_MAX_ROWS } from "../lib/transcript";
import type { ExportMessage, ExportSession, FlatMessage } from "../lib/transcript";
import {
  FIXTURE_MAIN_ID,
  fixtureProvider,
} from "../testutils/sessionDetail.fixture";

function mainFlat(): FlatMessage[] {
  const data = fixtureProvider.getSessionDetail(FIXTURE_MAIN_ID);
  if (data === null) throw new Error("fixture missing");
  return flattenTurns(data.detail, data.children);
}

function mockGeometry(el: HTMLElement, scrollHeight: number, scrollTop: number): void {
  Object.defineProperty(el, "scrollHeight", { value: scrollHeight, configurable: true });
  Object.defineProperty(el, "scrollTop", { value: scrollTop, writable: true, configurable: true });
  Object.defineProperty(el, "clientHeight", { value: 600, configurable: true });
}

describe("SessionDetail transcript tab", () => {
  it("renders interleaved messages with agent-lane chips", () => {
    render(
      <SessionDetail id={FIXTURE_MAIN_ID} tab="transcript" row={null} paletteOpen={false} />,
    );
    expect(screen.getByText("Add dashboard drill-down")).toBeDefined();
    expect(screen.getAllByText("build").length).toBeGreaterThan(0);
    expect(screen.getAllByText("explore").length).toBeGreaterThan(0);
    expect(
      screen.getByText("The edit failed on a stale anchor; retrying with more context."),
    ).toBeDefined();
  });

  it("collapses reasoning by default with an expander", () => {
    render(
      <SessionDetail id={FIXTURE_MAIN_ID} tab="transcript" row={null} paletteOpen={false} />,
    );
    const hidden =
      "Plan the windowing approach first, then wire the tail-follow state.";
    expect(screen.queryByText(hidden)).toBeNull();
    fireEvent.click(screen.getAllByText("reasoning")[0]);
    expect(screen.getByText(hidden)).toBeDefined();
  });

  it("renders tool rows with status glyphs, incl. the error rows", () => {
    const { container } = render(
      <SessionDetail id={FIXTURE_MAIN_ID} tab="transcript" row={null} paletteOpen={false} />,
    );
    expect(container.querySelectorAll(".tool-row").length).toBeGreaterThan(0);
    expect(screen.getAllByText("read").length).toBeGreaterThanOrEqual(2);
    expect(screen.getAllByText("error").length).toBeGreaterThanOrEqual(2);
  });

  it("shows all six tabs enabled with no W3-2 stubs left", () => {
    render(
      <SessionDetail id={FIXTURE_MAIN_ID} tab="transcript" row={null} paletteOpen={false} />,
    );
    const tablist = screen.getByRole("tablist");
    expect(tablist.textContent).toContain("Transcript");
    for (const label of ["Turns", "Timeline", "Tools", "Cost", "Raw"]) {
      expect(tablist.textContent).toContain(label);
    }
    expect(screen.queryByText("lands in W3-2")).toBeNull();
    expect(
      screen.getByRole("tab", { name: /transcript/i }).getAttribute("aria-selected"),
    ).toBe("true");
  });

  it("renders the empty state for an unknown session", () => {
    render(
      <SessionDetail id="ses_nope" tab="transcript" row={null} paletteOpen={false} />,
    );
    expect(screen.getByText(/Unknown session/)).toBeDefined();
  });

  it("renders an empty state with no turns", () => {
    const { container } = render(<TurnsTab turns={[]} onSelect={() => undefined} />);
    expect(container.textContent).toContain("No assistant turns yet.");
  });
});

describe("SessionDetail turns tab", () => {
  it("renders one card per assistant turn with math, and jumps to transcript", () => {
    const { container, rerender } = render(
      <SessionDetail id={FIXTURE_MAIN_ID} tab="turns" row={null} paletteOpen={false} />,
    );
    const cards = screen.getAllByTestId(/turn-card-/, { exact: false });
    expect(cards).toHaveLength(6);
    const turnsEl = container.querySelector(".turns");
    expect(turnsEl?.textContent).toContain("Add a transcript view with tail follow");
    expect(turnsEl?.textContent).toContain("2 tools · read, bash");
    expect(turnsEl?.textContent).toContain("35.0s");

    fireEvent.click(cards[0]);
    expect(window.location.hash).toBe(`#/sessions/${FIXTURE_MAIN_ID}`);
    rerender(
      <SessionDetail id={FIXTURE_MAIN_ID} tab="transcript" row={null} paletteOpen={false} />,
    );
    const anchor = document.getElementById("turn-msg_fix_a1");
    expect(anchor).not.toBeNull();
    expect(anchor?.className).toContain("tmsg-hl");
    window.location.hash = "#/";
  });
});

describe("tail-follow, pin pill, Esc", () => {
  function renderPinned(flat: FlatMessage[]) {
    const view = render(
      <TranscriptTab flat={flat} jumpTarget={null} onJumped={() => undefined} paletteOpen={false} />,
    );
    const scroller = view.container.querySelector(".transcript") as HTMLElement;
    mockGeometry(scroller, 10_000, 0);
    fireEvent.scroll(scroller);
    return { view, scroller };
  }

  it("pins on scroll-up and shows a counted pill for new rows; click resumes", () => {
    const flat = mainFlat();
    const { view, scroller } = renderPinned(flat);
    expect(screen.queryByText(/new/)).toBeNull();

    const extra: ExportMessage = {
      info: {
        id: "msg_extra",
        sessionID: "s",
        role: "assistant",
        time: { created: 9_999_999_999_999 },
      },
      parts: [
        {
          type: "text",
          id: "p_extra",
          messageID: "msg_extra",
          sessionID: "s",
          text: "late arrival",
        },
      ],
    };
    const grown: FlatMessage[] = [
      ...flat,
      { message: extra, sessionId: "s", agent: "build", lane: "main", timeCreated: 9_999_999_999_999 },
    ];
    view.rerender(
      <TranscriptTab flat={grown} jumpTarget={null} onJumped={() => undefined} paletteOpen={false} />,
    );
    expect(screen.getByText(/1 new/)).toBeDefined();
    fireEvent.click(screen.getByText(/1 new/));
    expect(screen.queryByText(/new/)).toBeNull();
    expect(scroller.scrollTop).toBe(scroller.scrollHeight);
  });

  it("Esc resumes tail-follow but is ignored while typing", () => {
    const flat = mainFlat();
    const { view } = renderPinned(flat);
    const grown: FlatMessage[] = [
      ...flat,
      {
        message: {
          info: {
            id: "msg_extra2",
            sessionID: "s",
            role: "assistant",
            time: { created: 9_999_999_999_998 },
          },
          parts: [],
        },
        sessionId: "s",
        agent: "build",
        lane: "main",
        timeCreated: 9_999_999_999_998,
      },
    ];
    view.rerender(
      <TranscriptTab flat={grown} jumpTarget={null} onJumped={() => undefined} paletteOpen={false} />,
    );
    expect(screen.getByText(/1 new/)).toBeDefined();

    const input = document.createElement("input");
    document.body.appendChild(input);
    fireEvent.keyDown(input, { key: "Escape" });
    expect(screen.getByText(/1 new/)).toBeDefined();
    input.remove();

    fireEvent.keyDown(window, { key: "Escape" });
    expect(screen.queryByText(/new/)).toBeNull();
  });
});

describe("windowing at scale", () => {
  function bigFlat(n: number): FlatMessage[] {
    const messages: ExportMessage[] = [];
    for (let i = 0; i < n; i += 1) {
      const id = `msg_big_${i}`;
      messages.push({
        info: {
          id,
          sessionID: "ses_big",
          role: i % 2 === 0 ? "user" : "assistant",
          time: { created: 1_700_000_000_000 + i },
        },
        parts: [
          { type: "text", id: `p_${i}`, messageID: id, sessionID: "ses_big", text: `message ${i}` },
        ],
      });
    }
    const session: ExportSession = {
      info: {
        id: "ses_big",
        slug: "big",
        projectID: "p",
        directory: "/tmp/fake-proj",
        title: "big",
        agent: "build",
        model: { id: "m", providerID: "p", variant: "d" },
        cost: 0,
        tokens: { input: 0, output: 0, reasoning: 0, cache: { read: 0, write: 0 }, total: 0 },
        time: { created: 0, updated: 0 },
        parentID: null,
      },
      messages,
    };
    return flattenTurns(session, []);
  }

  it("caps rendered rows for 1500 messages and keeps stable anchors", () => {
    const flat = bigFlat(1500);
    expect(buildTurns(flat)).toHaveLength(750);
    const onJumped = vi.fn();
    const { container } = render(
      <TranscriptTab flat={flat} jumpTarget={null} onJumped={onJumped} paletteOpen={false} />,
    );
    expect(container.querySelectorAll(".tmsg").length).toBeLessThanOrEqual(WINDOW_MAX_ROWS);
    expect(container.querySelectorAll(".tmsg").length).toBeGreaterThan(0);
  });

  it("jump-to-turn renders the distant anchor", () => {
    const flat = bigFlat(1500);
    const onJumped = vi.fn();
    render(
      <TranscriptTab
        flat={flat}
        jumpTarget="msg_big_1499"
        onJumped={onJumped}
        paletteOpen={false}
      />,
    );
    expect(onJumped).toHaveBeenCalled();
    expect(document.getElementById("turn-msg_big_1499")).not.toBeNull();
  });
});
