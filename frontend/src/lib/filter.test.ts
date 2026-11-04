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
    profiles: ["work"],
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
  profiles: ["home"],
  ngrok: { proto: "http", addr: "127.0.0.1:3000" },
});
const none = { query: "", view: "all", type: "all", profile: "all" } as const;

describe("search and filters", () => {
  it("matches name, host and port", () => {
    expect(matchesSearch(db, "production")).toBe(true);
    expect(matchesSearch(db, "SERVER1")).toBe(true);
    expect(matchesSearch(db, "5433")).toBe(true);
  });
  it("filters view, type and profile", () => {
    expect(filterTunnels([db, web], { ...none, view: "running" }).map((v) => v.tunnel.id)).toEqual(["db"]);
    expect(filterTunnels([db, web], { ...none, type: "ngrok" }).map((v) => v.tunnel.id)).toEqual(["web"]);
    expect(filterTunnels([db, web], { ...none, profile: "work" }).map((v) => v.tunnel.id)).toEqual(["db"]);
  });
});
