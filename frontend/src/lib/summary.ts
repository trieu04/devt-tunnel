import type { Tunnel } from "../../bindings/github.com/trieu04/devt-tunnel/internal/core/model/models";

/** Joins one-letter options the way they are usually typed: -N -T -q becomes -NTq. */
export function mergeShortFlags(flags: string[]): string[] {
  const short = flags.filter((flag) => /^-[A-Za-z0-9]$/.test(flag));
  const rest = flags.filter((flag) => !short.includes(flag));
  return short.length ? [`-${short.map((flag) => flag.slice(1)).join("")}`, ...rest] : rest;
}

/** One line saying what a tunnel runs. For display only: the preview is the exact command. */
export function commandSummary(tunnel: Tunnel): string {
  const parts: (string | undefined)[] = [tunnel.type];
  if (tunnel.type === "ssh") {
    const ssh = tunnel.ssh;
    const manual = ssh?.hostname && `${ssh.user ? `${ssh.user}@` : ""}${ssh.hostname}`;
    parts.push(ssh?.host || manual || undefined, ...mergeShortFlags(ssh?.flags ?? []), ssh?.extraArgs);
  } else if (tunnel.type === "ngrok") {
    parts.push(tunnel.ngrok?.proto, tunnel.ngrok?.domain || tunnel.ngrok?.addr);
  } else if (tunnel.type === "cloudflared") {
    parts.push(tunnel.cloudflared?.name || tunnel.cloudflared?.url);
  } else {
    parts.push(tunnel.command?.line);
  }
  return parts.filter(Boolean).join(" ");
}

/**
 * How many leading items fit in the available width, leaving room for a "+N"
 * marker when some do not. Always at least one, so the first can be truncated.
 */
export function fitCount(widths: number[], more: number, available: number, gap: number): number {
  const through = (count: number) => widths.slice(0, count).reduce((sum, width) => sum + width + gap, 0);
  if (through(widths.length) - gap <= available) return widths.length;
  let count = widths.length - 1;
  while (count > 1 && through(count) + more > available) count--;
  return Math.max(count, 1);
}
