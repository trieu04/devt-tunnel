import { ReactNode, SyntheticEvent, useState } from "react";
import { createPortal } from "react-dom";
import type { Forward } from "../bindings/github.com/trieu04/devt-tunnel/internal/core/model";
import { STATE_LABELS } from "./lib/filter";

export function AppLogo({ size = 28 }: { size?: number }) {
  return (
    <svg className="app-logo" role="img" aria-label="devt-tunnel" width={size} height={size} viewBox="100 100 824 824">
      <rect x="100" y="100" width="824" height="824" fill="#141416" />
      <g fill="#f7f7f4">
        <rect x="336" y="336" width="352" height="56" />
        <rect x="336" y="632" width="352" height="56" />
        <rect x="336" y="392" width="56" height="240" />
        <rect x="632" y="392" width="56" height="240" />
        <rect x="224" y="484" width="112" height="56" />
        <rect x="688" y="484" width="112" height="56" />
      </g>
      <rect x="448" y="392" width="56" height="240" fill="#3d6bff" />
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

export function SettingsGlyph() {
  return (
    <svg
      aria-hidden="true"
      width="16"
      height="16"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
    >
      <circle cx="12" cy="12" r="3" />
      <path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-2.82 1.17V21a2 2 0 1 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06a1.65 1.65 0 0 0-1.17-2.82H3a2 2 0 1 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 1 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 1 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z" />
    </svg>
  );
}

export function BackGlyph() {
  return (
    <svg
      aria-hidden="true"
      width="16"
      height="16"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
    >
      <path d="M19 12H5M12 19l-7-7 7-7" />
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

/** Tooltip that does not depend on the webview showing native title text and is not clipped by the row. */
export function HoverTip({ className, lines, children }: { className?: string; lines: string[]; children: ReactNode }) {
  const [anchor, setAnchor] = useState<DOMRect | null>(null);
  const show = (event: SyntheticEvent<HTMLElement>) => setAnchor(event.currentTarget.getBoundingClientRect());
  const hide = () => setAnchor(null);
  const above = anchor !== null && anchor.top > window.innerHeight / 2;
  return (
    <>
      <span
        className={className}
        tabIndex={0}
        aria-label={lines.join(", ")}
        onMouseEnter={show}
        onMouseLeave={hide}
        onFocus={show}
        onBlur={hide}
      >
        {children}
      </span>
      {anchor &&
        createPortal(
          <div
            className="hover-tip"
            role="tooltip"
            style={{
              right: window.innerWidth - anchor.right,
              ...(above ? { bottom: window.innerHeight - anchor.top + 4 } : { top: anchor.bottom + 4 }),
            }}
          >
            {lines.map((line, index) => (
              <div key={index}>{line}</div>
            ))}
          </div>,
          document.body,
        )}
    </>
  );
}
