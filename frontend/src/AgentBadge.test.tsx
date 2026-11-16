import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import type { TunnelView } from "./api";
import { AgentBadge, EphemeralTag, formatTimeLeft } from "./AgentBadge";

const now = Date.parse("2026-11-05T12:00:00Z");
const expiry = (seconds: number) => new Date(now + seconds * 1000).toISOString();
const owner: TunnelView["owner"] = {
  pinned: false,
  ephemeral: true,
  holder: { kind: "mcp", name: "dev-agent" },
};

describe("agent badge", () => {
  it("formats a live countdown with seconds, minutes and hours", () => {
    expect(formatTimeLeft(expiry(32), now)).toBe("32s left");
    expect(formatTimeLeft(expiry(90), now)).toBe("2m left");
    expect(formatTimeLeft(expiry(3600), now)).toBe("1h left");
    expect(formatTimeLeft(expiry(3660), now)).toBe("1h 1m left");
    expect(formatTimeLeft(expiry(0), now)).toBe("Expired");
    expect(formatTimeLeft(expiry(-30), now)).toBe("Expired");
    expect(formatTimeLeft(undefined, now)).toBe("Expiry unavailable");
    expect(formatTimeLeft("invalid", now)).toBe("Expiry unavailable");
  });
  it("shows the agent marker with no countdown, but not for person holders", () => {
    const html = renderToStaticMarkup(<AgentBadge owner={owner} />);
    expect(html).toContain("dev-agent");
    expect(html).not.toContain("left");
    expect(renderToStaticMarkup(<AgentBadge owner={{ ...owner, holder: { kind: "cli", name: "person" } }} />)).toBe("");
    expect(renderToStaticMarkup(<AgentBadge owner={{ ...owner, holder: undefined }} />)).toBe("");
  });
  it("marks a pinned agent tunnel", () => {
    const html = renderToStaticMarkup(<AgentBadge owner={{ ...owner, pinned: true }} />);
    expect(html).toContain("dev-agent");
    expect(html).toContain("pinned");
  });
  it("provides an explicit Save action on the ephemeral tag", () => {
    const html = renderToStaticMarkup(<EphemeralTag onSave={() => undefined} />);
    expect(html).toContain("ephemeral");
    expect(html).toContain(">Save</button>");
  });
});
