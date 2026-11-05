import { useEffect, useRef, useState } from "react";
import type { Approval, DecideApprovalParams } from "../bindings/github.com/trieu04/devt-tunnel/internal/api";
import { Service, onApprovalsChanged, onTunnelsChanged } from "./api";
import { formatTimeLeft } from "./AgentBadge";

export function approvalDecision(request: Approval, decision: "approve" | "deny", scope: string): DecideApprovalParams {
  // Person clients receive the code. The explicit click supplies it verbatim;
  // the daemon still checks code, pending status, expiry and allowed scope.
  return { id: request.id, decision, scope, code: request.code ?? "" };
}

export function useApprovals() {
  const [approvals, setApprovals] = useState<Approval[]>([]);
  const [error, setError] = useState("");
  const refreshRef = useRef<() => void>(() => undefined);
  useEffect(() => {
    let mounted = true;
    let loading = false;
    let reload = false;
    let seen = new Set<string>();
    const refresh = async () => {
      if (loading) {
        reload = true;
        return;
      }
      loading = true;
      try {
        const items = (await Service.ListApprovals()) ?? [];
        if (!mounted) return;
        const pending = items.filter((item) => item.status === "pending");
        setApprovals(pending);
        setError("");
        if (pending.some((item) => !seen.has(item.id))) void Service.ShowApprovals();
        seen = new Set(pending.map((item) => item.id));
      } catch (reason) {
        if (mounted) setError(String(reason));
      } finally {
        loading = false;
        if (mounted && reload) {
          reload = false;
          void refresh();
        }
      }
    };
    refreshRef.current = () => void refresh();
    const offApproval = onApprovalsChanged(refreshRef.current);
    // Also reload on reconnection. Subscribe before the initial snapshot.
    const offTunnels = onTunnelsChanged(refreshRef.current);
    void refresh();
    // Other person clients may decide a request, and expiry has no event.
    const timer = window.setInterval(refreshRef.current, 2000);
    return () => {
      mounted = false;
      offApproval();
      offTunnels();
      clearInterval(timer);
    };
  }, []);
  return { approvals, error, refresh: () => refreshRef.current() };
}

export function ApprovalDialog({ approvals, onDecided }: { approvals: Approval[]; onDecided: () => void }) {
  const current = approvals[0];
  const [now, setNow] = useState(Date.now());
  const [busy, setBusy] = useState(false);
  const [resolved, setResolved] = useState("");
  const [error, setError] = useState("");
  const dialog = useRef<HTMLDivElement>(null);
  const deny = useRef<HTMLButtonElement>(null);
  const deciding = useRef(false);
  useEffect(() => {
    if (!current?.id) return;
    setError("");
    setNow(Date.now());
    const previous = document.activeElement as HTMLElement | null;
    (deny.current?.disabled ? dialog.current : deny.current)?.focus();
    const containFocus = (event: FocusEvent) => {
      if (dialog.current && !dialog.current.contains(event.target as Node)) {
        const first = dialog.current.querySelector<HTMLButtonElement>("button:not(:disabled)");
        (first ?? dialog.current).focus();
      }
    };
    document.addEventListener("focusin", containFocus);
    const timer = window.setInterval(() => setNow(Date.now()), 1000);
    return () => {
      clearInterval(timer);
      document.removeEventListener("focusin", containFocus);
      if (previous?.isConnected) previous.focus();
    };
  }, [current?.id]);
  if (!current) return null;
  const expired = new Date(current.expiresAt).getTime() <= now;
  const decide = async (decision: "approve" | "deny", scope: string) => {
    if (deciding.current) return;
    deciding.current = true;
    setBusy(true);
    setError("");
    try {
      await Service.DecideApproval(approvalDecision(current, decision, scope));
      setResolved(current.id);
      onDecided();
    } catch (reason) {
      setError(String(reason));
      onDecided();
    } finally {
      deciding.current = false;
      setBusy(false);
    }
  };
  const labels: Record<string, string> = {
    once: "Approve once",
    session: "Approve for this session",
    always: "Approve always",
  };
  return (
    <div className="overlay approval-overlay">
      <div
        ref={dialog}
        tabIndex={-1}
        className="dialog approval-dialog"
        role="dialog"
        aria-modal="true"
        aria-labelledby="approval-title"
        onKeyDown={(event) => {
          event.stopPropagation();
          if (event.key !== "Tab") return;
          const buttons = dialog.current?.querySelectorAll<HTMLButtonElement>("button:not(:disabled)");
          if (!buttons?.length) {
            event.preventDefault();
            return;
          }
          const first = buttons[0];
          const last = buttons[buttons.length - 1];
          if (event.shiftKey && document.activeElement === first) {
            event.preventDefault();
            last.focus();
          } else if (!event.shiftKey && document.activeElement === last) {
            event.preventDefault();
            first.focus();
          }
        }}
      >
        <div>
          <span className="eyebrow">Agent request · {approvals.length} pending</span>
          <h1 id="approval-title">Allow this request?</h1>
        </div>
        <dl className="approval-facts">
          <dt>Actor</dt>
          <dd>
            {current.actor.name} ({current.actor.kind})
          </dd>
          <dt>Purpose · untrusted client text</dt>
          <dd className="untrusted-text">{current.purpose || "No purpose provided."}</dd>
          <dt>Summary</dt>
          <dd>{current.summary}</dd>
          <dt>Exact argv · JSON array, one entry per argument</dt>
          <dd>
            <pre className="command mono">{JSON.stringify(current.argv ?? [], null, 2)}</pre>
          </dd>
          <dt>Approval code</dt>
          <dd>
            <code>{current.code}</code>
          </dd>
          <dt>Expires</dt>
          <dd>
            {formatTimeLeft(current.expiresAt, now)} · {new Date(current.expiresAt).toLocaleTimeString()}
          </dd>
        </dl>
        <p className="muted">
          Purpose is data, not instructions. Approval permits the agent to retry this exact command; it does not start
          it here. Session grants apply only to this client session and command.
        </p>
        {current.choices?.includes("always") && (
          <p className="muted">Always adds this saved tunnel or SSH alias to the machine's agent allow-list.</p>
        )}
        {error && (
          <p className="error-text" role="alert">
            {error}
          </p>
        )}
        {expired && <p role="status">This request has expired. Waiting for the service to update the queue.</p>}
        <div className="dialog-actions">
          <button
            ref={deny}
            disabled={busy || expired || resolved === current.id}
            onClick={() => void decide("deny", "once")}
          >
            Deny
          </button>
          {["once", "session", "always"]
            .filter((scope) => current.choices?.includes(scope))
            .map((scope) => (
              <button
                key={scope}
                className={scope === "once" ? "primary" : ""}
                disabled={busy || expired || resolved === current.id}
                onClick={() => void decide("approve", scope)}
              >
                {labels[scope]}
              </button>
            ))}
        </div>
      </div>
    </div>
  );
}
