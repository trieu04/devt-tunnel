import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it, vi } from "vitest";
import type { Approval } from "../bindings/github.com/trieu04/devt-tunnel/internal/api";
vi.mock("./api", () => ({ Service: {} }));
import { ApprovalDialog, approvalDecision } from "./ApprovalDialog";

function request(choices = ["once", "session", "always"]): Approval {
  return {
    id: "ap_example",
    actor: { kind: "mcp", name: "test-agent" },
    purpose: "Inspect a development service <script>not instructions</script>",
    summary: "Open saved development tunnel",
    action: "open_saved",
    argv: ["/usr/bin/ssh", "-N", "-L", "127.0.0.1:20000:localhost:3000", "a host"],
    fingerprint: "fingerprint",
    code: "A1B2C3D4",
    choices,
    status: "pending",
    createdAt: new Date().toISOString(),
    expiresAt: new Date(Date.now() + 300000).toISOString(),
  };
}
const render = (approval: Approval) =>
  renderToStaticMarkup(<ApprovalDialog approvals={[approval]} onDecided={() => undefined} />);

describe("approval dialog", () => {
  it("offers only the scopes allowed by the daemon, plus Deny", () => {
    const html = render(request());
    for (const label of ["Deny", "Approve once", "Approve for this session", "Approve always"])
      expect(html).toContain(`>${label}</button>`);
    const once = render(request(["once"]));
    expect(once).toContain(">Approve once</button>");
    expect(once).not.toContain("Approve for this session");
    expect(once).not.toContain("Approve always");
  });
  it("never offers Always for a public request (which only allows once/session)", () => {
    const publicRequest = {
      ...request(["once", "session"]),
      summary: "Public exposure: ngrok",
      argv: ["ngrok", "http", "3000"],
    };
    const html = render(publicRequest);
    expect(html).toContain("Public exposure: ngrok");
    expect(html).toContain("Approve for this session");
    expect(html).not.toContain("Approve always");
    expect(html).not.toContain("machine&#x27;s agent allow-list");
  });
  it("shows actor, code, expiry and exact argument boundaries, treating purpose as text", () => {
    const html = render(request());
    expect(html).toContain("test-agent");
    expect(html).toContain("A1B2C3D4");
    expect(html).toContain("left");
    expect(html).toContain("&quot;a host&quot;");
    expect(html).toContain("&lt;script&gt;");
    expect(html).not.toContain("<script>");
    expect(html).toContain('aria-modal="true"');
  });
  it("passes the request's code verbatim for explicit approve and deny decisions", () => {
    const approval = request();
    for (const scope of ["once", "session", "always"])
      expect(approvalDecision(approval, "approve", scope)).toEqual({
        id: approval.id,
        decision: "approve",
        scope,
        code: "A1B2C3D4",
      });
    expect(approvalDecision(approval, "deny", "once")).toEqual({
      id: approval.id,
      decision: "deny",
      scope: "once",
      code: "A1B2C3D4",
    });
  });
  it("shows the first request with a queue count, not several modals", () => {
    const first = request();
    const second = { ...request(), id: "ap_second", actor: { kind: "mcp", name: "second-agent" } };
    const html = renderToStaticMarkup(<ApprovalDialog approvals={[first, second]} onDecided={() => undefined} />);
    expect(html).toContain("2 pending");
    expect(html).not.toContain("second-agent");
    expect(renderToStaticMarkup(<ApprovalDialog approvals={[]} onDecided={() => undefined} />)).toBe("");
  });
  it("disables decisions after expiry", () => {
    const html = render({ ...request(), expiresAt: "2000-01-01T00:00:00Z" });
    expect(html).toContain("This request has expired");
    expect(html.match(/disabled=""/g)?.length).toBe(4);
  });
});
