import type { TunnelView } from "../../bindings/github.com/trieu04/devt-tunnel/internal/api";

export const TOOLS = ["ssh", "ngrok", "cloudflared"] as const;
export type ToolName = (typeof TOOLS)[number];

const ORIGIN_LABELS: Record<string, string> = {
  path: "PATH",
  homebrew: "Homebrew",
  scoop: "Scoop",
  winget: "WinGet",
  system: "System",
  downloaded: "Downloaded",
  custom: "Custom",
};

export function originLabel(origin: string): string {
  return ORIGIN_LABELS[origin] ?? origin;
}

/** Shortens a path to at most max characters by replacing the middle with an ellipsis. */
export function truncateMiddle(path: string, max = 48): string {
  if (max < 5 || path.length <= max) return path;
  const keep = max - 1;
  const head = Math.ceil(keep / 2);
  const tail = Math.floor(keep / 2);
  return `${path.slice(0, head)}…${path.slice(path.length - tail)}`;
}

const LIVE = new Set(["starting", "running", "reconnecting"]);

/**
 * Running tunnels of this tool type that were started with a different
 * executable than the one now selected. They keep the old one until restarted.
 */
export function staleTunnels(
  tunnels: TunnelView[] | null | undefined,
  tool: string,
  selectedPath: string,
): TunnelView[] {
  return (tunnels ?? []).filter(
    (view) =>
      view.tunnel.type === tool &&
      LIVE.has(view.status.state) &&
      !!view.status.toolPath &&
      view.status.toolPath !== selectedPath,
  );
}

export function staleMessage(count: number, tool: string): string {
  return `${count} running ${count === 1 ? "tunnel still uses" : "tunnels still use"} the previous ${tool}.`;
}

export type Platform = "mac" | "windows" | "linux";

export function platformOf(navigatorPlatform: string): Platform {
  if (navigatorPlatform.startsWith("Mac")) return "mac";
  if (navigatorPlatform.startsWith("Win")) return "windows";
  return "linux";
}

/** Commands shown, with a copy button, when a tool is not installed. */
export function installCommands(tool: string, platform: Platform): string[] {
  switch (tool) {
    case "ngrok":
      return ["brew install ngrok", "winget install ngrok.ngrok", "snap install ngrok"];
    case "cloudflared":
      return ["brew install cloudflared", "winget install --id Cloudflare.cloudflared"];
    default:
      return platform === "windows" ? [] : platform === "mac" ? [] : ["sudo apt install openssh-client"];
  }
}

export function sshInstallHelp(platform: Platform): string {
  switch (platform) {
    case "windows":
      return "Install it from Settings → Optional features → OpenSSH Client.";
    case "mac":
      return "OpenSSH ships with macOS. Reinstall the Xcode command line tools or choose a file below.";
    default:
      return "Install your distribution's openssh-client package.";
  }
}

/** Payload of the "tool:download" event. Not part of the generated bindings. */
export type ToolDownloadEvent = {
  tool: string;
  version: string;
  state: "downloading" | "verifying" | "done" | "failed" | "cancelled";
  received: number;
  total: number;
  error?: string;
};

/** In-flight or failed downloads by tool. Finished and cancelled downloads are absent. */
export type DownloadStates = Record<string, ToolDownloadEvent>;

export function reduceDownloads(states: DownloadStates, event: ToolDownloadEvent): DownloadStates {
  if (event.state === "done" || event.state === "cancelled") {
    if (!(event.tool in states)) return states;
    const { [event.tool]: _gone, ...rest } = states;
    return rest;
  }
  return { ...states, [event.tool]: event };
}

const MB = 1024 * 1024;

function megabytes(bytes: number, fraction: number): string {
  const text = (bytes / MB).toFixed(fraction);
  return fraction > 0 ? text.replace(/\.0+$/, "") : text;
}

/** "38 MB" */
export function formatSize(bytes: number): string {
  return `${megabytes(bytes, 0)} MB`;
}

/** "12.4 / 38 MB"; without a known total, just "12.4 MB". */
export function progressText(received: number, total: number): string {
  if (total <= 0) return `${megabytes(received, 1)} MB`;
  return `${megabytes(received, 1)} / ${megabytes(total, total % MB === 0 ? 0 : 1)} MB`;
}

/** Fraction 0..1 for a progress bar, or null when the total is unknown. */
export function progressFraction(received: number, total: number): number | null {
  if (total <= 0) return null;
  return Math.min(1, Math.max(0, received / total));
}

export const CHECKSUM_MESSAGE = "The download didn't match the expected checksum and was deleted.";

/** Turns a Service error string into text for the download row. */
export function downloadErrorText(raw: string | undefined): string {
  const text = (raw ?? "").replace(/^(Error:\s*)+/, "").trim();
  if (/checksum|archive:|larger than the expected|announced \d+ bytes|does not report version/i.test(text)) {
    return CHECKSUM_MESSAGE;
  }
  return text || "The download failed.";
}

export function isConflictError(raw: string): boolean {
  return /in progress|running tunnel uses/i.test(raw);
}

export function isCancelError(raw: string): boolean {
  return /cancel/i.test(raw);
}

/** Version of a downloaded binary: the name of its parent directory. */
export function versionFromPath(path: string): string {
  const parts = path.split(/[\\/]/).filter(Boolean);
  return parts.length >= 2 ? parts[parts.length - 2] : "";
}

function normalize(path: string): string {
  return path.replace(/\\/g, "/");
}

/** Whether path lies inside dir (the directory itself does not count). */
export function isInside(path: string, dir: string): boolean {
  const base = normalize(dir).replace(/\/+$/, "");
  return !!base && normalize(path).startsWith(`${base}/`);
}

/**
 * Running tunnels whose launched binary is inside the version directory of the
 * downloaded executable at path. Removing that version is refused while any exist.
 */
export function tunnelsUsingDownload(tunnels: TunnelView[] | null | undefined, path: string): TunnelView[] {
  const dir = normalize(path).replace(/\/[^/]*$/, "");
  return (tunnels ?? []).filter(
    (view) => LIVE.has(view.status.state) && !!view.status.toolPath && isInside(view.status.toolPath, dir),
  );
}

export function removeDisabled(tunnels: TunnelView[] | null | undefined, path: string): boolean {
  return tunnelsUsingDownload(tunnels, path).length > 0;
}

/** Settings navigation parameter that opens the Binaries page on a tool's card. */
export function binariesParam(tool: string): string {
  return `binaries:${tool}`;
}

/** Splits a settings navigation parameter into page and focused item. */
export function parseSettingsParam(param: string | undefined): { page?: string; item?: string } {
  if (!param) return {};
  const index = param.indexOf(":");
  return index < 0 ? { page: param } : { page: param.slice(0, index), item: param.slice(index + 1) };
}

/** Matches the editor check that reports a missing tool; returns the tool name. */
export function missingToolFromFinding(finding: { severity: string; message: string }): string | null {
  if (finding.severity !== "error") return null;
  const match = /^(\S+) was not found on this machine$/.exec(finding.message);
  return match ? match[1] : null;
}
