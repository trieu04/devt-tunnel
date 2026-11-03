import { useEffect, useMemo, useState } from "react";
import { Service, TunnelView } from "./api";
import { TypeFilter, ViewFilter, filterTunnels, isActive } from "./filter";
import { ForwardLine, StateBadge, TunnelGlyph } from "./parts";
import { PromptDialog } from "./PromptDialog";
import { useOverview } from "./useOverview";

const VIEWS: [ViewFilter, string][] = [
  ["all", "All"],
  ["running", "Running"],
  ["favorites", "Favorites"],
];
const TYPES: [TypeFilter, string][] = [
  ["ssh", "SSH"],
  ["cloudflared", "Cloudflared"],
  ["ngrok", "ngrok"],
  ["command", "Command"],
];

export function MainWindow() {
  const { overview, refresh } = useOverview();
  const [query, setQuery] = useState("");
  const [view, setView] = useState<ViewFilter>("all");
  const [type, setType] = useState<TypeFilter>("all");
  const [profile, setProfile] = useState("all");
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [actionError, setActionError] = useState("");

  const tunnels = overview?.tunnels ?? [];
  const visible = useMemo(
    () => filterTunnels(tunnels, { query, view, type, profile }),
    [tunnels, query, view, type, profile],
  );
  const selected = tunnels.find((t) => t.tunnel.id === selectedId) ?? visible[0] ?? null;

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "k") {
        e.preventDefault();
        document.getElementById("search")?.focus();
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  const run = (p: Promise<unknown>) => {
    setActionError("");
    p.catch((e) => setActionError(String(e?.message ?? e))).finally(refresh);
  };

  if (!overview) return null;
  const count = (f: (v: TunnelView) => boolean) => tunnels.filter(f).length;

  return (
    <div className="app">
      <header className="topbar">
        <div className="brand">
          <span className="logo">
            <TunnelGlyph />
          </span>
          <span>devt-tunnel</span>
        </div>
        <div className="search">
          <label htmlFor="search" className="sr">
            Search tunnels
          </label>
          <input
            id="search"
            type="search"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Search name, host or port"
          />
          <kbd>{navigator.platform.startsWith("Mac") ? "⌘K" : "Ctrl+K"}</kbd>
        </div>
        <div className="spacer" />
        <label htmlFor="profile" className="muted">
          Profile
        </label>
        <select id="profile" value={profile} onChange={(e) => setProfile(e.target.value)}>
          <option value="all">All profiles</option>
          {(overview.profiles ?? []).map((p) => (
            <option key={p.id} value={p.id}>
              {p.name}
            </option>
          ))}
        </select>
        <button type="button" onClick={() => run(Service.Reload())}>
          Reload
        </button>
        <button type="button" className="primary" onClick={() => run(Service.RevealConfig())}>
          Edit tunnels.toml
        </button>
      </header>

      {overview.configError && (
        <div className="banner banner-error" role="alert">
          Could not load tunnels.toml, so the last good definitions are still in use: {overview.configError}
        </div>
      )}
      {overview.shortcutError && (
        <div className="banner" role="status">
          Global shortcut {overview.shortcut} is not available: {overview.shortcutError}. Use the tray or this window instead.
        </div>
      )}
      {(overview.orphans ?? []).map((o) => (
        <div className="banner" role="status" key={o.pid}>
          <span>
            Tunnel “{o.tunnelId}” is still running from a previous session (PID {o.pid}).
          </span>
          <button type="button" onClick={() => run(Service.StopOrphan(o.pid))}>
            Stop it
          </button>
        </div>
      ))}
      {actionError && (
        <div className="banner banner-error" role="alert">
          {actionError}
        </div>
      )}

      <div className="body">
        <nav className="sidebar" aria-label="Filters">
          <h2>View</h2>
          {VIEWS.map(([key, label]) => (
            <NavButton
              key={key}
              label={label}
              pressed={view === key}
              count={count((v) =>
                key === "all" ? true : key === "running" ? isActive(v.status.state) : !!v.tunnel.favorite,
              )}
              onClick={() => setView(key)}
            />
          ))}
          <h2>Type</h2>
          {TYPES.map(([key, label]) => (
            <NavButton
              key={key}
              label={label}
              pressed={type === key}
              count={count((v) => v.tunnel.type === key)}
              onClick={() => setType(type === key ? "all" : key)}
            />
          ))}
        </nav>

        <main className="list">
          {tunnels.length === 0 ? (
            <div className="empty">
              <h1>No tunnels yet</h1>
              <p className="muted">
                Definitions live in <code className="mono">{overview.configPath}</code>. The tunnel editor is not built yet, so add
                them to this file and press Reload.
              </p>
              <button type="button" className="primary" onClick={() => run(Service.RevealConfig())}>
                Create tunnels.toml
              </button>
            </div>
          ) : visible.length === 0 ? (
            <div className="empty">
              <p className="muted">No tunnels match these filters.</p>
            </div>
          ) : (
            visible.map((v) => (
              <TunnelRow
                key={v.tunnel.id}
                view={v}
                selected={selected?.tunnel.id === v.tunnel.id}
                onSelect={() => setSelectedId(v.tunnel.id)}
                onToggle={() => run(Service.Toggle(v.tunnel.id))}
              />
            ))
          )}
        </main>

        {selected && <Detail view={selected} onAction={run} />}
      </div>
      <PromptDialog prompts={overview.prompts ?? []} tunnels={tunnels} />
    </div>
  );
}

function NavButton(props: { label: string; pressed: boolean; count: number; onClick: () => void }) {
  return (
    <button type="button" className="nav" aria-pressed={props.pressed} onClick={props.onClick}>
      <span>{props.label}</span>
      <span className="muted">{props.count}</span>
    </button>
  );
}

function TunnelRow(props: { view: TunnelView; selected: boolean; onSelect: () => void; onToggle: () => void }) {
  const { tunnel, status } = props.view;
  return (
    <div className={`row ${props.selected ? "row-selected" : ""}`}>
      <button type="button" className="row-main" onClick={props.onSelect}>
        <div className="row-title">
          <span className="name">{tunnel.name}</span>
          <span className="badge">{tunnel.type}</span>
          {tunnel.favorite && <span className="muted">★</span>}
        </div>
        {(tunnel.forwards ?? []).map((f) => (
          <ForwardLine key={f.kind + f.bind} forward={f} />
        ))}
        {status.address && !(tunnel.forwards ?? []).length && <div className="mono muted">{status.address}</div>}
      </button>
      <StateBadge state={status.state} />
      <button type="button" onClick={props.onToggle} disabled={status.state === "keyring_unavailable"}>
        {isActive(status.state) ? "Stop" : "Start"}
      </button>
    </div>
  );
}

function Detail({ view, onAction }: { view: TunnelView; onAction: (p: Promise<unknown>) => void }) {
  const { tunnel, status, preview } = view;
  const [logs, setLogs] = useState<string[]>([]);
  const [findings, setFindings] = useState<{ severity: string; message: string }[]>([]);
  const [secretValue, setSecretValue] = useState("");

  useEffect(() => {
    let alive = true;
    const load = () =>
      Service.Logs(tunnel.id).then((lines) => {
        if (alive) setLogs(lines ?? []);
      });
    load();
    const timer = setInterval(load, 1000);
    return () => {
      alive = false;
      clearInterval(timer);
    };
  }, [tunnel.id]);

  useEffect(() => {
    Service.Findings(tunnel.id).then((f) => setFindings(f ?? []));
  }, [tunnel.id, status.state]);

  const missing = secretName(view);

  return (
    <aside className="detail" aria-label="Tunnel details">
      <div className="detail-head">
        <h1>{tunnel.name}</h1>
        <StateBadge state={status.state} />
      </div>
      {status.error && <p className="error-text">{status.error}</p>}
      {status.address && (
        <div className="address">
          <code className="mono">{status.address}</code>
          <button type="button" onClick={() => navigator.clipboard.writeText(status.address ?? "")}>
            Copy
          </button>
        </div>
      )}
      {status.state === "needs_token" && missing && (
        <form
          className="secret-form"
          onSubmit={(e) => {
            e.preventDefault();
            onAction(Service.SetSecret(missing, secretValue).then(() => setSecretValue("")));
          }}
        >
          <label htmlFor="secret-value">Value for secret “{missing}”, saved in the OS keychain</label>
          <div className="address">
            <input id="secret-value" type="password" value={secretValue} onChange={(e) => setSecretValue(e.target.value)} />
            <button type="submit" disabled={!secretValue}>
              Save
            </button>
          </div>
        </form>
      )}
      <h2>Command</h2>
      <pre className="mono command">{preview}</pre>
      {findings.length > 0 && (
        <>
          <h2>Checks</h2>
          <ul className="findings">
            {findings.map((f) => (
              <li key={f.message} className={f.severity === "error" ? "error-text" : "muted"}>
                {f.message}
              </li>
            ))}
          </ul>
        </>
      )}
      {status.pid ? (
        <p className="muted">PID {status.pid}</p>
      ) : null}
      <h2>Log</h2>
      <pre className="mono log">{logs.join("\n") || "No output yet."}</pre>
    </aside>
  );
}

function secretName({ tunnel }: TunnelView): string | undefined {
  return (
    tunnel.ssh?.passphraseSecret ||
    tunnel.ngrok?.secret ||
    tunnel.cloudflared?.secret ||
    Object.values(tunnel.command?.secretEnvs ?? {})[0]
  );
}
