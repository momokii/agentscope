import { afterEach, describe, expect, it } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";

import SessionStub from "./SessionStub";
import { buildFixtureSnapshot } from "../testutils/fixture";

afterEach(cleanup);

describe("SessionStub", () => {
  it("renders summary facts from Snapshot data with the Wave 3 note", () => {
    const row = buildFixtureSnapshot().sessions[0]!;
    render(<SessionStub row={row} id={row.id} />);
    expect(screen.getByText("Implement fleet view")).toBeDefined();
    expect(screen.getByText(row.id)).toBeDefined();
    expect(screen.getByText(/Wave 3/)).toBeDefined();
  });

  it("handles unknown ids without crashing", () => {
    render(<SessionStub row={null} id="ses_missing" />);
    expect(screen.getByText(/No session/)).toBeDefined();
    expect(screen.getByText(/Wave 3/)).toBeDefined();
  });
});
