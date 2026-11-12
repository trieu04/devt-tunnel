import { useEffect, useRef, useState } from "react";
import { TunnelType } from "../../bindings/github.com/trieu04/devt-tunnel/internal/core/model/models";
import type { Tunnel } from "../../bindings/github.com/trieu04/devt-tunnel/internal/core/model/models";
import { Service } from "../api";
import { ConfirmDialog } from "../ConfirmDialog";
import { CloudflaredFields } from "./editor/CloudflaredFields";
import { SecretOption, Field, Toggle } from "./editor/controls";
import { NgrokFields } from "./editor/NgrokFields";
import { SSHFields, SSHHost } from "./editor/SSHFields";

type Named = { id: string; name: string };
type Props = { source?: string; onDone: (id?: string) => void; profiles: Named[]; groups: Named[] };

export function EditorScreen({ source = "", onDone, profiles, groups }: Props) {
  const [draft, setDraft] = useState<Tunnel | null>(null);
  const [original, setOriginal] = useState("");
  const [paste, setPaste] = useState(source === "paste" ? "" : "");
  const [pasteMode, setPasteMode] = useState(source === "paste");
  const [warnings, setWarnings] = useState<string[]>([]);
  const [error, setError] = useState("");
  const [evaluation, setEvaluation] = useState<{
    preview: string;
    buildError: string;
    findings: { severity: string; message: string; field?: string }[];
  }>({ preview: "", buildError: "", findings: [] });
  const [lastPreview, setLastPreview] = useState("");
  const [hosts, setHosts] = useState<SSHHost[]>([]);
  const [sshConfigPath, setSSHConfigPath] = useState("~/.ssh/config");
  const [secrets, setSecrets] = useState<SecretOption[]>([]);
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [discardOpen, setDiscardOpen] = useState(false);
  const request = useRef(0);

  useEffect(() => {
    setPaste("");
    setPasteMode(source === "paste");
    const load = async () => {
      let value: Tunnel;
      if (source.startsWith("edit:")) {
        const id = source.slice(5);
        const overview = await Service.Overview();
        value = structuredClone((overview.tunnels ?? []).find((v) => v.tunnel.id === id)!.tunnel);
      } else if (source.startsWith("duplicate:")) value = await Service.DuplicateDraft(source.slice(10));
      else {
        value = await Service.NewDraft(TunnelType.TypeSSH);
        if (source.startsWith("host:")) value.ssh = { ...(value.ssh ?? {}), host: source.slice(5) };
        if (source.startsWith("name:")) value.name = source.slice(5);
      }
      setDraft(value);
      setOriginal(stable(value));
    };
    load().catch((reason) => setError(String(reason)));
    Service.ListSecrets().then((result) => setSecrets(result ?? []));
  }, [source]);

  const draftProfile = draft?.profile ?? "";
  useEffect(() => {
    Service.SSHHosts(draftProfile).then((result) => {
      setHosts(result.hosts ?? []);
      setSSHConfigPath(result.configPath || "~/.ssh/config");
    });
  }, [draftProfile]);

  useEffect(() => {
    if (!draft) return;
    const id = ++request.current;
    const timer = window.setTimeout(
      () =>
        Service.EvaluateDraft(draft).then((result) => {
          if (id !== request.current) return;
          const next = { preview: result.preview, buildError: result.buildError, findings: result.findings ?? [] };
          setEvaluation(next);
          if (next.preview) setLastPreview(next.preview);
        }),
      150,
    );
    return () => clearTimeout(timer);
  }, [draft]);

  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      const mod = event.metaKey || event.ctrlKey;
      if (mod && event.key.toLowerCase() === "s") {
        event.preventDefault();
        save(event.shiftKey);
      }
      if (event.key === "Escape" && !discardOpen) cancel();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  });

  const dirty = !!draft && stable(draft) !== original;
  useEffect(() => {
    const guard = (event: BeforeUnloadEvent) => {
      if (dirty) event.preventDefault();
    };
    window.addEventListener("beforeunload", guard);
    return () => window.removeEventListener("beforeunload", guard);
  }, [dirty]);
  const blocking =
    !!evaluation.buildError ||
    evaluation.findings.some(
      (finding) =>
        finding.severity === "error" && !finding.message.includes("missing") && !finding.message.includes("not found"),
    );
  const missingRequired = requiredFieldsMissing(draft);
  const save = async (start: boolean) => {
    if (
      !draft ||
      missingRequired ||
      evaluation.buildError ||
      (start && evaluation.findings.some((finding) => finding.severity === "error"))
    )
      return;
    setError("");
    try {
      const id = await Service.SaveTunnel(draft, start);
      onDone(id);
    } catch (reason) {
      setError(String(reason));
    }
  };
  const cancel = () => {
    if (!dirty) onDone();
    else setDiscardOpen(true);
  };
  const update = (patch: Partial<Tunnel>) => setDraft((current) => (current ? { ...current, ...patch } : current));
  const switchType = async (type: TunnelType) => {
    if (draft?.id) return;
    const fresh = await Service.NewDraft(type);
    setDraft({
      ...fresh,
      name: draft?.name ?? "",
      profile: draft?.profile,
      groups: draft?.groups,
      favorite: draft?.favorite,
      reconnect: draft?.reconnect,
      startOnLaunch: draft?.startOnLaunch,
    });
  };
  const parse = async () => {
    try {
      const result = await Service.ImportCommand(paste);
      setDraft({ ...result.tunnel, id: "", name: draft?.name ?? "Imported tunnel" });
      setWarnings(result.warnings ?? []);
      setPaste("");
      setPasteMode(false);
    } catch (reason) {
      setError(String(reason));
    }
  };

  if (!draft)
    return (
      <main className="screen">
        <p>Loading editor…</p>
      </main>
    );
  return (
    <main className="screen editor">
      <header className="screen-header page-header">
        <div className="page-title">
          <span className="breadcrumb">
            <button className="breadcrumb-button" onClick={cancel}>
              Tunnels
            </button>{" "}
            / {draft.name || "New tunnel"}
          </span>
          <h1>{draft.id ? "Edit tunnel" : "New tunnel"}</h1>
        </div>
        <div className="actions">
          {dirty && <span className="unsaved-pill">Unsaved changes</span>}
          <button onClick={cancel}>Cancel</button>
          <button className="outline-strong" onClick={() => save(false)} disabled={missingRequired || blocking}>
            Save
          </button>
          <button
            className="primary"
            onClick={() => save(true)}
            disabled={missingRequired || evaluation.findings.some((finding) => finding.severity === "error")}
          >
            ▷ Save & start
          </button>
        </div>
      </header>
      {error && (
        <div className="banner banner-error" role="alert">
          {error}
        </div>
      )}
      <div className="editor-layout">
        <div className="editor-form">
          <section className="card source-card">
            <div className="source-row">
              <strong>Paste a command</strong>
              <input
                className="mono"
                value={paste}
                onChange={(event) => {
                  setPaste(event.target.value);
                  setPasteMode(true);
                }}
                placeholder="ssh -N -L 8080:localhost:80 server1"
              />
              <button className="outline-strong" onClick={parse} disabled={!paste.trim()}>
                Fill form
              </button>
            </div>
            <p className="muted">
              Parses ssh, ngrok and cloudflared commands into the fields below. Nothing runs until you start the tunnel.
            </p>
            {warnings.map((warning) => (
              <p className="warning" key={warning}>
                {warning}
              </p>
            ))}
          </section>
          {(!pasteMode || !paste.trim()) && (
            <>
              <section className="card">
                <h2>Type</h2>
                <div className="segments type-segments">
                  {TYPE_OPTIONS.map(([type, label]) => (
                    <button
                      key={type}
                      aria-pressed={draft.type === type}
                      disabled={!!draft.id}
                      onClick={() => switchType(type)}
                    >
                      {label}
                    </button>
                  ))}
                </div>
              </section>
              <section className="card grid basics-card">
                <h2>Basics</h2>
                <Field label="Name" invalid={!draft.name.trim()}>
                  <input
                    value={draft.name}
                    required
                    aria-invalid={!draft.name.trim()}
                    onChange={(event) => update({ name: event.target.value })}
                  />
                </Field>
                <Field label="Profile">
                  <select value={draft.profile ?? ""} onChange={(event) => update({ profile: event.target.value })}>
                    <option value="">No profile (default SSH config)</option>
                    {profiles.map((profile) => (
                      <option key={profile.id} value={profile.id}>
                        {profile.name}
                      </option>
                    ))}
                  </select>
                </Field>
                <div className="field full">
                  <span className="field-label">Groups</span>
                  <div className="profile-options">
                    {groups.length ? (
                      groups.map((group) => {
                        const pressed = (draft.groups ?? []).includes(group.id);
                        return (
                          <button
                            key={group.id}
                            aria-pressed={pressed}
                            onClick={() =>
                              update({
                                groups: pressed
                                  ? (draft.groups ?? []).filter((id) => id !== group.id)
                                  : [...(draft.groups ?? []), group.id],
                              })
                            }
                          >
                            {pressed ? "✓ " : "+ "}
                            {group.name}
                          </button>
                        );
                      })
                    ) : (
                      <span className="muted">No groups yet</span>
                    )}
                  </div>
                </div>
                <div className="full toggle-row">
                  <Toggle
                    label="Favorite (shows in tray)"
                    value={!!draft.favorite}
                    onChange={(favorite) => update({ favorite })}
                  />
                  <Toggle
                    label="Start when devt-tunnel opens"
                    value={!!draft.startOnLaunch}
                    onChange={(startOnLaunch) => update({ startOnLaunch })}
                  />
                  <Toggle
                    label="Reconnect automatically"
                    value={!!draft.reconnect}
                    onChange={(reconnect) => update({ reconnect })}
                  />
                </div>
              </section>
              {draft.type === "ssh" && (
                <SSHFields
                  draft={draft}
                  update={update}
                  hosts={hosts}
                  configPath={sshConfigPath}
                  secrets={secrets}
                  onHostsReloaded={(items, path) => {
                    setHosts(items);
                    setSSHConfigPath(path);
                  }}
                />
              )}
              {draft.type === "ngrok" && <NgrokFields draft={draft} update={update} secrets={secrets} />}
              {draft.type === "cloudflared" && <CloudflaredFields draft={draft} update={update} secrets={secrets} />}
              {draft.type === "command" && (
                <section className="card grid">
                  <h2>Command</h2>
                  <Field label="Command line" invalid={!draft.command?.line.trim()}>
                    <textarea
                      value={draft.command?.line ?? ""}
                      required
                      aria-invalid={!draft.command?.line.trim()}
                      onChange={(event) => update({ command: { ...draft.command!, line: event.target.value } })}
                    />
                  </Field>
                  <Field label="Working directory">
                    <input
                      value={draft.command?.dir ?? ""}
                      onChange={(event) => update({ command: { ...draft.command!, dir: event.target.value } })}
                    />
                  </Field>
                  <Field label="Ready log regex">
                    <input
                      value={draft.command?.readyLog ?? ""}
                      onChange={(event) => update({ command: { ...draft.command!, readyLog: event.target.value } })}
                    />
                  </Field>
                </section>
              )}
            </>
          )}
          {draft.id && (
            <section className="danger-zone card">
              <div>
                <strong>Delete tunnel</strong>
                <p className="muted">This removes the definition but keeps referenced secrets in the Vault.</p>
              </div>
              <button className="danger" onClick={() => setDeleteOpen(true)}>
                Delete “{draft.name}”
              </button>
            </section>
          )}
        </div>
        <aside className="editor-aside">
          <section className="card">
            <div className="section-line">
              <h2>Command preview</h2>
              <button
                className="small-button"
                onClick={() => navigator.clipboard.writeText(evaluation.preview || lastPreview)}
              >
                Copy
              </button>
            </div>
            <pre className={`mono command command-dark ${evaluation.buildError ? "dim" : ""}`}>
              {evaluation.preview || lastPreview || "Complete the required fields."}
            </pre>
            {evaluation.buildError && <p className="error-text">{evaluation.buildError}</p>}
            <p className="muted preview-note">Updates as you edit. This is exactly what devt-tunnel runs.</p>
          </section>
          <section className="card">
            <h2>Checks</h2>
            <ul className="findings">
              {evaluation.findings.map((finding) => (
                <li
                  className={
                    finding.severity === "error" ? "error-text" : finding.severity === "ok" ? "ok-text" : "warning"
                  }
                  key={`${finding.field}:${finding.message}`}
                >
                  {finding.message}
                </li>
              ))}
            </ul>
          </section>
          <section className="storage-note">
            <strong>Where this is stored</strong>
            <span>
              Tunnel definition: <span className="mono">~/.config/devt-tunnel/tunnels.toml</span>
            </span>
            <span>Keys, tokens and passphrases: OS keychain, never in the file</span>
          </section>
        </aside>
      </div>
      <ConfirmDialog
        open={deleteOpen}
        title={`Delete ${draft.name}?`}
        description="This removes the tunnel definition and stops it if it is running. Referenced secrets stay in the Vault."
        confirmLabel="Delete tunnel"
        busyLabel="Deleting…"
        onCancel={() => setDeleteOpen(false)}
        onConfirm={async () => {
          await Service.DeleteTunnel(draft.id);
          onDone();
        }}
      />
      <ConfirmDialog
        open={discardOpen}
        title="Discard unsaved changes?"
        description="Your tunnel edits will be lost."
        confirmLabel="Discard changes"
        onCancel={() => setDiscardOpen(false)}
        onConfirm={async () => {
          setDiscardOpen(false);
          onDone();
        }}
      />
    </main>
  );
}

const TYPE_OPTIONS: [TunnelType, string][] = [
  [TunnelType.TypeSSH, "SSH"],
  [TunnelType.TypeCloudflared, "Cloudflared"],
  [TunnelType.TypeNgrok, "ngrok"],
  [TunnelType.TypeCommand, "Command"],
];

function requiredFieldsMissing(draft: Tunnel | null): boolean {
  if (!draft) return true;
  if (!draft.name.trim()) return true;
  if (draft.type === TunnelType.TypeSSH) {
    if (!draft.ssh?.host?.trim() && !draft.ssh?.hostname?.trim()) return true;
    return (draft.forwards ?? []).some(
      (forward) => !forward.bind.trim() || (forward.kind !== "D" && !forward.target?.trim()),
    );
  }
  if (draft.type === TunnelType.TypeNgrok) return !draft.ngrok?.proto?.trim() || !draft.ngrok?.addr?.trim();
  if (draft.type === TunnelType.TypeCloudflared)
    return !draft.cloudflared?.name?.trim() && !draft.cloudflared?.secret?.trim() && !draft.cloudflared?.url?.trim();
  if (draft.type === TunnelType.TypeCommand) return !draft.command?.line?.trim();
  return true;
}

function stable(value: unknown): string {
  return JSON.stringify(value);
}
