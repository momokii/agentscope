import { describe, expect, it } from "vitest";

import { isTypingTarget } from "./useCommandPalette";

describe("isTypingTarget", () => {
  it("treats inputs, textareas, selects, and contentEditable as typing", () => {
    expect(isTypingTarget(document.createElement("input"))).toBe(true);
    expect(isTypingTarget(document.createElement("textarea"))).toBe(true);
    expect(isTypingTarget(document.createElement("select"))).toBe(true);
    const editable = document.createElement("div");
    // jsdom does not compute isContentEditable; stub the DOM property.
    Object.defineProperty(editable, "isContentEditable", { value: true });
    expect(isTypingTarget(editable)).toBe(true);
  });

  it("ignores body, buttons, and null (digits jump, : opens)", () => {
    expect(isTypingTarget(document.body)).toBe(false);
    expect(isTypingTarget(document.createElement("button"))).toBe(false);
    expect(isTypingTarget(null)).toBe(false);
  });
});
