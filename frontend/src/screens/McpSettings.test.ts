import { describe, expect, it } from "vitest";
import { callLabel, formatCallTime, pageCount, pageSlice } from "./McpSettings";

describe("MCP history labels", () => {
  it("uses MCP wording for known audit actions and passes unknown ones through", () => {
    expect(callLabel("tunnel.open")).toBe("open tunnel");
    expect(callLabel("lease.open")).toBe("open tunnel");
    expect(callLabel("policy.deny")).toBe("refused by policy");
    expect(callLabel("future.action")).toBe("future.action");
  });
  it("falls back to the raw text for an unparsable time", () => {
    expect(formatCallTime("not a time")).toBe("not a time");
    expect(formatCallTime("2026-11-05T10:00:00Z")).not.toBe("2026-11-05T10:00:00Z");
  });
});

describe("MCP history pagination", () => {
  const items = Array.from({ length: 250 }, (_, i) => i);
  it("defaults to 100 per page and clamps the page", () => {
    expect(pageCount(250)).toBe(3);
    expect(pageCount(0)).toBe(1);
    expect(pageSlice(items, 0)).toHaveLength(100);
    expect(pageSlice(items, 2)).toHaveLength(50);
    expect(pageSlice(items, 9)[0]).toBe(200);
  });
});
