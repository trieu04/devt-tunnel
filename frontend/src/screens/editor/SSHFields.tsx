import { useEffect, useRef, useState } from "react";
import { ForwardKind } from "../../../bindings/github.com/trieu04/devt-tunnel/internal/core/model/models";
import type { Tunnel } from "../../../bindings/github.com/trieu04/devt-tunnel/internal/core/model/models";
import { Service } from "../../api";
import { Field, SecretOption, SecretSelect } from "./controls";

export type SSHHost = { alias: string; source: string };
export type ResolvedHost = {
  HostName: string;
  User: string;
  Port: string;
  ProxyJump: string;
  IdentityFiles: string[] | null;
};

export function SSHFields({
  draft,
  update,
  hosts,
  configPath,
  secrets,
  onHostsReloaded,
}: {
  draft: Tunnel;
  update: (patch: Partial<Tunnel>) => void;
  hosts: SSHHost[];
  configPath: string;
  secrets: SecretOption[];
  onHostsReloaded: (hosts: SSHHost[], path: string) => void;
}) {
  const ssh = draft.ssh ?? {};
  const [connection, setConnection] = useState<"config" | "manual">(ssh.host || !ssh.hostname ? "config" : "manual");
  const [resolved, setResolved] = useState<ResolvedHost | null>(null);
  const [resolveError, setResolveError] = useState("");
  const [reloading, setReloading] = useState(false);
  const configHost = useRef(ssh.host ?? "");
  const manualConnection = useRef({
    user: ssh.user ?? "",
    hostname: ssh.hostname ?? "",
    port: ssh.port || 22,
    identity: ssh.identity ?? "",
    passphraseSecret: ssh.passphraseSecret ?? "",
  });
  const change = (patch: Record<string, unknown>) => update({ ssh: { ...ssh, ...patch } });
  const changeManual = (patch: Partial<typeof manualConnection.current>) => {
    manualConnection.current = { ...manualConnection.current, ...patch };
    change(patch);
  };
  const flags = ssh.flags ?? [];
  const options = ssh.options ?? {};

  useEffect(() => {
    if (connection !== "config" || !ssh.host) {
      setResolved(null);
      setResolveError("");
      return;
    }
    let active = true;
    Service.ResolveSSHHost(ssh.host, "")
      .then((value) => {
        if (active) {
          setResolved(value);
          setResolveError("");
        }
      })
      .catch((reason) => {
        if (active) {
          setResolved(null);
          setResolveError(String(reason));
        }
      });
    return () => {
      active = false;
    };
  }, [connection, ssh.host]);

  const chooseConnection = (next: "config" | "manual") => {
    if (connection === "config") configHost.current = ssh.host ?? "";
    else
      manualConnection.current = {
        user: ssh.user ?? "",
        hostname: ssh.hostname ?? "",
        port: ssh.port || 22,
        identity: ssh.identity ?? "",
        passphraseSecret: ssh.passphraseSecret ?? "",
      };
    setConnection(next);
    if (next === "config")
      change({
        host: configHost.current || hosts[0]?.alias || "",
        user: "",
        hostname: "",
        port: 0,
        identity: "",
        passphraseSecret: "",
      });
    else change({ host: "", ...manualConnection.current });
  };
  const reloadHosts = async () => {
    setReloading(true);
    try {
      await Service.ReloadSSHHosts();
      const result = await Service.SSHHosts("");
      onHostsReloaded(result.hosts ?? [], result.configPath || configPath);
    } finally {
      setReloading(false);
    }
  };
  const addForward = (kind: ForwardKind) => {
    const forward =
      kind === ForwardKind.ForwardDynamic
        ? { kind, bind: "127.0.0.1:1080" }
        : kind === ForwardKind.ForwardRemote
          ? { kind, bind: "0.0.0.0:9000", target: "localhost:3000" }
          : { kind, bind: "127.0.0.1:5432", target: "localhost:5432" };
    update({ forwards: [...(draft.forwards ?? []), forward] });
  };
  const updateForward = (index: number, patch: Record<string, unknown>) => {
    const next = [...(draft.forwards ?? [])];
    next[index] = { ...next[index], ...patch };
    update({ forwards: next });
  };
  const flagInfo = [
    ["-N", "No remote command"],
    ["-C", "Compress traffic"],
    ["-T", "No TTY"],
    ["-A", "Forward ssh-agent"],
    ["-q", "Quiet"],
    ["-v", "Verbose log"],
  ];

  return (
    <section className="card ssh-card">
      <div className="section-heading">
        <div>
          <h2>Connection</h2>
          <p className="muted">Runs your system ssh, so agent, ProxyJump and Match blocks keep working.</p>
        </div>
      </div>
      <div className="connection-options" role="radiogroup" aria-label="SSH connection source">
        <button
          type="button"
          role="radio"
          aria-checked={connection === "config"}
          onClick={() => chooseConnection("config")}
        >
          <span className="radio-dot" />
          <span>
            <strong>
              Host from <span className="mono">~/.ssh/config</span>
            </strong>
            <small>Use aliases, Match blocks and ProxyJump</small>
          </span>
        </button>
        <button
          type="button"
          role="radio"
          aria-checked={connection === "manual"}
          onClick={() => chooseConnection("manual")}
        >
          <span className="radio-dot" />
          <span>
            <strong>
              Manual <span className="muted">(user@host)</span>
            </strong>
            <small>Enter connection details directly</small>
          </span>
        </button>
      </div>
      {connection === "config" ? (
        <div className="connection-fields">
          <Field label="SSH host">
            <select
              value={ssh.host ?? ""}
              onChange={(event) => {
                configHost.current = event.target.value;
                change({ host: event.target.value, hostname: "" });
              }}
            >
              <option value="">Select a host…</option>
              {hosts.map((host) => (
                <option key={`${host.source}:${host.alias}`} value={host.alias}>
                  {host.alias}
                </option>
              ))}
            </select>
          </Field>
          <div className="ssh-config-meta">
            <span>
              {hosts.length} host{hosts.length === 1 ? "" : "s"} from <span className="mono">{configPath}</span>
            </span>
            <button type="button" className="small-button" disabled={reloading} onClick={reloadHosts}>
              {reloading ? "Reloading…" : "↻ Reload"}
            </button>
          </div>
          {resolved && (
            <div className="resolved-host">
              <span>
                <small>HostName</small>
                <strong>{resolved.HostName || "—"}</strong>
              </span>
              <span>
                <small>User</small>
                <strong>{resolved.User || "—"}</strong>
              </span>
              <span>
                <small>Port</small>
                <strong>{resolved.Port || "22"}</strong>
              </span>
              <span>
                <small>IdentityFile</small>
                <strong className="mono">{resolved.IdentityFiles?.join(", ") || "Agent / default"}</strong>
              </span>
              {resolved.ProxyJump && resolved.ProxyJump !== "none" && (
                <span className="wide">
                  <small>ProxyJump</small>
                  <strong>{resolved.ProxyJump}</strong>
                </span>
              )}
            </div>
          )}
          {resolveError && <p className="error-text">Could not resolve this host: {resolveError}</p>}
        </div>
      ) : (
        <div className="connection-fields manual-grid">
          <Field label="User">
            <input
              autoComplete="username"
              value={ssh.user ?? ""}
              onChange={(event) => changeManual({ user: event.target.value })}
              placeholder="deploy"
            />
          </Field>
          <Field label="Host">
            <input
              value={ssh.hostname ?? ""}
              onChange={(event) => changeManual({ hostname: event.target.value })}
              placeholder="server.example.com"
            />
          </Field>
          <Field label="Port">
            <input
              type="number"
              min="1"
              max="65535"
              value={ssh.port || 22}
              onChange={(event) => changeManual({ port: Number(event.target.value) || 22 })}
            />
          </Field>
          <Field label="Identity file">
            <input
              value={ssh.identity ?? ""}
              onChange={(event) => changeManual({ identity: event.target.value })}
              placeholder="~/.ssh/id_ed25519"
            />
          </Field>
          <Field label="Key passphrase">
            <SecretSelect
              value={ssh.passphraseSecret ?? ""}
              kind="ssh_passphrase"
              secrets={secrets}
              emptyLabel="None / use agent"
              onChange={(passphraseSecret) => changeManual({ passphraseSecret })}
            />
          </Field>
        </div>
      )}
      <div className="form-divider" />
      <div className="section-line">
        <div>
          <h2>Port forwards</h2>
          <p className="muted">Add local, remote or dynamic SOCKS forwarding.</p>
        </div>
        <div className="forward-actions">
          <button type="button" onClick={() => addForward(ForwardKind.ForwardLocal)}>
            + -L local
          </button>
          <button type="button" onClick={() => addForward(ForwardKind.ForwardRemote)}>
            + -R remote
          </button>
          <button type="button" onClick={() => addForward(ForwardKind.ForwardDynamic)}>
            + -D SOCKS
          </button>
        </div>
      </div>
      <div className="forward-list">
        {(draft.forwards ?? []).map((forward, index) => (
          <ForwardEditor
            key={index}
            forward={forward}
            onChange={(patch) => updateForward(index, patch)}
            onDelete={() => update({ forwards: (draft.forwards ?? []).filter((_, n) => n !== index) })}
          />
        ))}
        {!(draft.forwards ?? []).length && <div className="inline-empty">No port forwards yet.</div>}
      </div>
      <div className="form-divider" />
      <div>
        <h2>SSH flags</h2>
        <div className="flag-grid">
          {flagInfo.map(([flag, description]) => (
            <label className="flag-option" key={flag} data-checked={flags.includes(flag)}>
              <input
                type="checkbox"
                checked={flags.includes(flag)}
                onChange={(event) =>
                  change({ flags: event.target.checked ? [...flags, flag] : flags.filter((value) => value !== flag) })
                }
              />
              <span>
                <strong className="mono">{flag}</strong>
                <small>{description}</small>
              </span>
            </label>
          ))}
        </div>
      </div>
      <div className="options-grid">
        <Field label="ServerAliveInterval">
          <input
            type="number"
            min="0"
            value={options.ServerAliveInterval ?? ""}
            onChange={(event) => change({ options: { ...options, ServerAliveInterval: event.target.value } })}
            placeholder="30"
          />
        </Field>
        <Field label="ExitOnForwardFailure">
          <select
            value={options.ExitOnForwardFailure ?? "yes"}
            onChange={(event) => change({ options: { ...options, ExitOnForwardFailure: event.target.value } })}
          >
            <option value="yes">yes</option>
            <option value="no">no</option>
          </select>
        </Field>
        <Field label="Extra arguments">
          <input
            value={ssh.extraArgs ?? ""}
            onChange={(event) => change({ extraArgs: event.target.value })}
            placeholder="-o TCPKeepAlive=yes"
          />
        </Field>
      </div>
    </section>
  );
}

function ForwardEditor({
  forward,
  onChange,
  onDelete,
}: {
  forward: NonNullable<Tunnel["forwards"]>[number];
  onChange: (patch: Record<string, unknown>) => void;
  onDelete: () => void;
}) {
  const [bindHost, bindPort] = splitEndpoint(forward.bind);
  const [targetHost, targetPort] = splitEndpoint(forward.target ?? "");
  const dynamic = forward.kind === "D";
  return (
    <div className="forward-edit">
      <div className="forward-kind">
        <select
          aria-label="Forward type"
          value={forward.kind}
          onChange={(event) =>
            onChange({
              kind: event.target.value as ForwardKind,
              target: event.target.value === "D" ? "" : forward.target || "localhost:3000",
            })
          }
        >
          <option value="L">-L Local</option>
          <option value="R">-R Remote</option>
          <option value="D">-D SOCKS</option>
        </select>
      </div>
      <div className="endpoint-fields">
        <Field label="Bind host">
          <input
            value={bindHost}
            onChange={(event) => onChange({ bind: joinEndpoint(event.target.value, bindPort) })}
            placeholder="127.0.0.1"
          />
        </Field>
        <Field label="Bind port">
          <input
            inputMode="numeric"
            value={bindPort}
            onChange={(event) => onChange({ bind: joinEndpoint(bindHost, event.target.value) })}
            placeholder={dynamic ? "1080" : "5432"}
          />
        </Field>
        {!dynamic && (
          <>
            <span className="forward-arrow">→</span>
            <Field label="Target host">
              <input
                value={targetHost}
                onChange={(event) => onChange({ target: joinEndpoint(event.target.value, targetPort) })}
                placeholder="localhost"
              />
            </Field>
            <Field label="Target port">
              <input
                inputMode="numeric"
                value={targetPort}
                onChange={(event) => onChange({ target: joinEndpoint(targetHost, event.target.value) })}
                placeholder="5432"
              />
            </Field>
          </>
        )}
      </div>
      <button type="button" className="icon-button danger-ghost" aria-label="Delete forward" onClick={onDelete}>
        ×
      </button>
      {dynamic && <p className="forward-help">Creates a local SOCKS proxy. No target is required.</p>}
    </div>
  );
}

function splitEndpoint(value: string): [string, string] {
  const bracketed = value.match(/^(\[[^\]]+\]):(.+)$/);
  if (bracketed) return [bracketed[1], bracketed[2]];
  const separator = value.lastIndexOf(":");
  if (separator < 0) return ["", value];
  return [value.slice(0, separator), value.slice(separator + 1)];
}

function joinEndpoint(host: string, port: string): string {
  return host ? `${host}:${port}` : port;
}
