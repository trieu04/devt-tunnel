import { describe, expect, it, vi } from "vitest";
vi.mock("./api", () => ({}));
import { filterTunnels, matchesSearch } from "./filter";
import type { TunnelView } from "./api";

function view(partial: Record<string, unknown>, state = "stopped"): TunnelView {
  return { tunnel: { id: "x", name: "x", type: "ssh", ...partial }, status: { id: "x", state }, preview: "" } as unknown as TunnelView;
}

const db = view({
  id: "db",
  name: "Production DB",
  profiles: ["work"],
  favorite: true,
  ssh: { host: "server1" },
  forwards: [{ kind: "L", bind: "127.0.0.1:5433", target: "10.0.0.1:5432" }],
}, "running");
const web = view({ id: "web", name: "Web", type: "ngrok", profiles: ["home"], ngrok: { proto: "http", addr: "127.0.0.1:3000" } });
const all = [db, web];
const none = { query: "", view: "all", type: "all", profile: "all" } as const;

describe("matchesSearch", () => {
  it("matches name, host and port", () => {
    expect(matchesSearch(db, "production")).toBe(true);
    expect(matchesSearch(db, "SERVER1")).toBe(true);
    expect(matchesSearch(db, "5433")).toBe(true);
    expect(matchesSearch(db, "3000")).toBe(false);
  });
  it("requires every word", () => {
    expect(matchesSearch(db, "prod 5433")).toBe(true);
    expect(matchesSearch(db, "prod 3000")).toBe(false);
  });
  it("matches everything for an empty query", () => {
    expect(matchesSearch(web, "  ")).toBe(true);
  });
});

describe("filterTunnels", () => {
  const ids = (o: Partial<typeof none> | object) => filterTunnels(all, { ...none, ...o }).map((v) => v.tunnel.id);
  it("filters by view", () => {
    expect(ids({ view: "running" })).toEqual(["db"]);
    expect(ids({ view: "favorites" })).toEqual(["db"]);
  });
  it("filters by type and profile", () => {
    expect(ids({ type: "ngrok" })).toEqual(["web"]);
    expect(ids({ profile: "work" })).toEqual(["db"]);
  });
  it("treats reconnecting as active", () => {
    const r = filterTunnels([view({ id: "r" }, "reconnecting")], { ...none, view: "running" });
    expect(r).toHaveLength(1);
  });
});
