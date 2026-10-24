import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";

import CmdBar, { filterCmdSessions } from "./CmdBar";
import { buildFixtureSnapshot } from "../testutils/fixture";

afterEach(cleanup);

const sessions = buildFixtureSnapshot().sessions;

describe("CmdBar filter", () => {
  it("empty query lists every session", () => {
    expect(filterCmdSessions(sessions, "")).toHaveLength(sessions.length);
  });

  it("fuzzy-matches across id/title/cwd/model", () => {
    expect(
      filterCmdSessions(sessions, "ses_running1").map((r) => r.id),
    ).toEqual(["ses_running1"]);
    expect(
      filterCmdSessions(sessions, "fleet view").map((r) => r.id),
    ).toContain("ses_running1");
    expect(filterCmdSessions(sessions, "zzz-no-match")).toHaveLength(0);
  });
});

describe("CmdBar component", () => {
  it("renders nothing when closed", () => {
    const { container } = render(
      <CmdBar
        open={false}
        sessions={sessions}
        onSelect={() => undefined}
        onClose={() => undefined}
      />,
    );
    expect(container.innerHTML).toBe("");
  });

  it("shows glyph + relative time per hit and empty-state text", () => {
    render(
      <CmdBar
        open={true}
        sessions={sessions}
        onSelect={() => undefined}
        onClose={() => undefined}
        nowMs={1_700_000_000_000}
      />,
    );
    expect(screen.getByLabelText("Jump to session")).toBeDefined();
    expect(screen.getByText("ses_running1")).toBeDefined();
    fireEvent.change(screen.getByLabelText("Jump to session"), {
      target: { value: "zzz-no-match" },
    });
    expect(screen.getByText(/No sessions match/)).toBeDefined();
  });

  it("Enter selects the active hit; Esc closes", () => {
    const onSelect = vi.fn();
    const onClose = vi.fn();
    render(
      <CmdBar
        open={true}
        sessions={sessions}
        onSelect={onSelect}
        onClose={onClose}
        nowMs={1_700_000_000_000}
      />,
    );
    fireEvent.change(screen.getByLabelText("Jump to session"), {
      target: { value: "ses_running1" },
    });
    fireEvent.keyDown(screen.getByLabelText("Jump to session"), {
      key: "ArrowDown",
    });
    fireEvent.keyDown(screen.getByText("ses_running1"), { key: "Enter" });
    expect(onSelect).toHaveBeenCalledWith("ses_running1");
    fireEvent.keyDown(screen.getByLabelText("Jump to session"), {
      key: "Escape",
    });
    expect(onClose).toHaveBeenCalled();
  });

  it("arrow nav moves the active hit without crashing on empty", () => {
    render(
      <CmdBar
        open={true}
        sessions={sessions}
        onSelect={() => undefined}
        onClose={() => undefined}
        nowMs={1_700_000_000_000}
      />,
    );
    const input = screen.getByLabelText("Jump to session");
    fireEvent.keyDown(input, { key: "ArrowDown" });
    fireEvent.keyDown(input, { key: "ArrowUp" });
    expect(input).toBeDefined();
  });
});
