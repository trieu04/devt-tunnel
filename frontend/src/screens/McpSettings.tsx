import { FormEvent, useCallback, useEffect, useState } from "react";
import type { Agent } from "../../bindings/github.com/trieu04/devt-tunnel/internal/core/settings";
import { onApprovalsChanged, onTunnelsChanged, Service } from "../api";
import { ConfirmDialog } from "../ConfirmDialog";
import type { MCPCall } from "../../bindings/github.com/trieu04/devt-tunnel/internal/host";

const lines = (value: string) =>
  value
    .split("\n")
    .map((item) => item.trim())
    .filter(Boolean);

export const HISTORY_PAGE_SIZE = 100;
const CLEARED_KEY = "mcp-history-cleared-seq";

export function pageCount(total: number, size = HISTORY_PAGE_SIZE): number {
  return Math.max(1, Math.ceil(total / size));
}

export function pageSlice<T>(items: T[], page: number, size = HISTORY_PAGE_SIZE): T[] {
  const index = Math.min(Math.max(page, 0), pageCount(items.length, size) - 1);
  return items.slice(index * size, (index + 1) * size);
}

// Newest-first rows, as the table shows them. The audit trail itself is never
// modified: "clear" only hides rows up to a sequence number in this browser.
export function visibleCalls(calls: MCPCall[], clearedSeq: number): MCPCall[] {
  return calls.filter((call) => call.seq > clearedSeq);
}

const readClearedSeq = (): number => {
  try {
    return Number(localStorage.getItem(CLEARED_KEY)) || 0;
  } catch {
    return 0;
  }
};

// Audit actions in the words an MCP client uses.
const CALL_LABELS: Record<string, string> = {
  "tunnel.open": "open tunnel",
  "tunnel.release": "close tunnel",
  // Entries written before holders replaced leases keep their wording.
  "lease.open": "open tunnel",
  "lease.close": "close tunnel",
  "tunnel.runEphemeral": "create tunnel",
  "tunnel.promote": "save tunnel",
  "approval.request": "needs approval",
  "approval.timeout": "approval timed out",
  "policy.deny": "refused by policy",
};

export const callLabel = (action: string) => CALL_LABELS[action] ?? action;

export function formatCallTime(value: string): string {
  const time = Date.parse(value);
  return Number.isFinite(time) ? new Date(time).toLocaleString() : value;
}

export function McpSettings() {
  const [policy, setPolicy] = useState<Agent | null>(null);
  const [tunnels, setTunnels] = useState("");
  const [hosts, setHosts] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
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
  const [history, setHistory] = useState<MCPCall[]>([]);
  const [historyError, setHistoryError] = useState("");
  const [page, setPage] = useState(0);
  const [clearedSeq, setClearedSeq] = useState(readClearedSeq);
  const [clearOpen, setClearOpen] = useState(false);
  const loadHistory = useCallback(async () => {
    try {
      setHistory((await Service.MCPHistory()) ?? []);
      setHistoryError("");
    } catch (reason) {
      setHistoryError(String(reason));
    }
  }, []);
  useEffect(() => {
    void loadPolicy();
    void loadHistory();
    const offTunnels = onTunnelsChanged(() => void loadHistory());
    const offApprovals = onApprovalsChanged(() => void loadHistory());
    const poll = window.setInterval(() => void loadHistory(), 5000);
    return () => {
      offTunnels();
      offApprovals();
      clearInterval(poll);
    };
  }, [loadPolicy, loadHistory]);
  const rows = visibleCalls(history, clearedSeq);
  const pages = pageCount(rows.length);
  const current = Math.min(page, pages - 1);
  const saveHistory = () => {
    const url = URL.createObjectURL(new Blob([JSON.stringify(rows, null, 2)], { type: "application/json" }));
    const link = document.createElement("a");
    link.href = url;
    link.download = `mcp-call-history-${new Date().toISOString().replace(/[:.]/g, "-")}.json`;
    link.click();
    window.setTimeout(() => URL.revokeObjectURL(url), 0);
  };
  const clearHistory = () => {
    const newest = history.reduce((max, call) => Math.max(max, call.seq), clearedSeq);
    try {
      localStorage.setItem(CLEARED_KEY, String(newest));
    } catch {
      // The view is still cleared for this session.
    }
    setClearedSeq(newest);
    setPage(0);
    setClearOpen(false);
  };
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
  return (
    <div className="mcp-settings">
      <form className="card mcp-policy" onSubmit={save}>
        <div className="section-line">
          <h2>MCP policy</h2>
          <button type="button" disabled={busy} onClick={() => void loadPolicy()}>
            Reload policy
          </button>
        </div>
        <p className="muted">
          Per-machine permissions for MCP clients. These controls are not a security boundary against a client with
          shell access. Tunnels opened through MCP show an MCP badge on the Tunnels screen, where you can stop them.
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
            <fieldset disabled={busy} className="mcp-policy-fields">
              <label className="check">
                <input
                  type="checkbox"
                  checked={policy.enabled}
                  onChange={(event) => change({ enabled: event.target.checked })}
                />
                Enable MCP changes
              </label>
              <label className="field">
                <span>Public exposure</span>
                <select value={policy.public} onChange={(event) => change({ public: event.target.value })}>
                  <option value="ask">Ask a person every time</option>
                  <option value="deny">Deny</option>
                  <option value="allow">Allow without asking</option>
                </select>
                <small>
                  Includes ngrok, cloudflared, remote forwards and non-loopback listeners. "Allow" lets MCP clients
                  publish without approval; use with care.
                </small>
              </label>
              <p className="muted">
                Saved tunnels and SSH hosts listed here are opened by MCP clients automatically, without asking you to
                approve. Anything not listed needs your approval each time.
              </p>
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
                  <small>One tunnel ID or glob per line, for example dev-*. * allows every saved tunnel.</small>
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
                  <small>
                    One SSH config alias per line, for safe loopback forwards only. * allows every alias in your SSH
                    config.
                  </small>
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
      <section className="card mcp-history">
        <div className="section-line">
          <h2>MCP call history</h2>
          <div className="actions">
            <button onClick={() => void loadHistory()}>Refresh</button>
            <button onClick={saveHistory} disabled={rows.length === 0}>
              Save log
            </button>
            <button onClick={() => setClearOpen(true)} disabled={rows.length === 0}>
              Clear log
            </button>
          </div>
        </div>
        <p className="muted">
          Calls from MCP clients that open or close tunnels, ask for approval or were refused by policy. Read-only calls
          such as listing tunnels are not recorded. Newest first, {HISTORY_PAGE_SIZE} per page.
        </p>
        {historyError && (
          <p className="error-text" role="alert">
            {historyError}
          </p>
        )}
        {rows.length === 0 ? (
          <p className="muted">No MCP calls recorded yet.</p>
        ) : (
          <div className="table-scroll">
            <table className="vault-table">
              <thead>
                <tr>
                  <th>Time</th>
                  <th>Client</th>
                  <th>Call</th>
                  <th>Tunnel</th>
                  <th>Result</th>
                </tr>
              </thead>
              <tbody>
                {pageSlice(rows, current).map((call) => (
                  <tr key={call.seq}>
                    <td>{formatCallTime(call.time)}</td>
                    <td>{call.client}</td>
                    <td className="mono">{callLabel(call.action)}</td>
                    <td className="mono">{call.tunnelId || "—"}</td>
                    <td className={`mcp-result mcp-result-${call.result}`}>
                      {call.result.replace(/_/g, " ")}
                      {call.count > 1 && ` ×${call.count}`}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
        {rows.length > HISTORY_PAGE_SIZE && (
          <div className="actions">
            <button onClick={() => setPage(current - 1)} disabled={current === 0}>
              Previous
            </button>
            <span role="status">
              Page {current + 1} of {pages} · {rows.length} calls
            </span>
            <button onClick={() => setPage(current + 1)} disabled={current >= pages - 1}>
              Next
            </button>
          </div>
        )}
      </section>
      <ConfirmDialog
        open={clearOpen}
        title="Clear MCP call history?"
        description="This hides the current entries from this list. The audit trail on disk is not deleted, and `devt-tunnel audit` still shows it. Save the log first if you want a copy."
        confirmLabel="Clear log"
        busyLabel="Clearing…"
        onCancel={() => setClearOpen(false)}
        onConfirm={async () => clearHistory()}
      />
    </div>
  );
}
