import { useEffect, useMemo, useState } from "react";
import { Overview, Service, TunnelView, onLogsAppended, openURL } from "../api";
import { applyLogUpdate } from "../lib/logs";
import { TypeFilter, ViewFilter, filterTunnels, isActive } from "../lib/filter";
import {
  ChevronGlyph,
  CopyGlyph,
  KeyGlyph,
  ListGlyph,
  PlayGlyph,
  PlusGlyph,
  SearchGlyph,
  StateBadge,
  StopGlyph,
  TunnelGlyph,
} from "../parts";

export function TunnelsScreen({
  overview,
  selectedId,
  onSelect,
  onNavigate,
  refresh,
}: {
  overview: Overview;
  selectedId: string | null;
  onSelect: (id: string | null) => void;
  onNavigate: (screen: string, param?: string) => void;
  refresh: () => void;
}) {
  const [query, setQuery] = useState("");
  const [view, setView] = useState<ViewFilter>("all");
  const [type, setType] = useState<TypeFilter>("all");
  const [error, setError] = useState("");
  const [now, setNow] = useState(Date.now());
  const [sshInfo, setSSHInfo] = useState<{ path: string; hosts: { alias: string }[] }>({
    path: "~/.ssh/config",
    hosts: [],
  });
  const tunnels = overview.tunnels ?? [];
  const profileNames = useMemo(
    () => new Map((overview.profiles ?? []).map((profile) => [profile.id, profile.name])),
    [overview.profiles],
  );
  const profileTunnels = overview.activeProfile
    ? tunnels.filter((item) => (item.tunnel.profiles ?? []).includes(overview.activeProfile))
    : tunnels;
  const visible = useMemo(
    () => filterTunnels(tunnels, { query, view, type, profile: overview.activeProfile || "all" }),
    [tunnels, query, view, type, overview.activeProfile],
  );
  const selected = visible.find((item) => item.tunnel.id === selectedId) ?? visible[0] ?? null;
  const running = tunnels.filter((item) => isActive(item.status.state)).length;
  const favorites = tunnels.filter((item) => item.tunnel.favorite).length;
  const activeOutside = tunnels.filter(
    (item) =>
      isActive(item.status.state) &&
      overview.activeProfile &&
      !(item.tunnel.profiles ?? []).includes(overview.activeProfile),
  ).length;

  useEffect(() => {
    if ((selected?.tunnel.id ?? null) !== selectedId) onSelect(selected?.tunnel.id ?? null);
  }, [selectedId, selected, onSelect]);
  useEffect(() => {
    const timer = window.setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(timer);
  }, []);
  useEffect(() => {
    Service.SSHHosts(overview.activeProfile)
      .then((result) => setSSHInfo({ path: result.configPath || "~/.ssh/config", hosts: result.hosts ?? [] }))
      .catch(() => undefined);
  }, [overview.activeProfile]);
  useEffect(() => {
    const moveSelection = (event: KeyboardEvent) => {
      if (event.key !== "ArrowUp" && event.key !== "ArrowDown") return;
      if (
        event.target instanceof HTMLInputElement ||
        event.target instanceof HTMLTextAreaElement ||
        event.target instanceof HTMLSelectElement ||
        (event.target instanceof HTMLElement && event.target.isContentEditable)
      )
        return;
      event.preventDefault();
      const current = visible.findIndex((item) => item.tunnel.id === selected?.tunnel.id);
      const next = Math.max(0, Math.min(visible.length - 1, current + (event.key === "ArrowDown" ? 1 : -1)));
      onSelect(visible[next]?.tunnel.id ?? null);
    };
    window.addEventListener("keydown", moveSelection);
    return () => window.removeEventListener("keydown", moveSelection);
  }, [visible, selected, onSelect]);

  const run = (operation: Promise<unknown>) => {
    setError("");
    operation.catch((reason) => setError(String(reason))).finally(refresh);
  };
  const profileLabel = overview.activeProfile ? (profileNames.get(overview.activeProfile) ?? "Profile") : "All tunnels";

  return (
    <div className="tunnels-layout">
      <header className="topbar">
        <div className="brand">
          <span className="logo">
            <TunnelGlyph />
          </span>
          <span>devt-tunnel</span>
        </div>
        <div className="search">
          <SearchGlyph />
          <label className="sr" htmlFor="search">
            Search tunnels
          </label>
          <input
            id="search"
            type="search"
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            placeholder="Search name, host or port"
          />
          <kbd>⌘K</kbd>
        </div>
        <div className="topbar-spacer" />
        <label className="profile-picker">
          <span>Profile</span>
          <select
            aria-label="Active profile"
            value={overview.activeProfile}
            onChange={(event) => run(Service.ActivateProfile(event.target.value))}
          >
            <option value="">All profiles</option>
            {(overview.profiles ?? []).map((profile) => (
              <option key={profile.id} value={profile.id}>
                {profile.name}
              </option>
            ))}
          </select>
        </label>
        <button className="primary new-tunnel" onClick={() => onNavigate("editor")}>
          <PlusGlyph />
          New tunnel
        </button>
      </header>
      {overview.configError && <div className="banner banner-error">{overview.configError}</div>}
      {error && <div className="banner banner-error">{error}</div>}
      {activeOutside > 0 && (
        <button className="banner profile-notice" onClick={() => run(Service.ActivateProfile(""))}>
          {activeOutside} running in other profiles · Show all
        </button>
      )}
      <div className="body tunnel-workspace">
        <nav className="sidebar" aria-label="Filters">
          <div className="sidebar-group">
            <h2>View</h2>
            {(["all", "running", "favorites"] as ViewFilter[]).map((value) => (
              <button className="nav" key={value} aria-pressed={view === value} onClick={() => setView(value)}>
                <span>{capitalize(value)}</span>
                <span className="nav-count">
                  {value === "all"
                    ? profileTunnels.length
                    : value === "running"
                      ? profileTunnels.filter((item) => isActive(item.status.state)).length
                      : profileTunnels.filter((item) => item.tunnel.favorite).length}
                </span>
              </button>
            ))}
          </div>
          <div className="sidebar-group">
            <h2>Type</h2>
            {(["ssh", "cloudflared", "ngrok", "command"] as TypeFilter[]).map((value) => (
              <button
                className="nav"
                key={value}
                aria-pressed={type === value}
                onClick={() => setType(type === value ? "all" : value)}
              >
                <span>{value === "ssh" ? "SSH" : capitalize(value)}</span>
                <span className="nav-count">{profileTunnels.filter((item) => item.tunnel.type === value).length}</span>
              </button>
            ))}
          </div>
          <div className="sidebar-manage">
            <button className="nav" onClick={() => onNavigate("settings", "vault")}>
              <span>
                <KeyGlyph />
                Vault & keys
              </span>
            </button>
            <button className="nav" onClick={() => onNavigate("profiles")}>
              <span>
                <ListGlyph />
                Profiles
              </span>
            </button>
            <button className="nav" onClick={() => onNavigate("settings")}>
              <span>Settings</span>
            </button>
          </div>
        </nav>
        <main className="tunnel-list" role="list">
          <div className="list-heading">
            <h1>{profileLabel}</h1>
            <span>
              {visible.length} shown · {running} running{favorites ? ` · ${favorites} favorites` : ""}
            </span>
          </div>
          {visible.length === 0 ? (
            <Empty
              query={query}
              hasTunnels={tunnels.length > 0}
              matchingHost={
                sshInfo.hosts.find((host) => host.alias.toLowerCase().includes(query.trim().toLowerCase()))?.alias
              }
              onNavigate={onNavigate}
            />
          ) : (
            visible.map((item) => (
              <TunnelRow
                key={item.tunnel.id}
                view={item}
                selected={selected?.tunnel.id === item.tunnel.id}
                profileNames={profileNames}
                now={now}
                onSelect={() => onSelect(item.tunnel.id)}
                onToggle={() => run(Service.Toggle(item.tunnel.id))}
                onFavorite={() => run(Service.SetFavorite(item.tunnel.id, !item.tunnel.favorite))}
                onVault={() => onNavigate("settings", "vault")}
                onEdit={() => onNavigate("editor", `edit:${item.tunnel.id}`)}
              />
            ))
          )}
        </main>
        {selected && <Detail view={selected} profileNames={profileNames} now={now} onNavigate={onNavigate} run={run} />}
      </div>
      <footer className="statusbar">
        <span>{running} running</span>
        <span>
          OpenSSH · <span className="mono">{sshInfo.path}</span> · {sshInfo.hosts.length} hosts
        </span>
        <span>Secrets in {overview.keychainName || "OS keychain"}</span>
        <span className="status-shortcuts">⌘K quick launch · ⌘N new · ⌘E edit · ⌘↵ start/stop</span>
      </footer>
    </div>
  );
}

function Empty({
  query,
  hasTunnels,
  matchingHost,
  onNavigate,
}: {
  query: string;
  hasTunnels: boolean;
  matchingHost?: string;
  onNavigate: (screen: string, param?: string) => void;
}) {
  return (
    <div className="empty">
      <h1>{hasTunnels ? `No tunnel matches “${query}”` : "No tunnels yet"}</h1>
      <p className="muted">
        {hasTunnels
          ? "Search covers names, SSH hosts and ports."
          : "Create one from an SSH host, a pasted command or manual settings."}
      </p>
      <div className="actions">
        {matchingHost && (
          <button className="primary" onClick={() => onNavigate("editor", `host:${matchingHost}`)}>
            Create from {matchingHost}
          </button>
        )}
        <button
          className={matchingHost ? "" : "primary"}
          onClick={() => onNavigate("editor", query ? `name:${query}` : "")}
        >
          {query ? `Create tunnel for “${query}”` : "New tunnel"}
        </button>
        <button onClick={() => onNavigate("editor", "paste")}>Paste command</button>
      </div>
    </div>
  );
}

function TunnelRow({
  view,
  selected,
  profileNames,
  now,
  onSelect,
  onToggle,
  onFavorite,
  onVault,
  onEdit,
}: {
  view: TunnelView;
  selected: boolean;
  profileNames: Map<string, string>;
  now: number;
  onSelect: () => void;
  onToggle: () => void;
  onFavorite: () => void;
  onVault: () => void;
  onEdit: () => void;
}) {
  const forwards = displayForwards(view);
  const [expanded, setExpanded] = useState(forwards.length > 0 || !!view.status.error);
  const active = isActive(view.status.state);
  const subtitle = tunnelSubtitle(view);
  const flags = view.tunnel.type === "ssh" ? (view.tunnel.ssh?.flags ?? []) : [];
  return (
    <article
      role="listitem"
      className={`tunnel-card ${selected ? "tunnel-card-selected" : ""} ${(expanded && forwards.length > 0) || view.status.error ? "tunnel-card-with-children" : ""} ${view.status.error ? "tunnel-card-with-error" : ""}`}
    >
      <div className="tunnel-row" onClick={onSelect}>
        <button
          className="row-expand icon-button"
          onClick={() => setExpanded(!expanded)}
          aria-label={`${expanded ? "Collapse" : "Expand"} ${view.tunnel.name}`}
          aria-expanded={expanded}
        >
          <ChevronGlyph expanded={expanded} />
        </button>
        <span className={`status-dot status-dot-${view.status.state}`} />
        <button className="row-main">
          <span className="row-title">
            <span className="name">{view.tunnel.name}</span>
            <span className="row-subtitle">{subtitle}</span>
            {flags.map((flag) => (
              <span className="flag" key={flag}>
                {flag}
              </span>
            ))}
            {view.status.stale && <span className="stale-pill">Restart to apply changes</span>}
          </span>
        </button>
        <div className="row-profiles">
          {(view.tunnel.profiles ?? []).map((profile) => (
            <span className="profile-chip" key={profile}>
              {profileNames.get(profile) ?? profile}
            </span>
          ))}
        </div>
        <span className="row-state">
          <StateBadge state={view.status.state} />
          {view.status.state !== "starting" && active && view.status.startedAt && (
            <span> · {formatUptime(view.status.startedAt, now)}</span>
          )}
        </span>
        <button
          className={`favorite-action ${view.tunnel.favorite ? "favorite-on" : ""}`}
          aria-label={`${view.tunnel.favorite ? "Remove from" : "Add to"} favorites`}
          onClick={onFavorite}
        >
          {view.tunnel.favorite ? "★" : "☆"}
        </button>
        <button
          className={`row-action ${active ? "row-action-stop" : "row-action-start"}`}
          onClick={onToggle}
          aria-label={`${active ? "Stop" : "Start"} ${view.tunnel.name}`}
          disabled={view.status.state === "keyring_unavailable"}
        >
          {active ? <StopGlyph /> : <PlayGlyph />}
        </button>
      </div>
      {expanded && forwards.length > 0 && (
        <div className="forward-list">
          {forwards.map((forward, index) => (
            <div className="forward-row" key={`${forward.kind}:${forward.bind}:${index}`}>
              <span className="forward-kind" title={forward.hint}>
                {forward.kind}
              </span>
              <ForwardEndpoint
                value={forward.bind}
                label={forwardLabel(forward.kind, forward.bind)}
                href={forward.bindHref}
                className="forward-bind"
              />
              <span className="forward-arrow">→</span>
              <ForwardEndpoint
                value={forward.target}
                label={forwardLabel(forward.kind, forward.target)}
                href={forward.targetHref}
                className="forward-target"
              />
              <button
                className="icon-button"
                aria-label={`Copy ${forward.copy}`}
                onClick={() => navigator.clipboard.writeText(forward.copy)}
              >
                <CopyGlyph />
              </button>
            </div>
          ))}
        </div>
      )}
      {view.status.error && (
        <div className="row-error">
          <span>{view.status.error}</span>
          {view.status.state === "needs_token" ? (
            <button onClick={onVault}>Add token in Vault</button>
          ) : (
            <button onClick={onEdit}>Edit tunnel</button>
          )}
        </div>
      )}
    </article>
  );
}

function Detail({
  view,
  profileNames,
  now,
  onNavigate,
  run,
}: {
  view: TunnelView;
  profileNames: Map<string, string>;
  now: number;
  onNavigate: (screen: string, param?: string) => void;
  run: (operation: Promise<unknown>) => void;
}) {
  const [logs, setLogs] = useState<{ seq: number; line: string }[]>([]);
  const active = isActive(view.status.state);
  useEffect(() => {
    let mounted = true;
    let initialized = false;
    let pending: { lines: { seq: number; line: string }[]; replace?: boolean }[] = [];
    Service.WatchLogs(view.tunnel.id).then((initial) => {
      if (!mounted) return;
      const next = pending.reduce(
        (current, update) => applyLogUpdate(current, update.lines, update.replace),
        initial ?? [],
      );
      initialized = true;
      pending = [];
      setLogs(next);
    });
    const off = onLogsAppended((event) => {
      if (event.data.id === view.tunnel.id) {
        if (!initialized) {
          pending.push(event.data);
          return;
        }
        setLogs((current) => applyLogUpdate(current, event.data.lines, event.data.replace));
      }
    });
    return () => {
      mounted = false;
      off();
      Service.WatchLogs("");
    };
  }, [view.tunnel.id]);
  const facts = [
    { key: "Source", value: tunnelSubtitle(view) },
    { key: "Profiles", value: view.tunnel.profiles?.map((id) => profileNames.get(id) ?? id).join(", ") || "All" },
    { key: "Forwards", value: String(displayForwards(view).length) },
    { key: "Auto-reconnect", value: view.tunnel.reconnect ? "On" : "Off" },
    { key: "PID", value: view.status.pid ? String(view.status.pid) : "—" },
    {
      key: "Uptime",
      value:
        view.status.state !== "starting" && active && view.status.startedAt
          ? formatUptime(view.status.startedAt, now)
          : "—",
    },
    { key: "Tool", value: view.toolPath || "Not found" },
  ];
  return (
    <aside className="detail" aria-label="Tunnel details">
      <div className="detail-head">
        <div>
          <span className="eyebrow">{view.tunnel.type}</span>
          <h2>{view.tunnel.name}</h2>
        </div>
        <span className={`state-pill state-pill-${view.status.state}`}>
          <StateBadge state={view.status.state} />
        </span>
      </div>
      {view.status.error && <p className="error-text">{view.status.error}</p>}
      {view.status.address && (
        <div className="address">
          <code>{view.status.address}</code>
          <button onClick={() => navigator.clipboard.writeText(view.status.address ?? "")}>Copy</button>
        </div>
      )}
      <section className="detail-section">
        <div className="section-line">
          <span className="field-label">Command</span>
          <button className="small-button" onClick={() => navigator.clipboard.writeText(view.preview)}>
            Copy
          </button>
        </div>
        <pre className="command mono command-dark">{view.preview}</pre>
      </section>
      <dl className="detail-facts">
        {facts.map((fact) => (
          <div key={fact.key}>
            <dt>{fact.key}</dt>
            <dd className={fact.key === "Tool" || fact.key === "PID" ? "mono" : ""}>{fact.value}</dd>
          </div>
        ))}
      </dl>
      {(view.missingSecrets ?? []).map((name) => (
        <button key={name} onClick={() => onNavigate("settings", "vault")}>
          Add {name} in Vault
        </button>
      ))}
      <section className="detail-section detail-log">
        <div className="section-line">
          <span className="field-label">Log</span>
          <div>
            <button
              className="small-button"
              onClick={() => navigator.clipboard.writeText(logs.map((line) => line.line).join("\n"))}
            >
              Copy
            </button>
            <button className="small-button" onClick={() => setLogs([])}>
              Clear
            </button>
          </div>
        </div>
        <pre className="log mono">{logs.map((line) => line.line).join("\n") || "No output yet."}</pre>
      </section>
      <div className="detail-actions">
        <button className={active ? "" : "primary"} onClick={() => run(Service.Toggle(view.tunnel.id))}>
          {active ? "Stop" : "Start"}
        </button>
        <button onClick={() => onNavigate("editor", `edit:${view.tunnel.id}`)}>Edit</button>
        <button onClick={() => onNavigate("editor", `duplicate:${view.tunnel.id}`)}>Duplicate</button>
        {view.status.stale && <button onClick={() => run(Service.Restart(view.tunnel.id))}>Restart</button>}
      </div>
    </aside>
  );
}

function tunnelSubtitle(view: TunnelView): string {
  if (view.tunnel.type === "ssh") return `SSH · ${view.tunnel.ssh?.host || view.tunnel.ssh?.hostname || "manual host"}`;
  if (view.tunnel.type === "ngrok")
    return `ngrok · ${view.tunnel.ngrok?.domain || view.tunnel.ngrok?.addr || "endpoint"}`;
  if (view.tunnel.type === "cloudflared")
    return `Cloudflared · ${view.tunnel.cloudflared?.name || view.tunnel.cloudflared?.url || "quick tunnel"}`;
  return "Command · custom command";
}

type DisplayForward = {
  kind: string;
  bind: string;
  target: string;
  hint: string;
  copy: string;
  bindHref?: string;
  targetHref?: string;
};

function displayForwards(view: TunnelView): DisplayForward[] {
  const tunnel = view.tunnel;
  if (tunnel.type === "ssh")
    return (tunnel.forwards ?? []).map((forward) => ({
      kind: forward.kind === "L" || forward.kind === "R" ? `-${forward.kind}` : "SOCKS",
      bind: forward.bind,
      target: forward.target || "SOCKS5",
      hint:
        forward.kind === "R"
          ? "Remote forward: the SSH host listens, traffic reaches the target from here"
          : forward.kind === "D"
            ? "SOCKS5 proxy through the SSH host"
            : "Local forward: listens here, reaches the target through the tunnel",
      copy: forward.bind,
      bindHref: forward.kind === "L" ? localWebURL(forward.bind) : undefined,
      targetHref: forward.kind === "R" ? localWebURL(forward.target || "") : undefined,
    }));
  if (tunnel.type === "cloudflared" && tunnel.cloudflared) {
    const source = tunnel.cloudflared.url || "Local service";
    const target = tunnel.cloudflared.hostname || view.status.address || "Public URL";
    const kind = protocolLabel(source, "HTTP");
    return [
      {
        kind,
        bind: source,
        target,
        hint: tunnel.cloudflared.name ? `Named tunnel ${tunnel.cloudflared.name}` : "Quick tunnel",
        copy: source,
        bindHref: inferredWebURL(source, "http", true),
        targetHref: inferredWebURL(target, "https"),
      },
    ];
  }
  if (tunnel.type === "ngrok" && tunnel.ngrok) {
    const target = tunnel.ngrok.domain || view.status.address || "Public URL";
    const source = tunnel.ngrok.addr || "Local service";
    const kind = (tunnel.ngrok.proto || "http").toUpperCase();
    return [
      {
        kind,
        bind: source,
        target,
        hint: tunnel.ngrok.domain ? "Reserved domain" : "Public ngrok endpoint",
        copy: tunnel.ngrok.addr || target,
        bindHref: kind === "HTTP" ? inferredWebURL(source, "http", true) : undefined,
        targetHref: kind === "HTTP" ? inferredWebURL(target, "https") : undefined,
      },
    ];
  }
  if (tunnel.type === "command" && tunnel.command) {
    const command = tunnel.command.line || view.preview || "Command";
    return [
      {
        kind: "CMD",
        bind: command,
        target: tunnel.command.dir || "Default working directory",
        hint: tunnel.command.readyLog ? `Ready when log matches ${tunnel.command.readyLog}` : "Supervised command",
        copy: command,
      },
    ];
  }
  return [];
}

function protocolLabel(value: string, fallback: string): string {
  return value.match(/^([a-z][a-z0-9+.-]*):\/\//i)?.[1].toUpperCase() || fallback;
}

function ForwardEndpoint({
  value,
  label,
  href,
  className,
}: {
  value: string;
  label: string;
  href?: string;
  className: string;
}) {
  const resolvedHref = href || webURL(value);
  const classes = `mono forward-endpoint ${className}`;
  return resolvedHref ? (
    <a
      className={classes}
      href={resolvedHref}
      title={value}
      onClick={(event) => {
        event.preventDefault();
        void openURL(resolvedHref);
      }}
    >
      {label}
    </a>
  ) : (
    <span className={classes} title={value}>
      {label}
    </span>
  );
}

function forwardLabel(kind: string, value: string): string {
  return kind === "HTTP" ? value.replace(/^http:\/\//i, "") : value;
}

function webURL(value: string): string | undefined {
  try {
    const url = new URL(value);
    return url.protocol === "http:" || url.protocol === "https:" ? url.href : undefined;
  } catch {
    return undefined;
  }
}

function inferredWebURL(value: string, scheme: "http" | "https", local = false): string | undefined {
  const explicit = webURL(value);
  if (explicit) return explicit;
  if (local) {
    const localURL = localWebURL(value);
    if (localURL) return localURL;
  }
  if (!value || /\s|\*/.test(value)) return undefined;
  try {
    const url = new URL(`${scheme}://${value}`);
    return url.hostname ? url.href : undefined;
  } catch {
    return undefined;
  }
}

function localWebURL(value: string): string | undefined {
  const endpoint = value.trim();
  if (!endpoint) return undefined;
  const explicit = webURL(endpoint);
  if (explicit) {
    const url = new URL(explicit);
    return isLocalHost(url.hostname) ? explicit : undefined;
  }
  let host = "localhost";
  let port = endpoint;
  const bracketed = endpoint.match(/^\[([^\]]+)]:(\d+)$/);
  if (bracketed) {
    [, host, port] = bracketed;
  } else if (!/^\d+$/.test(endpoint)) {
    const separator = endpoint.lastIndexOf(":");
    if (separator < 0) return undefined;
    host = endpoint.slice(0, separator) || "localhost";
    port = endpoint.slice(separator + 1);
  }
  const portNumber = Number(port);
  if (!isLocalHost(host) || !Number.isInteger(portNumber) || portNumber < 1 || portNumber > 65535) return undefined;
  const browserHost =
    host === "0.0.0.0" || host === "*" ? "127.0.0.1" : host === "::" ? "[::1]" : host === "::1" ? "[::1]" : host;
  return `http://${browserHost}:${portNumber}`;
}

function isLocalHost(host: string): boolean {
  return (
    host === "localhost" ||
    host === "0.0.0.0" ||
    host === "*" ||
    host === "::" ||
    host === "::1" ||
    /^127(?:\.\d{1,3}){3}$/.test(host)
  );
}

function formatUptime(startedAt: string, now: number): string {
  const seconds = Math.max(0, Math.floor((now - new Date(startedAt).getTime()) / 1000));
  if (seconds < 60) return `${seconds}s`;
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m`;
  return `${Math.floor(seconds / 3600)}h ${Math.floor((seconds % 3600) / 60)}m`;
}

function capitalize(value: string) {
  return value.charAt(0).toUpperCase() + value.slice(1);
}
