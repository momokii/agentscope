import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";

import AlertsBell, {
  loadAlertsConfig,
  saveAlertsConfig,
} from "./AlertsBell";

afterEach(() => {
  cleanup();
  window.localStorage.clear();
});

const base = {
  config: { enabled: false, budget: null as number | null },
  supported: true,
  permission: "default" as NotificationPermission,
  history: [],
  onClearHistory: () => undefined,
};

describe("AlertsBell", () => {
  it("toggles the panel and persists the opt-in", () => {
    const onChange = vi.fn();
    render(<AlertsBell {...base} onChange={onChange} onEnable={() => undefined} onTest={() => undefined} unread={0} />);
    expect(screen.queryByRole("dialog")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: /alert settings/i }));
    expect(screen.getByRole("dialog", { name: "Alert settings" })).toBeDefined();
    fireEvent.click(screen.getByRole("checkbox"));
    expect(onChange).toHaveBeenCalledWith({ enabled: true, budget: null });
  });

  it("shows the unread count badge", () => {
    render(
      <AlertsBell {...base} onChange={() => undefined} onEnable={() => undefined} onTest={() => undefined} unread={3} />,
    );
    expect(screen.getByLabelText("3 unread alerts")).toBeDefined();
  });

  it("explains itself when notifications are unavailable", () => {
    render(
      <AlertsBell
        {...base}
        supported={false}
        permission="unsupported"
        onChange={() => undefined}
        onEnable={() => undefined}
        onTest={() => undefined}
        unread={0}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: /alert settings/i }));
    expect(screen.getByText(/not a secure context/i)).toBeDefined();
  });

  it("lists surfaced triggers with when and why, newest first", () => {
    const { container } = render(
      <AlertsBell
        {...base}
        history={[
          {
            kind: "budget",
            title: "Daily budget crossed",
            body: "b",
            ts: Date.now() - 60_000,
            why: "cost_today $5.01 ≥ budget $5.00; fires once per local day",
          },
        ]}
        onChange={() => undefined}
        onEnable={() => undefined}
        onTest={() => undefined}
        unread={0}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: /alert settings/i }));
    expect(screen.getByText("history (1)")).toBeDefined();
    expect(screen.getByText("Daily budget crossed")).toBeDefined();
    expect(screen.getByText(/cost_today \$5\.01/)).toBeDefined();
    expect(container.querySelector(".alerts-log")).not.toBeNull();
  });

  it("round-trips config through localStorage", () => {
    saveAlertsConfig({ enabled: true, budget: 12.5 });
    expect(loadAlertsConfig()).toEqual({ enabled: true, budget: 12.5 });
    saveAlertsConfig({ enabled: false, budget: null });
    expect(loadAlertsConfig()).toEqual({ enabled: false, budget: null });
  });
});
