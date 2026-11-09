import type { Lease } from "../../bindings/github.com/trieu04/devt-tunnel/internal/core/model";
import { useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { Overview, Service, TunnelView, onLogsAppended, openURL } from "../api";
import { AgentBadge, EphemeralTag } from "../AgentBadge";
import { SaveTunnelDialog } from "../SaveTunnelDialog";
import { applyLogUpdate } from "../lib/logs";
import { commandSummary, fitCount } from "../lib/summary";
import { ViewFilter, filterTunnels, inGroup, isActive } from "../lib/filter";
import {
  ChevronGlyph,
  CopyGlyph,
  HoverTip,
  PlayGlyph,
  PlusGlyph,
  SearchGlyph,
  SettingsGlyph,
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
  group: requestedGroup,
  onGroup,
}: {
  overview: Overview;
  selectedId: string | null;
  onSelect: (id: string | null) => void;
  onNavigate: (screen: string, param?: string) => void;
  refresh: () => void;
  group: string | null;
  onGroup: (group: string | null) => void;
}) {
  const [leases, setLeases] = useState<Lease[] | undefined>();
  const [saving, setSaving] = useState<TunnelView | null>(null);
  const [query, setQuery] = useState("");
  const [view, setView] = useState<ViewFilter>("all");
  const [error, setError] = useState("");
  const [now, setNow] = useState(Date.now());
  const [sshInfo, setSSHInfo] = useState<{ path: string; hosts: { alias: string }[] }>({
    path: "~/.ssh/config",
    hosts: [],
  });
  const tunnels = overview.tunnels ?? [];
  useEffect(() => {
    let mounted = true;
    Service.ListLeases()
      .then((items) => {
        if (mounted) setLeases(items ?? []);
      })
      .catch(() => {
        if (mounted) setLeases(undefined);
      });
    return () => {
      mounted = false;
    };
  }, [overview]);
  const profiles = overview.profiles ?? [];
  const groups = overview.groups ?? [];
  const profileNames = useMemo(() => new Map(profiles.map((profile) => [profile.id, profile.name])), [profiles]);
  const groupNames = useMemo(() => new Map(groups.map((group) => [group.id, group.name])), [groups]);
  // A group deleted elsewhere must not leave the list filtered by nothing.
  const group = requestedGroup !== null && groupNames.has(requestedGroup) ? requestedGroup : null;
  const scoped = tunnels.filter((item) => inGroup(item, group));
  const visible = useMemo(() => filterTunnels(tunnels, { query, view, group }), [tunnels, query, view, group]);
  const selected = visible.find((item) => item.tunnel.id === selectedId) ?? visible[0] ?? null;
  const running = tunnels.filter((item) => isActive(item.status.state)).length;
  const favorites = tunnels.filter((item) => item.tunnel.favorite).length;

  useEffect(() => {
    if ((selected?.tunnel.id ?? null) !== selectedId) onSelect(selected?.tunnel.id ?? null);
  }, [selectedId, selected, onSelect]);
  useEffect(() => {
    const timer = window.setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(timer);
  }, []);
  useEffect(() => {
    Service.SSHHosts("")
      .then((result) => setSSHInfo({ path: result.configPath || "~/.ssh/config", hosts: result.hosts ?? [] }))
      .catch(() => undefined);
  }, []);
  useEffect(() => {
    const moveSelection = (event: KeyboardEvent) => {
      if (document.querySelector('[aria-modal="true"]')) return;
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
  const activeGroup = groups.find((item) => item.id === group);
  const scopedRunning = scoped.filter((item) => isActive(item.status.state)).length;

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
        <button className="primary new-tunnel" onClick={() => onNavigate("editor")}>
          <PlusGlyph />
          New tunnel
        </button>
      </header>
      {overview.configError && <div className="banner banner-error">{overview.configError}</div>}
      {error && <div className="banner banner-error">{error}</div>}
      <div className="body tunnel-workspace">
        <nav className="sidebar" aria-label="Filters">
          <div className="sidebar-group">
            <h2>View</h2>
            {(["all", "running", "favorites"] as ViewFilter[]).map((value) => (
              <button className="nav" key={value} aria-pressed={view === value} onClick={() => setView(value)}>
                <span>{capitalize(value)}</span>
                <span className="nav-count">
                  {value === "all"
                    ? scoped.length
                    : value === "running"
                      ? scopedRunning
                      : scoped.filter((item) => item.tunnel.favorite).length}
                </span>
              </button>
            ))}
          </div>
          <div className="sidebar-group">
            <div className="sidebar-heading">
              <h2>Groups</h2>
              <button className="sidebar-add" aria-label="New group" onClick={() => onNavigate("groups", "new")}>
                <PlusGlyph />
              </button>
            </div>
            {groups.map((item) => (
              <GroupNav
                key={item.id}
                name={item.name}
                members={tunnels.filter((view) => inGroup(view, item.id))}
                selected={group === item.id}
                onSelect={() => onGroup(group === item.id ? null : item.id)}
                onStart={() => run(Service.StartGroup(item.id))}
                onStop={() => run(Service.StopGroup(item.id))}
              />
            ))}
            {groups.length === 0 && (
              <button className="nav nav-hint" onClick={() => onNavigate("groups", "new")}>
                <span>Group tunnels to start them together</span>
              </button>
            )}
          </div>
          <div className="sidebar-manage">
            <button className="nav" onClick={() => onNavigate("settings")}>
              <span>
                <SettingsGlyph />
                Settings
              </span>
            </button>
          </div>
        </nav>
        <main className="tunnel-list" role="list">
          <div className="list-heading">
            <div className="list-title">
              <h1>{activeGroup?.name ?? "All tunnels"}</h1>
              {activeGroup?.description && <p className="muted">{activeGroup.description}</p>}
              <span className="list-summary">
                {visible.length} shown · {activeGroup ? `${scopedRunning} of ${scoped.length}` : running} running
                {favorites && !activeGroup ? ` · ${favorites} favorites` : ""}
              </span>
            </div>
            {activeGroup && (
              <div className="actions">
                <button
                  className="primary"
                  disabled={scoped.length === 0}
                  onClick={() => run(Service.StartGroup(activeGroup.id))}
                >
                  <PlayGlyph />
                  Start all
                </button>
                <button disabled={scopedRunning === 0} onClick={() => run(Service.StopGroup(activeGroup.id))}>
                  <StopGlyph />
                  Stop all
                </button>
                <button onClick={() => onNavigate("groups", activeGroup.id)}>Edit group</button>
              </div>
            )}
          </div>
          {visible.length === 0 ? (
            <Empty
              query={query}
              hasTunnels={tunnels.length > 0}
              group={activeGroup && scoped.length === 0 ? activeGroup : undefined}
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
                leases={leases}
                selected={selected?.tunnel.id === item.tunnel.id}
                profileNames={profileNames}
                groupNames={groupNames}
                now={now}
                onSelect={() => onSelect(item.tunnel.id)}
                onToggle={() => run(Service.Toggle(item.tunnel.id))}
                onFavorite={() => run(Service.SetFavorite(item.tunnel.id, !item.tunnel.favorite))}
                onVault={() => onNavigate("settings", "vault")}
                onEdit={() => onNavigate("editor", `edit:${item.tunnel.id}`)}
                onSave={() => setSaving(item)}
              />
            ))
          )}
        </main>
        {selected && (
          <Detail
            view={selected}
            leases={leases}
            profileNames={profileNames}
            groupNames={groupNames}
            now={now}
            onNavigate={onNavigate}
            run={run}
            onSave={() => setSaving(selected)}
          />
        )}
      </div>
      {saving && (
        <SaveTunnelDialog key={saving.tunnel.id} view={saving} onClose={() => setSaving(null)} onSaved={refresh} />
      )}
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
  group,
  matchingHost,
  onNavigate,
}: {
  query: string;
  hasTunnels: boolean;
  group?: { id: string; name: string };
  matchingHost?: string;
  onNavigate: (screen: string, param?: string) => void;
}) {
  if (group)
    return (
      <div className="empty">
        <h1>{group.name} has no tunnels</h1>
        <p className="muted">Choose the tunnels this group should start and stop together.</p>
        <div className="actions">
          <button className="primary" onClick={() => onNavigate("groups", group.id)}>
            Choose tunnels
          </button>
        </div>
      </div>
    );
  if (hasTunnels && !query)
    return (
      <div className="empty">
        <h1>No tunnels match these filters</h1>
        <p className="muted">Clear the view or group in the sidebar to see more.</p>
      </div>
    );
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

/** A sidebar group: selecting filters the list, the button acts on the whole group. */
function GroupNav({
  name,
  members,
  selected,
  onSelect,
  onStart,
  onStop,
}: {
  name: string;
  members: TunnelView[];
  selected: boolean;
  onSelect: () => void;
  onStart: () => void;
  onStop: () => void;
}) {
  const running = members.filter((item) => isActive(item.status.state)).length;
  const failed = members.some((item) => item.status.state === "failed");
  const state = failed ? "failed" : running === 0 ? "stopped" : running === members.length ? "running" : "starting";
  return (
    <div className={`group-nav ${selected ? "group-nav-selected" : ""}`}>
      <button className="nav" aria-pressed={selected} onClick={onSelect}>
        <span className={`status-dot status-dot-${state}`} />
        <span className="nav-label">{name}</span>
        <span className="nav-count">
          {running}/{members.length}
        </span>
      </button>
      <button
        className="group-action"
        disabled={members.length === 0}
        aria-label={`${running ? "Stop" : "Start"} group ${name}`}
        title={running ? "Stop all" : "Start all"}
        onClick={running ? onStop : onStart}
      >
        {running ? <StopGlyph /> : <PlayGlyph />}
      </button>
    </div>
  );
}

/** As many groups as the row has room for; the rest are behind +N. */
function GroupChips({ labels }: { labels: string[] }) {
  const box = useRef<HTMLSpanElement>(null);
  const sizer = useRef<HTMLSpanElement>(null);
  const [fit, setFit] = useState(labels.length);
  useLayoutEffect(() => {
    const element = box.current;
    if (!element) return;
    const measure = () => {
      const widths = Array.from(sizer.current?.children ?? []).map((chip) => chip.getBoundingClientRect().width);
      const more = widths.pop() ?? 0;
      setFit(fitCount(widths, more, element.clientWidth, parseFloat(getComputedStyle(element).columnGap) || 0));
    };
    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(element);
    // Inter loads after first paint and changes every width.
    document.fonts.ready.then(measure);
    return () => observer.disconnect();
  }, [labels.join("\0")]);
  if (labels.length === 0) return null;
  const hidden = labels.slice(fit);
  return (
    <span className="group-chips" ref={box}>
      {labels.slice(0, fit).map((label, index) => (
        <span className="group-chip" key={index} title={label}>
          {label}
        </span>
      ))}
      {hidden.length > 0 && (
        <HoverTip className="group-chip group-more" lines={hidden}>
          +{hidden.length}
        </HoverTip>
      )}
      <span className="group-chips-sizer" ref={sizer} aria-hidden="true">
        {labels.map((label, index) => (
          <span className="group-chip" key={index}>
            {label}
          </span>
        ))}
        <span className="group-chip">+{labels.length}</span>
      </span>
    </span>
  );
}

function TunnelRow({
  view,
  leases,
  selected,
  profileNames,
  groupNames,
  now,
  onSelect,
  onToggle,
  onFavorite,
  onVault,
  onEdit,
  onSave,
}: {
  view: TunnelView;
  leases?: Lease[];
  selected: boolean;
  profileNames: Map<string, string>;
  groupNames: Map<string, string>;
  now: number;
  onSelect: () => void;
  onToggle: () => void;
  onFavorite: () => void;
  onVault: () => void;
  onEdit: () => void;
  onSave: () => void;
}) {
  const forwards = displayForwards(view);
  const [expanded, setExpanded] = useState(forwards.length > 0 || !!view.status.error);
  const active = isActive(view.status.state);
  const command = commandSummary(view.tunnel);
  const uptime = runningTime(view, now);
  const groupLabels = (view.tunnel.groups ?? []).map((group) => groupNames.get(group) ?? group);
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
        <div className="row-main">
          <span className="row-line-top">
            <span className="row-title">
              <button
                className={`favorite-action ${view.tunnel.favorite ? "favorite-on" : ""}`}
                aria-label={`${view.tunnel.favorite ? "Remove from" : "Add to"} favorites`}
                onClick={onFavorite}
              >
                {view.tunnel.favorite ? "★" : "☆"}
              </button>
              <button className="name row-name" title={view.tunnel.name}>
                {view.tunnel.name}
              </button>
            </span>
            <span className="row-tags">
              {view.tunnel.profile && (
                <span className="profile-chip" title="Profile">
                  {profileNames.get(view.tunnel.profile) ?? view.tunnel.profile}
                </span>
              )}
              <GroupChips labels={groupLabels} />
            </span>
          </span>
          <span className="row-line-bottom">
            <span className="row-command" title={command}>
              {command}
            </span>
            <AgentBadge owner={view.owner} now={now} tunnelID={view.tunnel.id} leases={leases} />
            {view.status.stale && <span className="stale-pill">Restart to apply changes</span>}
          </span>
        </div>
        {view.owner?.ephemeral && <EphemeralTag onSave={onSave} />}
        <span className="row-state">
          <StateBadge state={view.status.state} />
          {uptime && <span className="row-uptime">{uptime}</span>}
        </span>
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
  leases,
  profileNames,
  groupNames,
  now,
  onNavigate,
  run,
  onSave,
}: {
  view: TunnelView;
  leases?: Lease[];
  profileNames: Map<string, string>;
  groupNames: Map<string, string>;
  now: number;
  onNavigate: (screen: string, param?: string) => void;
  run: (operation: Promise<unknown>) => void;
  onSave: () => void;
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
    {
      key: "Profile",
      value: view.tunnel.profile ? (profileNames.get(view.tunnel.profile) ?? view.tunnel.profile) : "None",
    },
    { key: "Groups", value: view.tunnel.groups?.map((id) => groupNames.get(id) ?? id).join(", ") || "None" },
    { key: "Forwards", value: String(displayForwards(view).length) },
    { key: "Auto-reconnect", value: view.tunnel.reconnect ? "On" : "Off" },
    { key: "PID", value: view.status.pid ? String(view.status.pid) : "—" },
    {
      key: "Uptime",
      value: runningTime(view, now) || "—",
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
      <div className="ownership-tags">
        <AgentBadge owner={view.owner} now={now} tunnelID={view.tunnel.id} leases={leases} />
        {view.owner?.ephemeral && <EphemeralTag onSave={onSave} />}
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

/** Only a running tunnel has a meaningful uptime. */
function runningTime(view: TunnelView, now: number): string {
  return view.status.state === "running" && view.status.startedAt ? formatUptime(view.status.startedAt, now) : "";
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
