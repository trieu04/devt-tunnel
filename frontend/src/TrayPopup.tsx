import { useEffect, useMemo, useState } from "react";
import type { TunnelView } from "./api";
import { Service } from "./api";
import { isActive, matchesSearch, STATE_LABELS } from "./lib/filter";
import { useOverview } from "./useOverview";

type TrayGroup = { label: string; items: TunnelView[] };

function tunnelDetail(view: TunnelView): string {
  const state = view.status.state;
  if (view.missingSecrets?.length || state === "needs_token") return "Needs token · open app to fix";
  if (state === "keyring_unavailable") return "Keyring unavailable · open app to fix";
  if (state === "failed") return view.status.error || "Failed · open app for details";
  if (state === "starting" || state === "reconnecting") return `${STATE_LABELS[state]}…`;
  if (view.status.address) return view.status.address;

  const tunnel = view.tunnel;
  if (tunnel.type === "ssh") {
    const binds = (tunnel.forwards ?? []).map((forward) => forward.bind).filter(Boolean);
    return binds.join(" · ") || tunnel.ssh?.host || tunnel.ssh?.hostname || "SSH tunnel";
  }
  if (tunnel.type === "ngrok") {
    return [tunnel.ngrok?.addr, tunnel.ngrok?.domain].filter(Boolean).join(" → ") || "ngrok tunnel";
  }
  if (tunnel.type === "cloudflared") {
    return [tunnel.cloudflared?.url, tunnel.cloudflared?.hostname].filter(Boolean).join(" → ") || "Cloudflare tunnel";
  }
  return tunnel.command?.line || "Command tunnel";
}

function stateTone(view: TunnelView): "running" | "error" | "idle" {
  if (isActive(view.status.state)) return "running";
  if (view.missingSecrets?.length || ["failed", "needs_token", "keyring_unavailable"].includes(view.status.state)) {
    return "error";
  }
  return "idle";
}

function requiresAppFix(view: TunnelView): boolean {
  return Boolean(view.missingSecrets?.length) || ["needs_token", "keyring_unavailable"].includes(view.status.state);
}

function shortcutLabel(value: string, mac: boolean): string {
  if (!value) return mac ? "⌥Space" : "Ctrl+Alt+Space";
  return mac ? value.replace("Alt+", "⌥").replace("Ctrl+", "⌃") : value;
}

export function TrayPopup() {
  const { overview, refresh } = useOverview();
  const [query, setQuery] = useState("");
  const [pending, setPending] = useState<string | null>(null);
  const [error, setError] = useState("");
  const mac = navigator.platform.startsWith("Mac");

  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if (!(mac ? event.metaKey : event.ctrlKey)) return;
      if (event.key.toLowerCase() === "o") {
        event.preventDefault();
        Service.ShowMain();
      }
      if (event.key.toLowerCase() === "q") {
        event.preventDefault();
        Service.Quit();
      }
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [mac]);

  const groups = useMemo<TrayGroup[]>(() => {
    if (!overview) return [];
    const normalized = query.trim();
    const inActiveProfile = (view: TunnelView) =>
      !overview.activeProfile || (view.tunnel.profiles ?? []).includes(overview.activeProfile);
    const visible = (view: TunnelView) =>
      !normalized ||
      matchesSearch(view, normalized) ||
      tunnelDetail(view).toLowerCase().includes(normalized.toLowerCase());
    const running = overview.tunnels?.filter((view) => isActive(view.status.state) && visible(view)) ?? [];
    const favorites =
      overview.tunnels?.filter(
        (view) => !isActive(view.status.state) && view.tunnel.favorite && inActiveProfile(view) && visible(view),
      ) ?? [];
    return [
      { label: "Running", items: running },
      { label: "Favorites", items: favorites },
    ].filter((group) => group.items.length > 0);
  }, [overview, query]);

  if (!overview) return <div className="tray-loading">Loading devt-tunnel…</div>;

  const runningCount = overview.tunnels?.filter((view) => isActive(view.status.state)).length ?? 0;
  const activeProfile = overview.profiles?.find((profile) => profile.id === overview.activeProfile)?.name || "All";
  const openTunnel = (view: TunnelView) => {
    const needsFix = requiresAppFix(view);
    Service.OpenIn(needsFix ? "editor" : "tunnels", needsFix ? `edit:${view.tunnel.id}` : `select:${view.tunnel.id}`);
  };
  const toggle = async (view: TunnelView) => {
    setPending(view.tunnel.id);
    setError("");
    try {
      await Service.Toggle(view.tunnel.id);
      refresh();
    } catch (reason) {
      setError(String(reason));
    } finally {
      setPending(null);
    }
  };

  return (
    <main className="tray-popup">
      <header className="tray-header">
        <div className="tray-brand">
          <strong>devt-tunnel</strong>
          <span>{runningCount} running</span>
        </div>
        <span className="tray-profile" title={`Active profile: ${activeProfile}`}>
          {activeProfile}
        </span>
      </header>

      <div className="tray-search">
        <label className="sr" htmlFor="tray-query">
          Filter tunnels
        </label>
        <input
          id="tray-query"
          type="search"
          placeholder="Filter tunnels"
          value={query}
          onChange={(event) => setQuery(event.target.value)}
        />
      </div>

      <section className="tray-list" aria-label="Tray tunnels">
        {groups.map((group) => (
          <div className="tray-group" key={group.label}>
            <h2>{group.label}</h2>
            {group.items.map((view) => {
              const active = isActive(view.status.state);
              const tone = stateTone(view);
              const blocked = requiresAppFix(view);
              return (
                <div className="tray-row" key={view.tunnel.id}>
                  <span className={`tray-dot tray-dot-${tone}`} aria-hidden="true" />
                  <button className="tray-row-main" onClick={() => openTunnel(view)}>
                    <span className="tray-row-name">{view.tunnel.name}</span>
                    <span className={`tray-row-detail ${tone === "error" ? "tray-row-error" : ""}`}>
                      {tunnelDetail(view)}
                    </span>
                  </button>
                  <button
                    className="tray-toggle"
                    type="button"
                    aria-label={`${active ? "Stop" : "Start"} ${view.tunnel.name}`}
                    aria-pressed={active}
                    disabled={pending === view.tunnel.id || blocked}
                    onClick={() => void toggle(view)}
                  >
                    <span className="tray-toggle-track" aria-hidden="true">
                      <span />
                    </span>
                  </button>
                </div>
              );
            })}
          </div>
        ))}
        {!groups.length && (
          <p className="tray-empty">{query ? "No matching tunnels" : "No running or favorite tunnels"}</p>
        )}
        {error && <p className="tray-action-error">{error}</p>}
      </section>

      <footer className="tray-actions">
        <button onClick={() => Service.ShowQuickLaunch()}>
          <span>Quick launch…</span>
          <kbd>{shortcutLabel(overview.shortcut, mac)}</kbd>
        </button>
        <button onClick={() => Service.ShowMain()}>
          <span>Open devt-tunnel</span>
          <kbd>{mac ? "⌘O" : "Ctrl+O"}</kbd>
        </button>
        <button className="tray-quit" onClick={() => Service.Quit()}>
          <span>Stop all &amp; quit</span>
          <kbd>{mac ? "⌘Q" : "Ctrl+Q"}</kbd>
        </button>
      </footer>
    </main>
  );
}
