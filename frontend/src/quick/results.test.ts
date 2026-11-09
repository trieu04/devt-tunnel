import { describe, expect, it } from "vitest";
import type { TunnelView } from "../api";
import { buildResults } from "./results";

const tunnel = (id: string, name: string, host: string): TunnelView =>
  ({
    tunnel: { id, name, type: "ssh", ssh: { host } },
    status: { id, state: "stopped", owner: { pinned: false, ephemeral: false, leases: 0 } },
    owner: { pinned: false, ephemeral: false, leases: 0 },
    preview: "",
    missingSecrets: [],
    endpoints: [],
  }) as TunnelView;
describe("quick results", () => {
  it("groups tunnels, unused hosts and actions", () => {
    const groups = buildResults({
      query: "",
      scope: "all",
      tunnels: [tunnel("a", "Alpha", "used")],
      hosts: [
        { alias: "used", source: "config" },
        { alias: "new", source: "config" },
      ],
      recents: {},
    });
    expect(groups.map((group) => group.group)).toEqual(["Tunnels", "SSH hosts", "Actions"]);
    expect(groups[1].items).toHaveLength(1);
  });
  it("sorts recents first", () => {
    const groups = buildResults({
      query: "",
      scope: "tunnels",
      tunnels: [tunnel("a", "A", "a"), tunnel("b", "B", "b")],
      hosts: [],
      recents: { a: "2026-01-01", b: "2026-02-01" },
    });
    expect(groups[0].items.map((item) => (item.kind === "tunnel" ? item.view.tunnel.id : ""))).toEqual(["b", "a"]);
  });
});
