import { describe, expect, it } from "vitest";
import type { TunnelView } from "../api";
import { buildResults } from "./results";

const tunnel = (id: string, name: string, host: string, profiles: string[] = []): TunnelView =>
  ({
    tunnel: { id, name, type: "ssh", ssh: { host }, profiles },
    status: { id, state: "stopped" },
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
      activeProfile: "",
    });
    expect(groups.map((group) => group.group)).toEqual(["Tunnels", "SSH hosts", "Actions"]);
    expect(groups[1].items).toHaveLength(1);
  });
  it("sorts recents and filters profiles", () => {
    const groups = buildResults({
      query: "",
      scope: "tunnels",
      tunnels: [tunnel("a", "A", "a", ["work"]), tunnel("b", "B", "b", ["home"])],
      hosts: [],
      recents: { a: "2026-01-01", b: "2026-02-01" },
      activeProfile: "work",
    });
    expect(groups[0].items).toHaveLength(1);
  });
});
