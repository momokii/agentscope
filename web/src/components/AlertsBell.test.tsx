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

  it("round-trips config through localStorage", () => {
    saveAlertsConfig({ enabled: true, budget: 12.5 });
    expect(loadAlertsConfig()).toEqual({ enabled: true, budget: 12.5 });
    saveAlertsConfig({ enabled: false, budget: null });
    expect(loadAlertsConfig()).toEqual({ enabled: false, budget: null });
  });
});
