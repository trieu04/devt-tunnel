import { KeyboardEvent, useEffect, useMemo, useRef, useState } from "react";
import { Service, onQuickOpened } from "../api";
import { isActive } from "../lib/filter";
import { SearchGlyph, StateBadge } from "../parts";
import { useOverview } from "../useOverview";
import { buildResults, Item, Scope } from "./results";

export function QuickPalette({
  embedded,
  onClose,
  onNavigate,
}: {
  embedded?: boolean;
  onClose: () => void;
  onNavigate: (screen: string, param?: string) => void;
}) {
  const { overview } = useOverview();
  const [query, setQuery] = useState("");
  const [scope, setScope] = useState<Scope>("all");
  const [index, setIndex] = useState(0);
  const [hosts, setHosts] = useState<{ alias: string; source: string }[]>([]);
  const input = useRef<HTMLInputElement>(null);
  const results = useRef<HTMLDivElement>(null);
  const reset = () => {
    setQuery("");
    setScope("all");
    setIndex(0);
    input.current?.focus();
  };

  useEffect(() => {
    reset();
    Service.SSHHosts(overview?.activeProfile ?? "").then((result) => setHosts(result.hosts ?? []));
    return embedded ? undefined : onQuickOpened(reset);
  }, [embedded, overview?.activeProfile]);

  const groups = useMemo(
    () =>
      buildResults({
        query,
        scope,
        tunnels: overview?.tunnels ?? [],
        hosts,
        recents: overview?.lastUsed ?? {},
        activeProfile: overview?.activeProfile ?? "",
      }),
    [query, scope, overview, hosts],
  );
  const items = groups.flatMap((group) => group.items);
  const active = Math.min(index, Math.max(items.length - 1, 0));
  const highlighted = items[active];

  useEffect(() => {
    results.current?.querySelector<HTMLElement>('[aria-selected="true"]')?.scrollIntoView({ block: "nearest" });
  }, [active, groups]);

  const close = () => {
    onClose();
    if (!embedded) Service.HideQuickLaunch();
  };
  const choose = (item?: Item, startCopy = false) => {
    if (!item) return;
    if (item.kind === "tunnel") {
      (startCopy ? Service.StartAndCopy(item.view.tunnel.id) : Service.Toggle(item.view.tunnel.id)).finally(close);
    } else if (item.kind === "host") {
      onNavigate("editor", `host:${item.host.alias}`);
      close();
    } else {
      const destinations: Record<string, [string, string?]> = {
        new: ["editor"],
        paste: ["editor", "paste"],
        main: ["tunnels"],
        vault: ["settings", "vault"],
      };
      if (item.id === "stop-all")
        (overview?.tunnels ?? []).filter((v) => isActive(v.status.state)).forEach((v) => Service.Stop(v.tunnel.id));
      else onNavigate(...destinations[item.id]);
      close();
    }
  };

  const onKeyDown = (event: KeyboardEvent) => {
    const modifier = event.metaKey || event.ctrlKey;
    const selectedText = input.current && input.current.selectionStart !== input.current.selectionEnd;
    if (event.key === "ArrowDown" || event.key === "ArrowUp") {
      event.preventDefault();
      event.stopPropagation();
      const delta = event.key === "ArrowDown" ? 1 : -1;
      setIndex((active + delta + Math.max(items.length, 1)) % Math.max(items.length, 1));
    } else if (event.key === "Tab") {
      event.preventDefault();
      event.stopPropagation();
      const scopes: Scope[] = ["all", "tunnels", "hosts", "actions"];
      const delta = event.shiftKey ? -1 : 1;
      setScope(scopes[(scopes.indexOf(scope) + delta + scopes.length) % scopes.length]);
      setIndex(0);
    } else if (event.key === "Escape") {
      event.preventDefault();
      event.stopPropagation();
      if (query) setQuery("");
      else close();
    } else if (event.key === "Enter") {
      event.preventDefault();
      event.stopPropagation();
      choose(highlighted, modifier);
    } else if (modifier && event.key.toLowerCase() === "e" && highlighted?.kind === "tunnel") {
      event.preventDefault();
      event.stopPropagation();
      onNavigate("editor", `edit:${highlighted.view.tunnel.id}`);
      close();
    } else if (modifier && event.key.toLowerCase() === "l" && highlighted?.kind === "tunnel") {
      event.preventDefault();
      event.stopPropagation();
      onNavigate("tunnels", `select:${highlighted.view.tunnel.id}`);
      close();
    } else if (
      modifier &&
      event.key.toLowerCase() === "c" &&
      !selectedText &&
      highlighted?.kind === "tunnel" &&
      highlighted.view.status.address
    ) {
      event.preventDefault();
      event.stopPropagation();
      void navigator.clipboard.writeText(highlighted.view.status.address);
    } else if (modifier && event.key.toLowerCase() === "n") {
      event.preventDefault();
      event.stopPropagation();
      if (highlighted?.kind === "host") choose(highlighted);
      else {
        onNavigate("editor");
        close();
      }
    } else if (modifier && event.shiftKey && event.key.toLowerCase() === "v") {
      event.preventDefault();
      event.stopPropagation();
      onNavigate("editor", "paste");
      close();
    }
  };

  let flatIndex = 0;
  return (
    <div className="quick" onKeyDown={onKeyDown}>
      <div className="quick-search">
        <SearchGlyph />
        <label htmlFor="quick-input" className="sr">
          Search tunnels, SSH hosts and actions
        </label>
        <input
          id="quick-input"
          ref={input}
          autoFocus
          value={query}
          onChange={(event) => {
            setQuery(event.target.value);
            setIndex(0);
          }}
          placeholder="Search tunnels, hosts or actions"
        />
        <kbd>esc</kbd>
      </div>
      <div className="quick-scopebar">
        <div className="quick-scopes">
          {(["all", "tunnels", "hosts", "actions"] as Scope[]).map((value) => (
            <button key={value} aria-pressed={scope === value} onClick={() => setScope(value)}>
              {value === "hosts" ? "SSH hosts" : value}
            </button>
          ))}
        </div>
        <span className="muted">Tab to switch scope</span>
      </div>
      <div className="quick-results" role="listbox" ref={results}>
        {groups.map((group) => (
          <section key={group.group}>
            <h2>{group.group === "SSH hosts" ? "SSH hosts without a tunnel" : group.group}</h2>
            {group.items.map((item) => {
              const current = flatIndex++;
              const label =
                item.kind === "tunnel" ? item.view.tunnel.name : item.kind === "host" ? item.host.alias : item.label;
              const key =
                item.kind === "tunnel"
                  ? `tunnel:${item.view.tunnel.id}`
                  : item.kind === "host"
                    ? `host:${item.host.alias}:${item.host.source}`
                    : `action:${item.id}`;
              const firstForward = item.kind === "tunnel" ? item.view.tunnel.forwards?.[0] : undefined;
              return (
                <button
                  type="button"
                  role="option"
                  aria-selected={current === active}
                  className={current === active ? "quick-active" : ""}
                  key={key}
                  onPointerMove={() => setIndex(current)}
                  onClick={() => choose(item)}
                >
                  <span
                    className={`quick-kind-dot quick-kind-${item.kind} ${item.kind === "tunnel" ? `status-dot-${item.view.status.state}` : ""}`}
                  />
                  <span className="quick-item-text">
                    <span>
                      <strong>{label}</strong>
                      {item.kind === "tunnel" && <StateBadge state={item.view.status.state} />}
                    </span>
                    {firstForward && (
                      <small className="mono">
                        -{firstForward.kind} {firstForward.bind} → {firstForward.target || "SOCKS5"}
                      </small>
                    )}
                    {item.kind === "host" && <small className="mono">{item.host.source}</small>}
                  </span>
                  <span className="spacer" />
                  {item.kind === "tunnel" && <kbd>↵ {isActive(item.view.status.state) ? "Stop" : "Start"}</kbd>}
                  {item.kind === "host" && <kbd>⌘N New tunnel</kbd>}
                  {item.kind === "action" && <kbd>{item.hint}</kbd>}
                </button>
              );
            })}
          </section>
        ))}
        {groups.length === 0 && <div className="quick-none">No matching tunnels, hosts or actions.</div>}
      </div>
      <footer className="quick-hint muted">
        <span>
          {highlighted?.kind === "host"
            ? "↑↓ move · ↵ or ⌘N create tunnel · Tab scope"
            : highlighted?.kind === "tunnel"
              ? "↑↓ move · ↵ start/stop · ⌘↵ start & copy · ⌘E edit · ⌘L log"
              : "↑↓ move · ↵ run · Tab scope"}
        </span>
        <span>Recently used ranks first</span>
      </footer>
    </div>
  );
}
