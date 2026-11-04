import { useEffect, useState } from "react";
import type { Kind } from "../../bindings/github.com/trieu04/devt-tunnel/internal/core/secret/models";
import { Overview, Service } from "../api";
import { SHORTCUTS } from "../app/shortcuts";
import { ConfirmDialog } from "../ConfirmDialog";

type Page = "general" | "vault" | "ssh" | "binaries" | "shortcuts";

export function SettingsScreen({
  overview,
  initial = "general",
  onBack,
  onProfiles,
}: {
  overview: Overview;
  initial?: string;
  onBack: () => void;
  onProfiles: () => void;
}) {
  const [page, setPage] = useState<Page>(
    (["general", "vault", "ssh", "binaries", "shortcuts"].includes(initial) ? initial : "general") as Page,
  );
  const title = page === "ssh" ? "SSH & config files" : capitalize(page);
  return (
    <main className="screen settings">
      <header className="screen-header page-header">
        <div className="page-title">
          <span className="breadcrumb">
            <button className="breadcrumb-button" onClick={onBack}>
              Tunnels
            </button>{" "}
            / Settings
          </span>
          <h1>{title}</h1>
        </div>
        {page === "vault" && (
          <span className={`keychain-pill ${overview.keychainAvailable ? "" : "keychain-pill-error"}`}>
            ▣ {overview.keychainAvailable ? `Stored in ${overview.keychainName}` : "Keyring unavailable"}
          </span>
        )}
      </header>
      <div className="settings-shell">
        <nav className="settings-nav">
          <button aria-pressed={page === "general"} onClick={() => setPage("general")}>
            General
          </button>
          <button aria-pressed={page === "vault"} onClick={() => setPage("vault")}>
            Vault
          </button>
          <button onClick={onProfiles}>Profiles</button>
          <button aria-pressed={page === "ssh"} onClick={() => setPage("ssh")}>
            SSH & config files
          </button>
          <button aria-pressed={page === "binaries"} onClick={() => setPage("binaries")}>
            Binaries (ssh, ngrok, cloudflared)
          </button>
          <button aria-pressed={page === "shortcuts"} onClick={() => setPage("shortcuts")}>
            Shortcuts
          </button>
        </nav>
        <section className="settings-page">
          {page === "general" && <General overview={overview} />}
          {page === "vault" && <Vault overview={overview} />}
          {page === "ssh" && <SSHConfig overview={overview} />}
          {page === "binaries" && <Binaries />}
          {page === "shortcuts" && <Shortcuts overview={overview} />}
        </section>
      </div>
    </main>
  );
}

function General({ overview }: { overview: Overview }) {
  const [version, setVersion] = useState("");
  const [update, setUpdate] = useState<{ available: boolean; latest: string; url: string } | null>(null);
  const [checksEnabled, setChecksEnabled] = useState(true);
  const [error, setError] = useState("");
  useEffect(() => {
    Service.AppVersion().then(setVersion);
    Service.UpdateChecksEnabled().then(setChecksEnabled);
    Service.CheckForUpdate(false)
      .then(setUpdate)
      .catch(() => undefined);
  }, []);
  const toggleChecks = (enabled: boolean) => {
    setChecksEnabled(enabled);
    Service.SetUpdateChecks(enabled).catch((reason) => setError(String(reason)));
  };
  return (
    <div className="card">
      <h1>General</h1>
      <dl>
        <dt>Version</dt>
        <dd>{version}</dd>
        <dt>Window behavior</dt>
        <dd>Closing keeps tunnels running when a tray is available.</dd>
        <dt>Secrets</dt>
        <dd>{overview.keychainName}</dd>
        <dt>Updates</dt>
        <dd>
          {update?.available ? <a href={update.url}>Version {update.latest} is available</a> : "Up to date"}{" "}
          <button
            onClick={() =>
              Service.CheckForUpdate(true)
                .then(setUpdate)
                .catch((reason) => setError(String(reason)))
            }
          >
            Check now
          </button>
        </dd>
      </dl>
      <label className="check">
        <input type="checkbox" checked={checksEnabled} onChange={(event) => toggleChecks(event.target.checked)} />
        Check for releases automatically
      </label>
      {error && <p className="error-text">{error}</p>}
      <p className="muted">The optional GitHub release check is devt-tunnel's only network request of its own.</p>
    </div>
  );
}

function Vault({ overview }: { overview: Overview }) {
  const [secrets, setSecrets] = useState<
    { name: string; kind: Kind; state: string; usedBy: string[] | null; lastUsed: string }[]
  >([]);
  const [name, setName] = useState("");
  const [value, setValue] = useState("");
  const [kind, setKind] = useState<Kind>("env" as Kind);
  const [error, setError] = useState("");
  const [auth, setAuth] = useState(false);
  const [revealed, setRevealed] = useState<Record<string, string>>({});
  const [adding, setAdding] = useState(false);
  const [deleting, setDeleting] = useState<{ name: string; usedBy: string[] | null } | null>(null);
  const load = () => Service.ListSecrets().then((items) => setSecrets(items ?? []));
  useEffect(() => {
    void load();
    Service.UserAuthAvailable().then(setAuth);
    const hide = () => setRevealed({});
    window.addEventListener("blur", hide);
    return () => window.removeEventListener("blur", hide);
  }, []);
  useEffect(() => {
    if (!Object.keys(revealed).length) return;
    const timer = setTimeout(() => setRevealed({}), 30000);
    return () => clearTimeout(timer);
  }, [revealed]);
  const save = async () => {
    try {
      await Service.SetSecret(name, value, kind);
      setName("");
      setValue("");
      setAdding(false);
      await load();
    } catch (reason) {
      setError(String(reason));
    }
  };
  const hide = (secretName: string) =>
    setRevealed((current) => {
      const next = { ...current };
      delete next[secretName];
      return next;
    });
  const edit = (item: { name: string; kind: Kind }) => {
    setName(item.name);
    setKind(item.kind);
    setValue("");
    setAdding(true);
  };
  return (
    <div className="card vault-card">
      <div className="vault-heading">
        <div>
          <h2>Secrets</h2>
          <p>API keys, tunnel tokens and SSH key passphrases. Injected at start, never written to disk.</p>
        </div>
        <button
          className="primary"
          onClick={() => {
            setName("");
            setValue("");
            setKind("env" as Kind);
            setAdding(!adding);
          }}
        >
          + Add secret
        </button>
      </div>
      {error && <p className="error-text">{error}</p>}
      {adding && (
        <div className="secret-add">
          <input placeholder="Name" value={name} onChange={(event) => setName(event.target.value)} />
          <select value={kind} onChange={(event) => setKind(event.target.value as Kind)}>
            <option value="ssh_passphrase">SSH passphrase</option>
            <option value="ngrok">ngrok</option>
            <option value="cloudflared">Cloudflared</option>
            <option value="env">Environment</option>
          </select>
          <input
            type="password"
            autoComplete="new-password"
            placeholder="Value"
            value={value}
            onChange={(event) => setValue(event.target.value)}
          />
          <button className="primary" disabled={!name || !value} onClick={save}>
            {secrets.some((secret) => secret.name === name) ? "Replace" : "Add secret"}
          </button>
        </div>
      )}
      <div className="table-scroll">
        <table className="vault-table">
          <thead>
            <tr>
              <th>Name</th>
              <th>Kind</th>
              <th>Value</th>
              <th>Used by</th>
              <th>Last used</th>
              <th>
                <span className="sr">Actions</span>
              </th>
            </tr>
          </thead>
          <tbody>
            {secrets.map((item) => (
              <tr className={item.state === "present" ? "" : "secret-missing"} key={item.name}>
                <td className="mono secret-name">{item.name}</td>
                <td>{kindLabel(item.kind)}</td>
                <td className={`mono ${item.state === "present" ? "" : "error-text"}`}>
                  {revealed[item.name] ?? (item.state === "present" ? "••••••••" : capitalize(item.state))}
                </td>
                <td>{item.usedBy?.join(", ") || <span className="muted">Not used</span>}</td>
                <td className="muted">{item.lastUsed || "Never"}</td>
                <td className="vault-actions">
                  {item.state !== "present" ? (
                    <button className="danger" onClick={() => edit(item)}>
                      Paste token
                    </button>
                  ) : (
                    <>
                      {auth && (
                        <button
                          onClick={() =>
                            revealed[item.name]
                              ? hide(item.name)
                              : Service.RevealSecret(item.name)
                                  .then((secret) => setRevealed({ ...revealed, [item.name]: secret }))
                                  .catch((reason) => setError(String(reason)))
                          }
                        >
                          {revealed[item.name] ? "Hide" : "Reveal"}
                        </button>
                      )}
                      <button onClick={() => edit(item)}>Replace</button>
                    </>
                  )}
                  <button className="row-delete" onClick={() => setDeleting(item)}>
                    Delete
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      {!auth && (
        <p className="muted vault-note">
          Reveal is unavailable because this platform has no supported OS authentication prompt. Secrets can still be
          replaced.
        </p>
      )}
      {auth && (
        <p className="muted vault-note">
          Reveal asks for OS authentication. Deleting a secret that a tunnel uses moves that tunnel to “Needs token”.
        </p>
      )}
      {!overview.keychainAvailable && (
        <p className="banner">A Secret Service is required. Plaintext storage is not offered.</p>
      )}
      <ConfirmDialog
        open={!!deleting}
        title={`Delete ${deleting?.name ?? "secret"}?`}
        description={
          deleting?.usedBy?.length
            ? `Used by ${deleting.usedBy.join(", ")}. Those tunnels will move to “Needs token”.`
            : "This secret is not used by any tunnel."
        }
        confirmLabel="Delete secret"
        onCancel={() => setDeleting(null)}
        onConfirm={async () => {
          if (!deleting) return;
          await Service.DeleteSecret(deleting.name);
          await load();
        }}
      />
    </div>
  );
}

function SSHConfig({ overview }: { overview: Overview }) {
  const [hosts, setHosts] = useState<{ configPath: string; hosts: unknown[] | null; warnings: string[] | null } | null>(
    null,
  );
  const load = () => Service.SSHHosts(overview.activeProfile).then(setHosts);
  useEffect(() => {
    void load();
  }, [overview.activeProfile]);
  return (
    <div className="card">
      <h1>SSH & config files</h1>
      <dl>
        <dt>Definitions</dt>
        <dd className="mono">{overview.configPath}</dd>
        <dt>SSH config</dt>
        <dd className="mono">{hosts?.configPath}</dd>
        <dt>Hosts</dt>
        <dd>{hosts?.hosts?.length ?? 0}</dd>
      </dl>
      <div className="actions">
        <button onClick={() => Service.RevealConfig()}>Reveal definitions</button>
        <button onClick={() => Service.Reload()}>Reload definitions</button>
        <button
          onClick={() => {
            Service.ReloadSSHHosts();
            load();
          }}
        >
          Reload hosts
        </button>
      </div>
      {hosts?.warnings?.map((warning) => (
        <p className="warning" key={warning}>
          {warning}
        </p>
      ))}
    </div>
  );
}

function Binaries() {
  const [tools, setTools] = useState<{ name: string; path: string; version: string; source: string; error: string }[]>(
    [],
  );
  const [paths, setPaths] = useState<Record<string, string>>({});
  const [error, setError] = useState("");
  const load = () =>
    Service.Tools().then((items) => {
      setTools(items ?? []);
      setPaths(Object.fromEntries((items ?? []).map((item) => [item.name, item.path])));
    });
  useEffect(() => {
    void load();
  }, []);
  return (
    <div>
      <h1>Binaries</h1>
      {error && <p className="error-text">{error}</p>}
      {tools.map((tool) => (
        <article className="card" key={tool.name}>
          <h2>{tool.name}</h2>
          <p>{tool.error || `${tool.version} · ${tool.source}`}</p>
          <div className="address">
            <input
              className="mono"
              value={paths[tool.name] ?? ""}
              placeholder="Absolute executable path"
              onChange={(event) => setPaths({ ...paths, [tool.name]: event.target.value })}
            />
            <button
              onClick={() =>
                Service.SetToolPath(tool.name, paths[tool.name] ?? "")
                  .then((items) => setTools(items ?? []))
                  .catch((reason) => setError(String(reason)))
              }
            >
              Save
            </button>
            <button onClick={() => Service.SetToolPath(tool.name, "").then(() => load())}>Clear</button>
          </div>
        </article>
      ))}
    </div>
  );
}

function Shortcuts({ overview }: { overview: Overview }) {
  const [value, setValue] = useState(overview.shortcut);
  const [error, setError] = useState(overview.shortcutError);
  return (
    <div className="card">
      <h1>Shortcuts</h1>
      <label className="field">
        <span>Global quick launch</span>
        <div className="address">
          <input value={value} onChange={(event) => setValue(event.target.value)} />
          <button onClick={() => Service.SetShortcut(value).catch((reason) => setError(String(reason)))}>Save</button>
          <button
            onClick={() => {
              setValue("");
              Service.SetShortcut("");
            }}
          >
            Reset
          </button>
        </div>
      </label>
      {error && <p className="error-text">{error}</p>}
      <table>
        <tbody>
          {SHORTCUTS.map((shortcut) => (
            <tr key={shortcut.action}>
              <td>{shortcut.label}</td>
              <td>
                <kbd>{navigator.platform.startsWith("Mac") ? shortcut.mac : shortcut.other}</kbd>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function capitalize(value: string) {
  return value.charAt(0).toUpperCase() + value.slice(1);
}
function kindLabel(value: Kind) {
  return value === "ssh_passphrase"
    ? "SSH key passphrase"
    : value === "cloudflared"
      ? "Cloudflare tunnel token"
      : value === "ngrok"
        ? "ngrok authtoken"
        : "Env variable";
}
