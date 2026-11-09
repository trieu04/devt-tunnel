import type { TunnelView } from "../api";

export type ViewFilter = "all" | "running" | "favorites";

const ACTIVE = new Set(["starting", "running", "reconnecting"]);

export function isActive(state: string): boolean {
  return ACTIVE.has(state);
}

export function searchText(v: TunnelView): string {
  const t = v.tunnel;
  return [
    t.name,
    t.ssh?.host,
    t.ssh?.hostname,
    t.ngrok?.addr,
    t.ngrok?.domain,
    t.cloudflared?.hostname,
    t.cloudflared?.url,
    t.command?.line,
    ...(t.forwards ?? []).flatMap((f) => [f.bind, f.target]),
  ]
    .filter(Boolean)
    .join(" ")
    .toLowerCase();
}

export function matchesSearch(v: TunnelView, query: string): boolean {
  const text = searchText(v);
  return query
    .toLowerCase()
    .split(/\s+/)
    .filter(Boolean)
    .every((word) => text.includes(word));
}

/** A group of null means every tunnel. */
export function inGroup(v: TunnelView, group: string | null): boolean {
  return group === null || (v.tunnel.groups ?? []).includes(group);
}

export function filterTunnels(
  tunnels: TunnelView[],
  opts: { query: string; view: ViewFilter; group: string | null },
): TunnelView[] {
  return tunnels.filter((v) => {
    if (opts.view === "running" && !isActive(v.status.state)) return false;
    if (opts.view === "favorites" && !v.tunnel.favorite) return false;
    if (!inGroup(v, opts.group)) return false;
    return matchesSearch(v, opts.query);
  });
}

export const STATE_LABELS: Record<string, string> = {
  stopped: "Stopped",
  starting: "Starting",
  running: "Running",
  reconnecting: "Reconnecting",
  failed: "Failed",
  needs_token: "Needs token",
  keyring_unavailable: "Keyring unavailable",
};
