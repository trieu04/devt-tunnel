// Per-tool download state shared by every screen. It is driven by the daemon's
// "tool:download" events, so a download started elsewhere (the CLI, another
// window) shows progress here too.
import { useSyncExternalStore } from "react";
import { Service, onToolDownload } from "./api";
import { isCancelError, reduceDownloads, type DownloadStates, type ToolDownloadEvent } from "./lib/binaries";

let states: DownloadStates = {};
const listeners = new Set<() => void>();
const doneListeners = new Set<(tool: string) => void>();
let off: (() => void) | null = null;

function emit() {
  listeners.forEach((listener) => listener());
}

function apply(event: ToolDownloadEvent) {
  states = reduceDownloads(states, event);
  emit();
  if (event.state === "done") doneListeners.forEach((listener) => listener(event.tool));
}

function subscribe(listener: () => void): () => void {
  if (!off) off = onToolDownload(apply);
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
    if (listeners.size === 0 && off) {
      off();
      off = null;
    }
  };
}

/** Download state of one tool: undefined when idle. */
export function useToolDownload(tool: string): ToolDownloadEvent | undefined {
  return useSyncExternalStore(subscribe, () => states)[tool];
}

/** Calls listener when a download of any tool finishes, from anywhere. */
export function onToolDownloaded(listener: (tool: string) => void): () => void {
  doneListeners.add(listener);
  return () => doneListeners.delete(listener);
}

/**
 * Starts a download that also selects the result. Progress arrives through the
 * event stream; the returned promise settles when the daemon is done.
 */
export async function startToolDownload(tool: string, version: string) {
  apply({ tool, version, state: "downloading", received: 0, total: 0 });
  try {
    const tools = await Service.DownloadTool(tool, true);
    if (states[tool] && states[tool].state !== "failed") apply({ tool, version, state: "done", received: 0, total: 0 });
    return tools ?? [];
  } catch (reason) {
    const raw = String(reason);
    if (isCancelError(raw)) apply({ tool, version, state: "cancelled", received: 0, total: 0 });
    else if (states[tool]?.state !== "failed")
      apply({ tool, version, state: "failed", received: 0, total: 0, error: raw });
    return null;
  }
}

export async function cancelToolDownload(tool: string) {
  try {
    await Service.CancelToolDownload(tool);
  } catch {
    // The download may already have finished.
  }
}
