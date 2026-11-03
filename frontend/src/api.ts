// The only module that talks to the Go side. Types come from generated bindings.
import { Events } from "@wailsio/runtime";
import * as Service from "../bindings/github.com/trieu04/devt-tunnel/internal/host/service";
import type { Overview, TunnelView } from "../bindings/github.com/trieu04/devt-tunnel/internal/host";

export type { Overview, TunnelView };
export { Service };

export function onTunnelsChanged(callback: () => void): () => void {
  return Events.On("tunnels:changed", callback);
}

export function onQuickOpened(callback: () => void): () => void {
  return Events.On("quick:opened", callback);
}
