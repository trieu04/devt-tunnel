import { FormEvent, useCallback, useEffect, useState } from "react";
import type { Lease } from "../../bindings/github.com/trieu04/devt-tunnel/internal/core/model";
import type { Agent } from "../../bindings/github.com/trieu04/devt-tunnel/internal/core/settings";
import { onTunnelsChanged, Service } from "../api";
import { formatTimeLeft } from "../AgentBadge";
import { leaseDeadline, leaseIsActive } from "../lib/leases";
import { ConfirmDialog } from "../ConfirmDialog";

const lines = (value: string) =>
  value
    .split("\n")
    .map((item) => item.trim())
    .filter(Boolean);

export function AgentsSettings() {
  const [policy, setPolicy] = useState<Agent | null>(null);
  const [tunnels, setTunnels] = useState("");
  const [hosts, setHosts] = useState("");
  const [leases, setLeases] = useState<Lease[]>([]);
  const [revoking, setRevoking] = useState<Lease | null>(null);
  const [now, setNow] = useState(Date.now());
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [leaseError, setLeaseError] = useState("");
  const [saved, setSaved] = useState(false);
  const loadPolicy = useCallback(async () => {
    try {
      const next = await Service.AgentSettings();
      setPolicy(next);
      setTunnels((next.allowTunnels ?? []).join("\n"));
      setHosts((next.allowSSHHosts ?? []).join("\n"));
      setError("");
      setSaved(false);
    } catch (reason) {
      setError(String(reason));
    }
  }, []);
  const loadLeases = useCallback(async () => {
    try {
      setLeases((await Service.ListLeases()) ?? []);
      setLeaseError("");
    } catch (reason) {
      setLeaseError(String(reason));
    }
  }, []);
  useEffect(() => {
    void loadPolicy();
    void loadLeases();
    const off = onTunnelsChanged(() => void loadLeases());
    const poll = window.setInterval(() => void loadLeases(), 5000);
    const tick = window.setInterval(() => setNow(Date.now()), 1000);
    return () => {
      off();
      clearInterval(poll);
      clearInterval(tick);
    };
  }, [loadPolicy, loadLeases]);
  const change = (next: Partial<Agent>) => {
    setPolicy((current) => current && { ...current, ...next });
    setSaved(false);
  };
  const save = async (event: FormEvent) => {
    event.preventDefault();
    if (!policy || busy) return;
    setBusy(true);
    setError("");
    setSaved(false);
    try {
      const next = { ...policy, allowTunnels: lines(tunnels), allowSSHHosts: lines(hosts) };
      await Service.SetAgentSettings(next);
      setPolicy(next);
      setSaved(true);
    } catch (reason) {
      setError(String(reason));
    } finally {
      setBusy(false);
    }
  };
  const active = leases.filter((lease) => leaseIsActive(lease, now));
  return (
    <div className="agents-settings">
      <form className="card agent-policy" onSubmit={save}>
        <div className="section-line">
          <h2>Agent policy</h2>
          <button type="button" disabled={busy} onClick={() => void loadPolicy()}>
            Reload policy
          </button>
        </div>
        <p className="muted">
          Per-machine permissions for MCP clients. These controls are not a security boundary against a client with
          shell access. Existing leases can be revoked below.
        </p>
        {error && (
          <p className="error-text" role="alert">
            {error}
          </p>
        )}
        {!policy ? (
          <p>Loading policy…</p>
        ) : (
          <>
            <fieldset disabled={busy} className="agent-policy-fields">
              <label className="check">
                <input
                  type="checkbox"
                  checked={policy.enabled}
                  onChange={(event) => change({ enabled: event.target.checked })}
                />
                Enable agent mutations
              </label>
              <label className="field">
                <span>Public exposure</span>
                <select value={policy.public} onChange={(event) => change({ public: event.target.value })}>
                  <option value="ask">Ask a person every time</option>
                  <option value="deny">Deny</option>
                </select>
                <small>
                  Includes ngrok, cloudflared, remote forwards and non-loopback listeners. Public exposure can never be
                  always allowed.
                </small>
              </label>
              <div className="grid">
                <label className="field">
                  <span>Allowed saved tunnels</span>
                  <textarea
                    rows={4}
                    className="mono"
                    value={tunnels}
                    onChange={(event) => {
                      setTunnels(event.target.value);
                      setSaved(false);
                    }}
                    placeholder="dev-*"
                  />
                  <small>One tunnel ID or glob per line. Empty means ask.</small>
                </label>
                <label className="field">
                  <span>Allowed SSH hosts</span>
                  <textarea
                    rows={4}
                    className="mono"
                    value={hosts}
                    onChange={(event) => {
                      setHosts(event.target.value);
                      setSaved(false);
                    }}
                    placeholder="staging"
                  />
                  <small>One literal SSH config alias per line, for safe loopback forwards only.</small>
                </label>
              </div>
              <div className="grid">
                <label className="field">
                  <span>Automatic port range</span>
                  <input
                    value={policy.portRange}
                    onChange={(event) => change({ portRange: event.target.value })}
                    placeholder="20000-20999"
                    required
                  />
                  <small>Inclusive range, between 1 and 65535.</small>
                </label>
                <label className="field">
                  <span>Default lease TTL</span>
                  <input
                    value={policy.defaultTTL}
                    onChange={(event) => change({ defaultTTL: event.target.value })}
                    placeholder="30m"
                    required
                  />
                  <small>Go duration, for example 30m or 1h30m.</small>
                </label>
                <label className="field">
                  <span>Maximum lease TTL</span>
                  <input
                    value={policy.maxTTL}
                    onChange={(event) => change({ maxTTL: event.target.value })}
                    placeholder="8h"
                    required
                  />
                  <small>Must be at least the default TTL.</small>
                </label>
              </div>
            </fieldset>
            <div className="actions">
              <button type="submit" className="primary" disabled={busy}>
                {busy ? "Saving…" : "Save policy"}
              </button>
              {saved && <span role="status">Policy saved</span>}
            </div>
          </>
        )}
      </form>
      <section className="card agent-leases">
        <div className="section-line">
          <h2>Active leases</h2>
          <button onClick={() => void loadLeases()}>Refresh leases</button>
        </div>
        <p className="muted">
          Revoking the last claim stops an unpinned tunnel only if a lease started it. Person-pinned tunnels keep
          running.
        </p>
        {leaseError && (
          <p className="error-text" role="alert">
            {leaseError}
          </p>
        )}
        <div className="table-scroll">
          <table className="vault-table">
            <thead>
              <tr>
                <th>Holder</th>
                <th>Tunnel</th>
                <th>Time left</th>
                <th>
                  <span className="sr">Actions</span>
                </th>
              </tr>
            </thead>
            <tbody>
              {active.map((lease) => (
                <tr key={lease.id}>
                  <td>
                    {lease.holder.name}
                    <small className="muted"> ({lease.holder.kind})</small>
                  </td>
                  <td className="mono">{lease.tunnelId}</td>
                  <td>
                    {leaseDeadline(lease) ? formatTimeLeft(leaseDeadline(lease), now) : "No expiry"}
                    {lease.detached && " · detached"}
                    {lease.graceUntil && " · disconnected"}
                  </td>
                  <td>
                    <button className="danger" onClick={() => setRevoking(lease)}>
                      Revoke
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
        {!active.length && <p className="muted">No active leases.</p>}
      </section>
      <ConfirmDialog
        open={!!revoking}
        title="Revoke lease?"
        description={`Release ${revoking?.holder.name ?? ""}'s claim on ${revoking?.tunnelId ?? ""}. This may stop the tunnel.`}
        confirmLabel="Revoke"
        onCancel={() => setRevoking(null)}
        onConfirm={async () => {
          if (revoking) await Service.RevokeLease(revoking.id);
          await loadLeases();
        }}
      />
    </div>
  );
}
