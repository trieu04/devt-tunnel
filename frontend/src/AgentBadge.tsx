import type { Lease } from "../bindings/github.com/trieu04/devt-tunnel/internal/core/model";
import { leaseDeadline, leaseIsActive } from "./lib/leases";
import type { TunnelView } from "./api";

export function formatTimeLeft(expiry: string | null | undefined, now: number): string {
  if (!expiry || !Number.isFinite(Date.parse(expiry))) return "Expiry unavailable";
  const seconds = Math.max(0, Math.ceil((Date.parse(expiry) - now) / 1000));
  if (!seconds) return "Expired";
  if (seconds < 60) return `${seconds}s left`;
  const minutes = Math.ceil(seconds / 60);
  if (minutes < 60) return `${minutes}m left`;
  const hours = Math.floor(minutes / 60);
  const remainder = minutes % 60;
  return `${hours}h${remainder ? ` ${remainder}m` : ""} left`;
}

export function AgentBadge({
  owner,
  now,
  tunnelID,
  leases,
}: {
  owner: TunnelView["owner"];
  now: number;
  tunnelID?: string;
  leases?: Lease[];
}) {
  // owner.holder represents just the earliest claim. Inspect individual leases
  // as well so a CLI/GUI claim cannot hide another agent's ownership.
  const agents = leases
    ?.filter((lease) => lease.tunnelId === tunnelID && lease.holder.kind === "mcp" && leaseIsActive(lease, now))
    .sort((a, b) => Date.parse(leaseDeadline(a) ?? "") - Date.parse(leaseDeadline(b) ?? ""));
  const holder = agents ? agents[0]?.holder : owner?.holder;
  const count = agents ? agents.length : owner?.leases;
  const expiry = agents ? agents[0] && leaseDeadline(agents[0]) : owner?.expiry;
  if (holder?.kind !== "mcp" || !count) return null;
  return (
    <span
      className="agent-badge"
      title={owner.pinned ? "Pinned by a person; lease expiry will not stop this tunnel" : "Held by an agent lease"}
    >
      Agent · {holder.name || "Unnamed agent"} · {formatTimeLeft(expiry, now)}
      {count > 1 && ` · ${count} agent leases`}
      {owner.pinned && " · pinned"}
    </span>
  );
}

export function EphemeralTag({ onSave }: { onSave: () => void }) {
  return (
    <span className="ephemeral-tag">
      ephemeral{" "}
      <button className="small-button" onClick={onSave}>
        Save
      </button>
    </span>
  );
}
