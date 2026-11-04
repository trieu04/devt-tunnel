import type { Forward } from "../bindings/github.com/trieu04/devt-tunnel/internal/core/model";
import { STATE_LABELS } from "./lib/filter";

export function TunnelGlyph({ size = 16 }: { size?: number }) {
  return (
    <svg
      aria-hidden="true"
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
    >
      <path d="M2 12h5M17 12h5M7 7h10v10H7z" />
    </svg>
  );
}

export function SearchGlyph() {
  return (
    <svg
      aria-hidden="true"
      width="16"
      height="16"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
    >
      <circle cx="11" cy="11" r="7" />
      <path d="m20 20-4-4" />
    </svg>
  );
}

export function PlusGlyph() {
  return (
    <svg
      aria-hidden="true"
      width="16"
      height="16"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
    >
      <path d="M12 5v14M5 12h14" />
    </svg>
  );
}

export function ChevronGlyph({ expanded }: { expanded: boolean }) {
  return (
    <svg
      aria-hidden="true"
      width="16"
      height="16"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      className={expanded ? "chevron-expanded" : ""}
    >
      <path d="m9 6 6 6-6 6" />
    </svg>
  );
}

export function CopyGlyph() {
  return (
    <svg
      aria-hidden="true"
      width="16"
      height="16"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
    >
      <rect x="9" y="9" width="11" height="11" />
      <path d="M5 15V4h11" />
    </svg>
  );
}

export function PlayGlyph() {
  return (
    <svg
      aria-hidden="true"
      width="17"
      height="17"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinejoin="round"
    >
      <path d="m7 4 13 8-13 8z" />
    </svg>
  );
}

export function StopGlyph() {
  return (
    <svg aria-hidden="true" width="14" height="14" viewBox="0 0 24 24" fill="currentColor">
      <rect x="5" y="5" width="14" height="14" />
    </svg>
  );
}

export function KeyGlyph() {
  return (
    <svg
      aria-hidden="true"
      width="16"
      height="16"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
    >
      <circle cx="8" cy="15" r="4" />
      <path d="m11 12 9-9M17 6l3 3" />
    </svg>
  );
}

export function ListGlyph() {
  return (
    <svg
      aria-hidden="true"
      width="16"
      height="16"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
    >
      <path d="M4 6h16M4 12h10M4 18h6" />
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
