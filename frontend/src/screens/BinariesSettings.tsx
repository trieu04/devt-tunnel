import { useEffect, useRef, useState } from "react";
import type { ToolCandidate, ToolInfo } from "../../bindings/github.com/trieu04/devt-tunnel/internal/api";
import { Overview, Service } from "../api";
import { DownloadControl } from "../DownloadControl";
import { onToolDownloaded } from "../downloads";
import {
  installCommands,
  originLabel,
  platformOf,
  removeDisabled,
  sshInstallHelp,
  staleMessage,
  staleTunnels,
  TOOLS,
  truncateMiddle,
  versionFromPath,
} from "../lib/binaries";

export function Binaries({ overview, focus }: { overview: Overview; focus?: string }) {
  const [tools, setTools] = useState<ToolInfo[]>([]);
  const [loaded, setLoaded] = useState(false);
  const load = () =>
    Service.Tools().then((items) => {
      setTools(items ?? []);
      setLoaded(true);
    });
  useEffect(() => {
    void load();
  }, []);
  return (
    <div>
      <h1 className="tool-heading">Binaries</h1>
      {loaded &&
        TOOLS.map((name) => (
          <ToolCard
            key={name}
            name={name}
            info={tools.find((tool) => tool.name === name)}
            overview={overview}
            focused={focus === name}
            onTools={setTools}
          />
        ))}
    </div>
  );
}

function ToolCard({
  name,
  info,
  overview,
  focused,
  onTools,
}: {
  name: string;
  info?: ToolInfo;
  overview: Overview;
  focused: boolean;
  onTools: (tools: ToolInfo[]) => void;
}) {
  const [candidates, setCandidates] = useState<ToolCandidate[] | null>(null);
  const [error, setError] = useState("");
  const [customOpen, setCustomOpen] = useState(false);
  const [customPath, setCustomPath] = useState("");
  const [busy, setBusy] = useState(false);
  const ref = useRef<HTMLElement>(null);
  const loadCandidates = () =>
    Service.ToolCandidates(name)
      .then((items) => setCandidates(items ?? []))
      .catch((reason) => {
        setCandidates([]);
        setError(String(reason));
      });
  useEffect(() => {
    void loadCandidates();
  }, [name]);
  useEffect(
    () =>
      onToolDownloaded((tool) => {
        if (tool !== name) return;
        void Service.Tools().then((items) => onTools(items ?? []));
        void loadCandidates();
      }),
    [name],
  );
  useEffect(() => {
    if (focused && candidates) ref.current?.scrollIntoView?.({ block: "start" });
  }, [focused, candidates !== null]);

  const select = async (path: string) => {
    setBusy(true);
    setError("");
    try {
      onTools((await Service.SetToolPath(name, path)) ?? []);
      setCustomOpen(false);
      await loadCandidates();
    } catch (reason) {
      setError(String(reason));
    } finally {
      setBusy(false);
    }
  };
  const reveal = (path: string) => Service.RevealPath(path).catch((reason) => setError(String(reason)));
  const remove = async (path: string) => {
    setError("");
    try {
      onTools((await Service.RemoveDownloadedTool(name, versionFromPath(path))) ?? []);
      await loadCandidates();
    } catch (reason) {
      setError(String(reason).replace(/^(Error:\s*)+/, ""));
    }
  };
  const browse = async () => {
    try {
      const picked = await Service.PickExecutable(name);
      if (picked) setCustomPath(picked);
    } catch (reason) {
      setError(String(reason));
    }
  };

  const platform = platformOf(navigator.platform);
  const list = candidates ?? [];
  const auto = list.find((candidate) => candidate.auto);
  const hasSelection = list.some((candidate) => candidate.selected);
  const stale = staleTunnels(overview.tunnels, name, info?.path ?? "");
  const headline = info?.path
    ? `${info.version.split("\n")[0] || "version unknown"} · ${originLabel(info.origin || "path")}`
    : "Not installed";
  const missing = candidates !== null && list.length === 0;

  return (
    <article className="card tool-card" id={`tool-${name}`} ref={ref} aria-current={focused || undefined}>
      <h2>{name}</h2>
      {missing ? (
        <MissingTool name={name} platform={platform} offer={info?.download} onBrowse={() => setCustomOpen(true)} />
      ) : (
        <p className={info?.error ? "error-text" : "muted"}>
          {info?.error && info.path ? `⚠ ${info.error}` : headline}
        </p>
      )}
      {candidates !== null && (!missing || customOpen) && (
        <div className="tool-options" role="radiogroup" aria-label={`${name} binary`}>
          {!missing && (
            <label className="tool-option">
              <input
                type="radio"
                name={name}
                checked={!hasSelection && !customOpen}
                disabled={busy}
                onChange={() => void select("")}
              />
              <span>Auto</span>
              <span className="muted tool-hint" title={auto?.path}>
                {auto ? `uses ${truncateMiddle(auto.path, 40)}` : "no binary found"}
              </span>
            </label>
          )}
          {list.map((candidate) => (
            <div className="tool-option" key={candidate.path}>
              <label className="tool-option-main">
                <input
                  type="radio"
                  name={name}
                  checked={candidate.selected && !customOpen}
                  disabled={busy}
                  onChange={() => void select(candidate.path)}
                />
                <span className="mono tool-path" title={candidate.path}>
                  {truncateMiddle(candidate.path)}
                </span>
                {candidate.error ? (
                  <span className="error-text tool-version" title={candidate.error}>
                    {candidate.error}
                  </span>
                ) : (
                  <span className="tool-version">{candidate.version}</span>
                )}
                <span className="muted">{originLabel(candidate.origin)}</span>
              </label>
              {candidate.origin === "downloaded" && (
                <RowMenu
                  label={`Actions for ${candidate.path}`}
                  items={[
                    { label: revealLabel(platform), onSelect: () => void reveal(candidate.path) },
                    {
                      label: "Remove",
                      danger: true,
                      disabled: removeDisabled(overview.tunnels, candidate.path),
                      title: removeDisabled(overview.tunnels, candidate.path)
                        ? "A running tunnel uses this version. Stop it first."
                        : undefined,
                      onSelect: () => void remove(candidate.path),
                    },
                  ]}
                />
              )}
            </div>
          ))}
          <label className="tool-option">
            <input type="radio" name={name} checked={customOpen} disabled={busy} onChange={() => setCustomOpen(true)} />
            <span>Custom…</span>
          </label>
        </div>
      )}
      {customOpen && (
        <div className="address tool-custom">
          <input
            className="mono"
            value={customPath}
            placeholder="Absolute executable path"
            aria-label={`${name} path`}
            onChange={(event) => setCustomPath(event.target.value)}
          />
          <button onClick={() => void browse()}>Browse…</button>
          <button
            className="primary"
            disabled={busy || !customPath.trim()}
            onClick={() => void select(customPath.trim())}
          >
            Use
          </button>
        </div>
      )}
      {!missing && (
        <DownloadControl tool={name} offer={info?.download} newer={!!info?.path && info.origin === "downloaded"} />
      )}
      {missing && !info?.download && <DownloadControl tool={name} />}
      {error && <p className="error-text">{error}</p>}
      {stale.length > 0 && (
        <p className="tool-stale">
          {staleMessage(stale.length, name)}{" "}
          <button
            onClick={() =>
              Promise.all(stale.map((view) => Service.Restart(view.tunnel.id))).catch((reason) =>
                setError(String(reason)),
              )
            }
          >
            Restart them
          </button>
        </p>
      )}
    </article>
  );
}

function revealLabel(platform: ReturnType<typeof platformOf>): string {
  return platform === "mac"
    ? "Reveal in Finder"
    : platform === "windows"
      ? "Reveal in Explorer"
      : "Reveal in file manager";
}

type MenuItem = { label: string; onSelect: () => void; disabled?: boolean; title?: string; danger?: boolean };

/** A "⋯" button with a popover menu. Escape or a click elsewhere closes it. */
function RowMenu({ label, items }: { label: string; items: MenuItem[] }) {
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (!open) return;
    const close = (event: MouseEvent | KeyboardEvent) => {
      if (event instanceof KeyboardEvent) {
        if (event.key !== "Escape") return;
        ref.current?.querySelector("button")?.focus();
      } else if (ref.current?.contains(event.target as Node)) return;
      setOpen(false);
    };
    window.addEventListener("mousedown", close);
    window.addEventListener("keydown", close);
    return () => {
      window.removeEventListener("mousedown", close);
      window.removeEventListener("keydown", close);
    };
  }, [open]);
  return (
    <div className="log-menu" ref={ref}>
      <button
        className="icon-button"
        aria-label={label}
        aria-haspopup="menu"
        aria-expanded={open}
        onClick={() => setOpen(!open)}
      >
        ⋯
      </button>
      {open && (
        <div className="log-menu-popover" role="menu">
          {items.map((item) => (
            <button
              key={item.label}
              role="menuitem"
              className={item.danger ? "danger" : ""}
              disabled={item.disabled}
              title={item.title}
              onClick={() => {
                setOpen(false);
                item.onSelect();
              }}
            >
              {item.label}
            </button>
          ))}
        </div>
      )}
    </div>
  );
}

function MissingTool({
  name,
  platform,
  offer,
  onBrowse,
}: {
  name: string;
  platform: ReturnType<typeof platformOf>;
  offer?: ToolInfo["download"];
  onBrowse: () => void;
}) {
  const commands = installCommands(name, platform);
  return (
    <div className="tool-missing">
      <p className="error-text">⚠ {name} isn't installed</p>
      {name === "ssh" && <p className="muted">{sshInstallHelp(platform)}</p>}
      {offer && (
        <>
          <p className="muted">Cloudflare tunnels need it.</p>
          <DownloadControl tool={name} offer={offer} primary />
          {commands.length > 0 && <p className="muted">or install it another way</p>}
        </>
      )}
      {commands.map((command) => (
        <div className="address" key={command}>
          <code>{command}</code>
          <button aria-label={`Copy ${command}`} onClick={() => void navigator.clipboard.writeText(command)}>
            Copy
          </button>
        </div>
      ))}
      <p className="muted">
        Already installed somewhere else? <button onClick={onBrowse}>Choose file…</button>
      </p>
    </div>
  );
}
