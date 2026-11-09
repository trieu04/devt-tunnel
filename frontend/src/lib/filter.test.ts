import { describe, expect, it, vi } from "vitest";
vi.mock("../api", () => ({}));
import { filterTunnels, matchesSearch } from "./filter";
import type { TunnelView } from "../api";

function view(partial: Record<string, unknown>, state = "stopped"): TunnelView {
  return {
    tunnel: { id: "x", name: "x", type: "ssh", ...partial },
    status: { id: "x", state },
    preview: "",
  } as TunnelView;
}
const db = view(
  {
    id: "db",
    name: "Production DB",
    profile: "work",
    favorite: true,
    ssh: { host: "server1" },
    forwards: [{ kind: "L", bind: "127.0.0.1:5433", target: "10.0.0.1:5432" }],
  },
  "running",
);
const web = view({
  id: "web",
  name: "Web",
  type: "ngrok",
  profile: "home",
  groups: ["stack", "public"],
  ngrok: { proto: "http", addr: "127.0.0.1:3000" },
});
const none = { query: "", view: "all", group: null } as const;

describe("search and filters", () => {
  it("matches name, host and port", () => {
    expect(matchesSearch(db, "production")).toBe(true);
    expect(matchesSearch(db, "SERVER1")).toBe(true);
    expect(matchesSearch(db, "5433")).toBe(true);
  });
  it("filters view and group", () => {
    const ids = (opts: Partial<Parameters<typeof filterTunnels>[1]>) =>
      filterTunnels([db, web], { ...none, ...opts }).map((v) => v.tunnel.id);
    expect(ids({ view: "running" })).toEqual(["db"]);
    expect(ids({ group: "public" })).toEqual(["web"]);
    expect(ids({ group: "missing" })).toEqual([]);
  });
});
