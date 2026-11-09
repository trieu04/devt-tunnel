import { MutableRefObject, ReactNode, useEffect, useState } from "react";
import { TunnelView } from "../api";
import { ConfirmDialog } from "../ConfirmDialog";
import { PlusGlyph } from "../parts";

type Named = { id: string; name: string };

/** Lets the surrounding Settings page ask before it replaces an edited draft. */
export type LeaveGuard = MutableRefObject<((next: () => void) => void) | null>;

type Props<T extends Named> = {
  noun: string;
  items: T[];
  empty: () => T;
  tunnels: TunnelView[];
  initial?: string;
  isMember: (view: TunnelView, id: string) => boolean;
  help: string;
  membersHelp: string;
  deleteHelp: string;
  /** Second line of a list row. */
  meta: (item: T, members: TunnelView[]) => ReactNode;
  /** Edit fields for the draft. */
  fields: (draft: T, setDraft: (next: T) => void) => ReactNode;
  /** Buttons that act immediately on the saved item. */
  actions?: (draft: T, members: TunnelView[], dirty: boolean, fail: (reason: unknown) => void) => ReactNode;
  /** Note at the end of a member row. */
  rowNote?: (view: TunnelView, checked: boolean, draft: T) => ReactNode;
  save: (draft: T, members: string[]) => Promise<string>;
  remove: (id: string) => Promise<void>;
  duplicate: (id: string) => Promise<string>;
  refresh: () => void;
  leaveGuard: LeaveGuard;
};

/** Shared list-and-detail page for the things tunnels are organized into. */
export function OrganizeScreen<T extends Named>(props: Props<T>) {
  const { noun, items, empty, tunnels, isMember, refresh, leaveGuard } = props;
  const [selected, setSelected] = useState(props.initial === "new" ? "" : props.initial || items[0]?.id || "");
  const stored = items.find((item) => item.id === selected);
  const storedMembers = tunnels.filter((view) => stored && isMember(view, stored.id)).map((view) => view.tunnel.id);
  // Tunnel state changes refresh the overview constantly. Only a change to the
  // saved item or its membership may replace what is being edited.
  const storedKey = JSON.stringify([selected, stored ?? null, [...storedMembers].sort()]);
  const [draft, setDraft] = useState<T>(() => structuredClone(stored ?? empty()));
  const [members, setMembers] = useState<string[]>(storedMembers);
  const [error, setError] = useState("");
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [pendingLeave, setPendingLeave] = useState<(() => void) | null>(null);
  const dirty =
    JSON.stringify(draft) !== JSON.stringify(stored ?? empty()) ||
    JSON.stringify([...members].sort()) !== JSON.stringify([...storedMembers].sort());

  useEffect(() => {
    setDraft(structuredClone(stored ?? empty()));
    setMembers(storedMembers);
  }, [storedKey]);

  useEffect(() => {
    const guard = (event: BeforeUnloadEvent) => {
      if (dirty) event.preventDefault();
    };
    window.addEventListener("beforeunload", guard);
    return () => window.removeEventListener("beforeunload", guard);
  }, [dirty]);

  const fail = (reason: unknown) => setError(String(reason));
  const leave = (next: () => void) => {
    if (dirty) setPendingLeave(() => next);
    else next();
  };
  useEffect(() => {
    leaveGuard.current = leave;
    return () => {
      leaveGuard.current = null;
    };
  });
  const select = (id: string) => {
    setError("");
    setSelected(id);
  };
  const open = (promise: Promise<string>) =>
    promise.then((id) => {
      refresh();
      select(id);
    }, fail);
  const plural = `${noun.charAt(0).toUpperCase()}${noun.slice(1)}s`;
  const savedMembers = tunnels.filter((view) => storedMembers.includes(view.tunnel.id));

  return (
    <div className="organize">
      <section className="organize-list" aria-label={`${plural} list`}>
        <div className="organize-list-head">
          <span className="eyebrow">
            {plural} <span className="mono">{items.length}</span>
          </span>
          <button className="small-button" onClick={() => leave(() => select(""))}>
            <PlusGlyph />
            New {noun}
          </button>
        </div>
        <div className="organize-items">
          {items.map((item) => (
            <button
              key={item.id}
              aria-pressed={selected === item.id}
              onClick={() => selected !== item.id && leave(() => select(item.id))}
            >
              <strong>{item.name}</strong>
              <small>
                {props.meta(
                  item,
                  tunnels.filter((view) => isMember(view, item.id)),
                )}
              </small>
            </button>
          ))}
          {items.length === 0 && <p className="organize-empty muted">No {noun}s yet.</p>}
        </div>
        <p className="organize-help muted">{props.help}</p>
      </section>
      <div className="organize-detail">
        <header className="organize-detail-head">
          <div className="heading-with-status">
            <h2>{draft.id ? draft.name || `Unnamed ${noun}` : `New ${noun}`}</h2>
            {dirty && <span className="unsaved-pill">Unsaved</span>}
          </div>
          <div className="actions">
            {draft.id && props.actions?.(draft, savedMembers, dirty, fail)}
            {draft.id && <button onClick={() => open(props.duplicate(draft.id))}>Duplicate</button>}
            <button
              className="primary"
              disabled={!draft.name.trim() || !dirty}
              onClick={() => open(props.save(draft, members))}
            >
              Save
            </button>
          </div>
        </header>
        {error && (
          <div className="banner banner-error" role="alert">
            {error}
          </div>
        )}
        <div className="organize-detail-body">
          <section className="organize-section">
            <div className="grid profile-fields">{props.fields(draft, setDraft)}</div>
          </section>
          <section className="organize-section">
            <div className="section-line section-with-help">
              <div>
                <h3>Tunnels</h3>
                <p className="muted">{props.membersHelp}</p>
              </div>
              <span className="muted">
                {members.length} of {tunnels.length}
              </span>
            </div>
            {tunnels.length === 0 && <div className="inline-empty">No tunnels yet.</div>}
            <div className="member-list">
              {tunnels.map((view) => {
                const checked = members.includes(view.tunnel.id);
                return (
                  <label className={`member-row ${checked ? "member-row-selected" : ""}`} key={view.tunnel.id}>
                    <span className="check member-name">
                      <input
                        type="checkbox"
                        checked={checked}
                        onChange={(event) =>
                          setMembers(
                            event.target.checked
                              ? [...members, view.tunnel.id]
                              : members.filter((member) => member !== view.tunnel.id),
                          )
                        }
                      />
                      <span className={`status-dot status-dot-${view.status.state}`} />
                      <strong>{view.tunnel.name}</strong>
                      <span className="muted">{view.tunnel.type === "ssh" ? "SSH" : view.tunnel.type}</span>
                    </span>
                    <span className="muted member-also">{props.rowNote?.(view, checked, draft)}</span>
                  </label>
                );
              })}
            </div>
          </section>
          {draft.id && (
            <section className="delete-card">
              <div>
                <strong>Delete {noun}</strong>
                <p className="muted">{props.deleteHelp}</p>
              </div>
              <button className="danger" onClick={() => setDeleteOpen(true)}>
                Delete {noun}
              </button>
            </section>
          )}
        </div>
      </div>
      <ConfirmDialog
        open={deleteOpen}
        title={`Delete ${stored?.name ?? draft.name}?`}
        description={props.deleteHelp}
        confirmLabel={`Delete ${noun}`}
        busyLabel="Deleting…"
        onCancel={() => setDeleteOpen(false)}
        onConfirm={async () => {
          await props.remove(draft.id);
          setDeleteOpen(false);
          select(items.find((item) => item.id !== draft.id)?.id ?? "");
          refresh();
        }}
      />
      <ConfirmDialog
        open={pendingLeave !== null}
        title="Discard unsaved changes?"
        description={`Your ${noun} edits will be lost.`}
        confirmLabel="Discard changes"
        onCancel={() => setPendingLeave(null)}
        onConfirm={async () => {
          const next = pendingLeave;
          setPendingLeave(null);
          next?.();
        }}
      />
    </div>
  );
}
