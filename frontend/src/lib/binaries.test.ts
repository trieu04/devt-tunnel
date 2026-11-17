import { describe, expect, it } from "vitest";
import type { TunnelView } from "../../bindings/github.com/trieu04/devt-tunnel/internal/api";
import {
  binariesParam,
  CHECKSUM_MESSAGE,
  downloadErrorText,
  formatSize,
  installCommands,
  isConflictError,
  missingToolFromFinding,
  originLabel,
  parseSettingsParam,
  platformOf,
  progressFraction,
  progressText,
  reduceDownloads,
  removeDisabled,
  staleMessage,
  staleTunnels,
  truncateMiddle,
  versionFromPath,
  type DownloadStates,
  type ToolDownloadEvent,
} from "./binaries";

const view = (type: string, state: string, toolPath?: string) =>
  ({ tunnel: { id: type + state, type }, status: { state, toolPath } }) as unknown as TunnelView;

describe("truncateMiddle", () => {
  it("leaves short paths alone", () => {
    expect(truncateMiddle("/usr/bin/ssh", 48)).toBe("/usr/bin/ssh");
  });
  it("keeps both ends and the requested length", () => {
    const out = truncateMiddle(
      "/Users/someone/Library/Application Support/devt-tunnel/tools/cloudflared/1/cloudflared",
      30,
    );
    expect(out).toHaveLength(30);
    expect(out.startsWith("/Users/someone")).toBe(true);
    expect(out.endsWith("cloudflared")).toBe(true);
    expect(out).toContain("…");
  });
});

describe("staleTunnels", () => {
  const tunnels = [
    view("cloudflared", "running", "/old/cloudflared"),
    view("cloudflared", "reconnecting", "/new/cloudflared"),
    view("cloudflared", "stopped", "/old/cloudflared"),
    view("cloudflared", "running", undefined),
    view("ngrok", "running", "/old/ngrok"),
  ];
  it("counts only live tunnels of the tool that run a different executable", () => {
    expect(staleTunnels(tunnels, "cloudflared", "/new/cloudflared")).toHaveLength(1);
    expect(staleTunnels(tunnels, "ngrok", "/old/ngrok")).toHaveLength(0);
    expect(staleTunnels(null, "ssh", "/x")).toHaveLength(0);
  });
  it("words the banner for one and many", () => {
    expect(staleMessage(1, "ngrok")).toBe("1 running tunnel still uses the previous ngrok.");
    expect(staleMessage(2, "ngrok")).toBe("2 running tunnels still use the previous ngrok.");
  });
});

describe("labels and hints", () => {
  it("labels origins", () => {
    expect(originLabel("homebrew")).toBe("Homebrew");
    expect(originLabel("other")).toBe("other");
  });
  it("offers install commands for ngrok and cloudflared", () => {
    expect(installCommands("ngrok", "mac")).toContain("brew install ngrok");
    expect(installCommands("cloudflared", "windows")).toContain("winget install --id Cloudflare.cloudflared");
    expect(platformOf("MacIntel")).toBe("mac");
    expect(platformOf("Win32")).toBe("windows");
    expect(platformOf("Linux x86_64")).toBe("linux");
  });
});

describe("download state", () => {
  const event = (state: ToolDownloadEvent["state"], extra: Partial<ToolDownloadEvent> = {}): ToolDownloadEvent => ({
    tool: "cloudflared",
    version: "2026.9.1",
    state,
    received: 0,
    total: 0,
    ...extra,
  });
  it("tracks progress, keeps failures and drops finished or cancelled downloads", () => {
    let states: DownloadStates = {};
    states = reduceDownloads(states, event("downloading", { received: 5 }));
    expect(states.cloudflared.received).toBe(5);
    states = reduceDownloads(states, event("verifying"));
    expect(states.cloudflared.state).toBe("verifying");
    states = reduceDownloads(states, event("failed", { error: "x" }));
    expect(states.cloudflared.state).toBe("failed");
    expect(reduceDownloads(states, event("done"))).toEqual({});
    expect(reduceDownloads(states, event("cancelled"))).toEqual({});
    expect(reduceDownloads({}, event("done"))).toEqual({});
  });
  it("formats sizes", () => {
    expect(formatSize(38 * 1024 * 1024)).toBe("38 MB");
    expect(progressText(13002342, 38 * 1024 * 1024)).toBe("12.4 / 38 MB");
    expect(progressText(1024 * 1024, 0)).toBe("1 MB");
    expect(progressFraction(5, 10)).toBe(0.5);
    expect(progressFraction(5, 0)).toBeNull();
  });
  it("words errors", () => {
    expect(
      downloadErrorText("download cloudflared: the file did not match the expected checksum and was deleted"),
    ).toBe(CHECKSUM_MESSAGE);
    expect(downloadErrorText("Error: download cloudflared: connection closed after 3 of 9 bytes")).toBe(
      "download cloudflared: connection closed after 3 of 9 bytes",
    );
    expect(downloadErrorText("")).toBe("The download failed.");
    expect(isConflictError("a running tunnel uses cloudflared 1; stop it first")).toBe(true);
    expect(isConflictError("network down")).toBe(false);
  });
});

describe("downloaded paths", () => {
  it("reads the version from the parent directory", () => {
    expect(versionFromPath("/t/tools/cloudflared/2026.9.1/cloudflared")).toBe("2026.9.1");
    expect(versionFromPath("C:\\t\\tools\\cloudflared\\2026.9.1\\cloudflared.exe")).toBe("2026.9.1");
    expect(versionFromPath("cloudflared")).toBe("");
  });
  it("disables remove only while a live tunnel runs that version", () => {
    const path = "/t/tools/cloudflared/2026.9.1/cloudflared";
    expect(removeDisabled([view("cloudflared", "running", path)], path)).toBe(true);
    expect(removeDisabled([view("cloudflared", "running", "/t/tools/cloudflared/2026.9.10/cloudflared")], path)).toBe(
      false,
    );
    expect(removeDisabled([view("cloudflared", "stopped", path)], path)).toBe(false);
    expect(removeDisabled([view("cloudflared", "running", "/opt/homebrew/bin/cloudflared")], path)).toBe(false);
    expect(removeDisabled(null, path)).toBe(false);
    const win = "C:\\t\\tools\\cloudflared\\2026.9.1\\cloudflared.exe";
    expect(removeDisabled([view("cloudflared", "running", win)], win)).toBe(true);
  });
});

describe("navigation and findings", () => {
  it("round-trips the settings parameter", () => {
    expect(parseSettingsParam(binariesParam("cloudflared"))).toEqual({ page: "binaries", item: "cloudflared" });
    expect(parseSettingsParam("vault")).toEqual({ page: "vault" });
    expect(parseSettingsParam(undefined)).toEqual({});
  });
  it("recognizes the missing tool finding", () => {
    const message = "cloudflared was not found on this machine";
    expect(missingToolFromFinding({ severity: "error", message })).toBe("cloudflared");
    expect(missingToolFromFinding({ severity: "ok", message })).toBeNull();
    expect(missingToolFromFinding({ severity: "error", message: "something else" })).toBeNull();
  });
});
