import { describe, expect, it } from "vitest";
import { shortcutAction } from "./shortcuts";

describe("shortcutAction", () => {
  it("maps command on macOS and control elsewhere", () => {
    expect(shortcutAction({ key: "n", metaKey: true, ctrlKey: false, shiftKey: false, target: null }, true)).toBe(
      "new",
    );
    expect(shortcutAction({ key: "k", metaKey: false, ctrlKey: true, shiftKey: false, target: null }, false)).toBe(
      "quick",
    );
  });
  it("maps paste and toggle", () => {
    expect(shortcutAction({ key: "v", metaKey: true, ctrlKey: false, shiftKey: true, target: null }, true)).toBe(
      "paste",
    );
    expect(shortcutAction({ key: "Enter", metaKey: true, ctrlKey: false, shiftKey: false, target: null }, true)).toBe(
      "toggle",
    );
  });
});
