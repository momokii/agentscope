import { afterEach, describe, expect, it } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";

import DataGuide from "./DataGuide";

afterEach(cleanup);

describe("DataGuide", () => {
  it("toggles the glossary panel", () => {
    render(<DataGuide />);
    expect(screen.queryByLabelText("Data guide")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "How to read this dashboard" }));
    const panel = screen.getByLabelText("Data guide");
    expect(panel).toBeDefined();
    expect(panel.textContent).toContain("needs input");
    expect(panel.textContent).toContain("Metrics age");
    expect(panel.textContent).toContain("Live pane");
    fireEvent.click(screen.getByRole("button", { name: "Hide data guide" }));
    expect(screen.queryByLabelText("Data guide")).toBeNull();
  });
});
