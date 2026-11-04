export type ShortcutAction =
  "new" | "edit" | "paste" | "quick" | "toggle" | "up" | "down" | "quit" | "save" | "saveStart" | "settings";

export const SHORTCUTS: { action: ShortcutAction; mac: string; other: string; label: string }[] = [
  { action: "new", mac: "⌘N", other: "Ctrl+N", label: "New tunnel" },
  { action: "edit", mac: "⌘E", other: "Ctrl+E", label: "Edit selected tunnel" },
  { action: "paste", mac: "⌘⇧V", other: "Ctrl+Shift+V", label: "Create from pasted command" },
  { action: "quick", mac: "⌘K", other: "Ctrl+K", label: "Quick launch" },
  { action: "toggle", mac: "⌘↵", other: "Ctrl+Enter", label: "Start or stop selected" },
  { action: "quit", mac: "⌘Q", other: "Ctrl+Q", label: "Stop all and quit" },
  { action: "settings", mac: "⌘,", other: "Ctrl+,", label: "Settings" },
];

export function shortcutAction(
  event: Pick<KeyboardEvent, "key" | "metaKey" | "ctrlKey" | "shiftKey" | "target">,
  mac: boolean,
): ShortcutAction | null {
  const modifier = mac ? event.metaKey : event.ctrlKey;
  const key = event.key.toLowerCase();
  if (!modifier && (key === "arrowup" || key === "arrowdown")) return key === "arrowup" ? "up" : "down";
  if (!modifier) return null;
  if (key === "n") return "new";
  if (key === "e") return "edit";
  if (key === "v" && event.shiftKey) return "paste";
  if (key === "k") return "quick";
  if (key === "enter") return "toggle";
  if (key === "q") return "quit";
  if (key === ",") return "settings";
  if (key === "s") return event.shiftKey ? "saveStart" : "save";
  return null;
}
