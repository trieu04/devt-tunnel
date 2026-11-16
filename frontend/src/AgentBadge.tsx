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

// Marks a tunnel that an agent opened. Agent tunnels have no expiry: they run
// until the agent (or a person) closes them, so the badge shows no countdown.
export function AgentBadge({ owner }: { owner: TunnelView["owner"] }) {
  const holder = owner?.holder;
  if (holder?.kind !== "mcp") return null;
  return (
    <span
      className="agent-badge"
      title={
        owner.pinned
          ? "Opened through MCP and pinned by a person; closing it from the MCP client will not stop it"
          : "Opened through MCP. It keeps running until the MCP client closes it or you stop it"
      }
    >
      MCP · {holder.name || "Unnamed client"}
      {owner.pinned && " · pinned"}
    </span>
  );
}

export function EphemeralTag({ onSave }: { onSave?: () => void }) {
  return (
    <span className="ephemeral-tag">
      ephemeral
      {onSave && (
        <>
          {" "}
          <button className="small-button" onClick={onSave}>
            Save
          </button>
        </>
      )}
    </span>
  );
}
