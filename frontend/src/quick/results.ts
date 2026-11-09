import type { TunnelView } from "../api";
import type { Host } from "../../bindings/github.com/trieu04/devt-tunnel/internal/core/sshconfig/models";
import { matchesSearch, searchText } from "../lib/filter";

export type Scope = "all" | "tunnels" | "hosts" | "actions";
export type ActionId = "new" | "paste" | "main" | "vault" | "stop-all";
export type Item =
  | { kind: "tunnel"; view: TunnelView }
  | { kind: "host"; host: Host }
  | { kind: "action"; id: ActionId; label: string; hint: string };
export type Group = { group: string; items: Item[] };

const ACTIONS: Extract<Item, { kind: "action" }>[] = [
  { kind: "action", id: "new", label: "New tunnel", hint: "⌘N" },
  { kind: "action", id: "paste", label: "Create from pasted command", hint: "⌘⇧V" },
  { kind: "action", id: "main", label: "Open main window", hint: "" },
  { kind: "action", id: "vault", label: "Open Vault", hint: "" },
  { kind: "action", id: "stop-all", label: "Stop all tunnels", hint: "" },
];

function quality(text: string, query: string): number {
  if (!query) return 0;
  if (text.startsWith(query)) return 0;
  if (text.split(/\s+/).some((word) => word.startsWith(query))) return 1;
  return 2;
}

export function buildResults(input: {
  query: string;
  scope: Scope;
  tunnels: TunnelView[];
  hosts: Host[];
  recents: Record<string, string | undefined>;
}): Group[] {
  const query = input.query.trim().toLowerCase();
  const visible = input.tunnels;
  const usedHosts = new Set(input.tunnels.map((v) => v.tunnel.ssh?.host).filter(Boolean));
  const tunnels = visible
    .filter((v) => matchesSearch(v, query))
    .sort(
      (a, b) =>
        quality(searchText(a), query) - quality(searchText(b), query) ||
        Date.parse(input.recents[b.tunnel.id] ?? "0") - Date.parse(input.recents[a.tunnel.id] ?? "0") ||
        visible.indexOf(a) - visible.indexOf(b),
    )
    .slice(0, 20);
  const hosts = input.hosts
    .filter((host) => !usedHosts.has(host.alias) && host.alias.toLowerCase().includes(query))
    .slice(0, 12);
  const actions = ACTIONS.filter((action) => action.label.toLowerCase().includes(query)).slice(0, 8);
  const groups: Group[] = [];
  if ((input.scope === "all" || input.scope === "tunnels") && tunnels.length)
    groups.push({ group: "Tunnels", items: tunnels.map((view) => ({ kind: "tunnel", view })) });
  if ((input.scope === "all" || input.scope === "hosts") && hosts.length)
    groups.push({ group: "SSH hosts", items: hosts.map((host) => ({ kind: "host", host })) });
  if ((input.scope === "all" || input.scope === "actions") && actions.length)
    groups.push({ group: "Actions", items: actions });
  return groups;
}
