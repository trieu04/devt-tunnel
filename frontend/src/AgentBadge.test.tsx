import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import type { Lease } from "../bindings/github.com/trieu04/devt-tunnel/internal/core/model";
import type { TunnelView } from "./api";
import { AgentBadge, EphemeralTag, formatTimeLeft } from "./AgentBadge";
import { leaseDeadline, leaseIsActive } from "./lib/leases";

const now = Date.parse("2026-11-05T12:00:00Z");
const expiry = (seconds: number) => new Date(now + seconds * 1000).toISOString();
const owner: TunnelView["owner"] = {
  pinned: false,
  ephemeral: true,
  leases: 1,
  holder: { kind: "mcp", name: "dev-agent" },
  expiry: expiry(90),
};
const lease: Lease = {
  id: "ls_test",
  tunnelId: "t",
  holder: owner.holder!,
  expiresAt: expiry(300),
  createdAt: expiry(-60),
  session: "ss_test",
  startedIt: true,
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
  it("shows holder and time left, but not for person claims", () => {
    const html = renderToStaticMarkup(<AgentBadge owner={owner} now={now} />);
    expect(html).toContain("dev-agent");
    expect(html).toContain("2m left");
    expect(
      renderToStaticMarkup(<AgentBadge owner={{ ...owner, holder: { kind: "cli", name: "person" } }} now={now} />),
    ).toBe("");
    expect(renderToStaticMarkup(<AgentBadge owner={{ ...owner, leases: 0 }} now={now} />)).toBe("");
  });
  it("finds an agent among mixed person/agent claims and uses the disconnect grace deadline", () => {
    const claims = [
      { ...lease, graceUntil: expiry(45) },
      { ...lease, id: "ls_other", expiresAt: expiry(400) },
      { ...lease, id: "ls_person", holder: { kind: "cli", name: "person" } },
    ];
    const html = renderToStaticMarkup(
      <AgentBadge
        owner={{ ...owner, holder: { kind: "cli", name: "person" }, pinned: true }}
        now={now}
        tunnelID="t"
        leases={claims}
      />,
    );
    expect(html).toContain("dev-agent");
    expect(html).toContain("45s left");
    expect(html).toContain("2 agent leases");
    expect(html).toContain("pinned");
    expect(
      renderToStaticMarkup(<AgentBadge owner={owner} now={now} tunnelID="t" leases={[{ ...lease, revoked: true }]} />),
    ).toBe("");
  });
  it("provides an explicit Save action on the ephemeral tag", () => {
    const html = renderToStaticMarkup(<EphemeralTag onSave={() => undefined} />);
    expect(html).toContain("ephemeral");
    expect(html).toContain(">Save</button>");
  });
  it("keeps detached leases with no TTL in the active lease list", () => {
    const detached = { ...lease, detached: true, expiresAt: "0001-01-01T00:00:00Z" };
    expect(leaseIsActive(detached, now)).toBe(true);
    expect(leaseDeadline(detached)).toBeUndefined();
    expect(leaseIsActive({ ...detached, revoked: true }, now)).toBe(false);
    expect(leaseIsActive({ ...lease, graceUntil: expiry(-1) }, now)).toBe(false);
  });
});
