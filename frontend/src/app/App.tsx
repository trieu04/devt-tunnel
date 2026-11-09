import { useEffect, useReducer, useState } from "react";
import { Service, onNavigation, onSettingsChanged } from "../api";
import { isActive } from "../lib/filter";
import { ConfirmDialog } from "../ConfirmDialog";
import { ApprovalDialog, useApprovals } from "../ApprovalDialog";
import { PromptDialog } from "../PromptDialog";
import { QuickPalette } from "../quick/QuickPalette";
import { EditorScreen } from "../screens/EditorScreen";
import { SettingsScreen } from "../screens/SettingsScreen";
import { TunnelsScreen } from "../screens/TunnelsScreen";
import { useOverview } from "../useOverview";
import { shortcutAction } from "./shortcuts";

type Navigation = { screen: "tunnels" | "editor" | "profiles" | "groups" | "settings"; param?: string };

export function App() {
  const { overview, refresh } = useOverview();
  const approvalQueue = useApprovals();
  const [nav, navigate] = useReducer((_state: Navigation, next: Navigation) => next, { screen: "tunnels" });
  const [selected, setSelected] = useState<string | null>(null);
  const [group, setGroup] = useState<string | null>(null);
  const [quick, setQuick] = useState(false);
  const [update, setUpdate] = useState<{ available: boolean; latest: string; url: string } | null>(null);
  const [updateDismissed, setUpdateDismissed] = useState(false);
  const [quitCount, setQuitCount] = useState(0);
  const open = (screen: string, param = "") => {
    if (screen === "tunnels" && param.startsWith("select:")) setSelected(param.slice(7));
    navigate({ screen: screen as Navigation["screen"], param });
  };

  useEffect(() => onNavigation((event) => open(event.data.screen, event.data.param)), []);
  useEffect(() => {
    const check = () => {
      Service.CheckForUpdate(false)
        .then(setUpdate)
        .catch(() => undefined);
    };
    const off = onSettingsChanged(check);
    check();
    return off;
  }, []);
  useEffect(() => {
    const handler = (event: KeyboardEvent) => {
      if (document.querySelector('[aria-modal="true"]')) return;
      const action = shortcutAction(event, navigator.platform.startsWith("Mac"));
      if (!action) return;
      const input =
        event.target instanceof HTMLInputElement ||
        event.target instanceof HTMLTextAreaElement ||
        event.target instanceof HTMLSelectElement;
      if (input && (action === "up" || action === "down")) return;
      if (quick) return;
      event.preventDefault();
      if (action === "new") open("editor");
      if (action === "edit" && selected) open("editor", `edit:${selected}`);
      if (action === "paste") open("editor", "paste");
      if (action === "quick") setQuick(true);
      if (action === "settings") open("settings");
      if (action === "toggle" && selected) Service.Toggle(selected).finally(refresh);
      if (action === "quit") {
        const count = (overview?.tunnels ?? []).filter((view) => isActive(view.status.state)).length;
        if (count) setQuitCount(count);
        else Service.Quit();
      }
    };
    window.addEventListener("keydown", handler);
    return () => window.removeEventListener("keydown", handler);
  }, [overview, selected, refresh, quick]);

  if (!overview) return <div className="loading">Loading devt-tunnel…</div>;
  return (
    <div className="app">
      {update?.available && !updateDismissed && (
        <div className="update-banner">
          <a href={update.url}>devt-tunnel {update.latest} is available</a>
          <button aria-label="Dismiss update notice" onClick={() => setUpdateDismissed(true)}>
            ×
          </button>
        </div>
      )}
      {approvalQueue.error && (
        <div className="banner banner-error" role="alert">
          Cannot load agent approvals: {approvalQueue.error} <button onClick={approvalQueue.refresh}>Retry</button>
        </div>
      )}
      {nav.screen === "tunnels" && (
        <TunnelsScreen
          overview={overview}
          selectedId={selected}
          onSelect={setSelected}
          onNavigate={open}
          refresh={refresh}
          group={group}
          onGroup={setGroup}
        />
      )}
      {nav.screen === "editor" && (
        <EditorScreen
          source={nav.param}
          profiles={overview.profiles ?? []}
          groups={overview.groups ?? []}
          onDone={(id) => {
            if (id) setSelected(id);
            refresh();
            open("tunnels");
          }}
        />
      )}
      {(nav.screen === "settings" || nav.screen === "profiles" || nav.screen === "groups") && (
        <SettingsScreen
          key={`${nav.screen}:${nav.param ?? ""}`}
          overview={overview}
          initial={nav.screen === "settings" ? nav.param : nav.screen}
          item={nav.screen === "settings" ? undefined : nav.param}
          onBack={() => open("tunnels")}
          refresh={refresh}
        />
      )}
      {quick && (
        <div
          className="overlay"
          onMouseDown={(event) => {
            if (event.target === event.currentTarget) setQuick(false);
          }}
        >
          <QuickPalette embedded onClose={() => setQuick(false)} onNavigate={open} />
        </div>
      )}
      {!approvalQueue.approvals.length && (
        <PromptDialog prompts={overview.prompts ?? []} tunnels={overview.tunnels ?? []} />
      )}
      <ApprovalDialog
        key={approvalQueue.approvals[0]?.id ?? "none"}
        approvals={approvalQueue.approvals}
        onDecided={approvalQueue.refresh}
      />
      <ConfirmDialog
        open={quitCount > 0}
        title="Quit devt-tunnel?"
        description={`${quitCount} running ${quitCount === 1 ? "tunnel" : "tunnels"} will be stopped.`}
        confirmLabel="Stop and quit"
        busyLabel="Quitting…"
        tone="primary"
        onCancel={() => setQuitCount(0)}
        onConfirm={() => Service.Quit()}
      />
    </div>
  );
}
