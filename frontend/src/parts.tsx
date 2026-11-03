import type { Forward } from "../bindings/github.com/trieu04/devt-tunnel/internal/core/model";
import { STATE_LABELS } from "./filter";

export function TunnelGlyph({ size = 16 }: { size?: number }) {
  return (
    <svg aria-hidden="true" width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
      <path d="M2 12h5M17 12h5M7 7h10v10H7z" />
    </svg>
  );
}

export function StateBadge({ state }: { state: string }) {
  return <span className={`state state-${state}`}>{STATE_LABELS[state] ?? state}</span>;
}

/** Every kind reads listen → target; the badge says which side listens. */
export function ForwardLine({ forward }: { forward: Forward }) {
  return (
    <div className="forward">
      <span className="badge">-{forward.kind}</span>
      <span className="mono">
        {forward.bind}
        {" → "}
        {forward.target || "SOCKS5"}
      </span>
    </div>
  );
}
