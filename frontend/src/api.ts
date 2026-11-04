// The only module that talks to the Go side. Types come from generated bindings.
import { Browser, Events } from "@wailsio/runtime";
import * as Service from "../bindings/github.com/trieu04/devt-tunnel/internal/host/service";
import type { Overview, TunnelView } from "../bindings/github.com/trieu04/devt-tunnel/internal/api";

export type { Overview, TunnelView };
export { Service };

export function openURL(url: string): Promise<void> {
  if ("_wails" in window) return Browser.OpenURL(url);
  window.open(url, "_blank", "noopener,noreferrer");
  return Promise.resolve();
}

export function onTunnelsChanged(callback: () => void): () => void {
  return Events.On("tunnels:changed", callback);
}

export function onQuickOpened(callback: () => void): () => void {
  return Events.On("quick:opened", callback);
}

export function onLogsAppended(
  callback: (event: { data: { id: string; lines: { seq: number; line: string }[] } }) => void,
): () => void {
  return Events.On("logs:appended", callback);
}

export function onNavigation(callback: (event: { data: { screen: string; param: string } }) => void): () => void {
  return Events.On("nav:open", callback);
}
