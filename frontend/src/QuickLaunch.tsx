import { KeyboardEvent, useEffect, useMemo, useRef, useState } from "react";
import { Service, onQuickOpened } from "./api";
import { isActive, matchesSearch } from "./filter";
import { StateBadge } from "./parts";
import { useOverview } from "./useOverview";

export function QuickLaunch() {
  const { overview } = useOverview();
  const [query, setQuery] = useState("");
  const [index, setIndex] = useState(0);
  const input = useRef<HTMLInputElement>(null);

  useEffect(
    () =>
      onQuickOpened(() => {
        setQuery("");
        setIndex(0);
        input.current?.focus();
      }),
    [],
  );

  const results = useMemo(
    () => (overview?.tunnels ?? []).filter((v) => matchesSearch(v, query)),
    [overview, query],
  );
  const active = Math.min(index, Math.max(results.length - 1, 0));

  const onKeyDown = (e: KeyboardEvent) => {
    const chosen = results[active];
    switch (e.key) {
      case "ArrowDown":
        e.preventDefault();
        setIndex(Math.min(active + 1, results.length - 1));
        break;
      case "ArrowUp":
        e.preventDefault();
        setIndex(Math.max(active - 1, 0));
        break;
      case "Escape":
        Service.HideQuickLaunch();
        break;
      case "Enter":
        if (chosen) Service.Toggle(chosen.tunnel.id);
        break;
    }
  };

  return (
    <div className="quick" onKeyDown={onKeyDown}>
      <label htmlFor="quick-input" className="sr">
        Search tunnels
      </label>
      <input
        id="quick-input"
        ref={input}
        autoFocus
        value={query}
        onChange={(e) => {
          setQuery(e.target.value);
          setIndex(0);
        }}
        placeholder="Search name, host or port"
      />
      <ul className="quick-results" role="listbox" aria-label="Tunnels">
        {results.map((v, i) => (
          <li key={v.tunnel.id} role="option" aria-selected={i === active} className={i === active ? "quick-active" : ""}>
            <button type="button" onClick={() => Service.Toggle(v.tunnel.id)} onMouseEnter={() => setIndex(i)}>
              <span className="name">{v.tunnel.name}</span>
              <span className="badge">{v.tunnel.type}</span>
              <span className="spacer" />
              <StateBadge state={v.status.state} />
            </button>
          </li>
        ))}
        {results.length === 0 && <li className="muted quick-none">No matching tunnels.</li>}
      </ul>
      <footer className="quick-hint muted">
        ↵ {results[active] && isActive(results[active].status.state) ? "stop" : "start"} · esc close
      </footer>
    </div>
  );
}
