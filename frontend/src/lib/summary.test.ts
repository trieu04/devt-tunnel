import { describe, expect, it } from "vitest";
import type { Tunnel } from "../../bindings/github.com/trieu04/devt-tunnel/internal/core/model/models";
import { commandSummary, fitCount, mergeShortFlags } from "./summary";

const tunnel = (partial: Record<string, unknown>) => ({ id: "t", name: "T", ...partial }) as unknown as Tunnel;

describe("command summary", () => {
  it("merges only one-letter flags", () => {
    expect(mergeShortFlags(["-N", "-T", "-q"])).toEqual(["-NTq"]);
    expect(mergeShortFlags(["-N", "--long", "-C"])).toEqual(["-NC", "--long"]);
    expect(mergeShortFlags(["-oFoo"])).toEqual(["-oFoo"]);
    expect(mergeShortFlags([])).toEqual([]);
  });
  it("describes each tunnel type", () => {
    expect(commandSummary(tunnel({ type: "ssh", ssh: { host: "fd", flags: ["-N", "-T", "-q"] } }))).toBe("ssh fd -NTq");
    expect(commandSummary(tunnel({ type: "ssh", ssh: { user: "me", hostname: "a.b" } }))).toBe("ssh me@a.b");
    expect(commandSummary(tunnel({ type: "ngrok", ngrok: { proto: "http", addr: "127.0.0.1:3000" } }))).toBe(
      "ngrok http 127.0.0.1:3000",
    );
    expect(commandSummary(tunnel({ type: "cloudflared", cloudflared: { name: "prod" } }))).toBe("cloudflared prod");
    expect(commandSummary(tunnel({ type: "command", command: { line: "npm run dev" } }))).toBe("command npm run dev");
  });
});

describe("fitCount", () => {
  const widths = [50, 50, 50];
  it("keeps every item when all of them fit, without room for the marker", () => {
    expect(fitCount(widths, 20, 170, 10)).toBe(3);
  });
  it("leaves room for the marker when some items are cut", () => {
    expect(fitCount(widths, 20, 169, 10)).toBe(2);
    expect(fitCount(widths, 20, 139, 10)).toBe(1);
  });
  it("always keeps the first item and handles an empty list", () => {
    expect(fitCount(widths, 20, 5, 10)).toBe(1);
    expect(fitCount([], 20, 0, 10)).toBe(0);
  });
});
