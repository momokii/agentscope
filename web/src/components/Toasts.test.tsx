import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";

import Toasts from "./Toasts";

afterEach(cleanup);

describe("Toasts", () => {
  it("renders nothing when empty", () => {
    const { container } = render(<Toasts toasts={[]} onDismiss={() => undefined} />);
    expect(container.querySelector(".toasts")).toBeNull();
  });

  it("shows title, body, and a session link", () => {
    render(
      <Toasts
        toasts={[
          {
            toastId: 1,
            kind: "waiting",
            sessionId: "ses_1",
            title: "Input needed: fix bug",
            body: "alpha · waiting on you",
          },
        ]}
        onDismiss={() => undefined}
      />,
    );
    expect(screen.getByText("Input needed: fix bug")).toBeDefined();
    expect(screen.getByRole("link", { name: "Open session" }).getAttribute("href")).toBe(
      "#/sessions/ses_1",
    );
  });

  it("dismisses on close", () => {
    const onDismiss = vi.fn();
    render(
      <Toasts
        toasts={[{ toastId: 7, kind: "budget", title: "t", body: "b" }]}
        onDismiss={onDismiss}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: "Dismiss alert" }));
    expect(onDismiss).toHaveBeenCalledWith(7);
  });
});
