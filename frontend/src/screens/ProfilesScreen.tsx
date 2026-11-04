import { useEffect, useState } from "react";
import type { Profile } from "../../bindings/github.com/trieu04/devt-tunnel/internal/core/model/models";
import { Overview, Service } from "../api";
import { ConfirmDialog } from "../ConfirmDialog";
import { PlusGlyph } from "../parts";

export function ProfilesScreen({
  overview,
  refresh,
  onBack,
}: {
  overview: Overview;
  refresh: () => void;
  onBack: () => void;
}) {
  const [selected, setSelected] = useState(overview.profiles?.[0]?.id ?? "");
  const stored = (overview.profiles ?? []).find((profile) => profile.id === selected);
  const [draft, setDraft] = useState<Profile>(stored ?? emptyProfile());
  const [members, setMembers] = useState<string[]>([]);
  const [error, setError] = useState("");
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [pendingLeave, setPendingLeave] = useState<(() => void) | null>(null);
  const storedMembers = (overview.tunnels ?? [])
    .filter((view) => (view.tunnel.profiles ?? []).includes(selected))
    .map((view) => view.tunnel.id);
  const dirty =
    JSON.stringify(draft) !== JSON.stringify(stored ?? emptyProfile()) ||
    JSON.stringify([...members].sort()) !== JSON.stringify([...storedMembers].sort());

  useEffect(() => {
    const profile = (overview.profiles ?? []).find((value) => value.id === selected) ?? emptyProfile();
    setDraft(structuredClone(profile));
    setMembers(
      (overview.tunnels ?? [])
        .filter((view) => (view.tunnel.profiles ?? []).includes(profile.id))
        .map((view) => view.tunnel.id),
    );
  }, [selected, overview]);

  const run = (operation: Promise<unknown>) => operation.then(refresh).catch((reason) => setError(String(reason)));
  const save = () =>
    Service.SaveProfile(draft, members)
      .then((id) => {
        refresh();
        setSelected(id);
      })
      .catch((reason) => setError(String(reason)));
  const leaveDraft = (next: () => void) => {
    if (dirty) setPendingLeave(() => next);
    else next();
  };
  const chooseProfile = (id: string) => leaveDraft(() => setSelected(id));
  const toggleMember = (id: string, checked: boolean) => {
    setMembers(checked ? [...members, id] : members.filter((member) => member !== id));
    if (!checked && (draft.autostart ?? []).includes(id))
      setDraft({ ...draft, autostart: (draft.autostart ?? []).filter((member) => member !== id) });
  };

  useEffect(() => {
    const guard = (event: BeforeUnloadEvent) => {
      if (dirty) event.preventDefault();
    };
    window.addEventListener("beforeunload", guard);
    return () => window.removeEventListener("beforeunload", guard);
  }, [dirty]);

  return (
    <main className="screen profiles-screen">
      <header className="screen-header page-header">
        <div className="page-title">
          <span className="breadcrumb">
            <button className="breadcrumb-button" onClick={() => leaveDraft(onBack)}>
              Tunnels
            </button>{" "}
            / Profiles
          </span>
          <h1>Profiles</h1>
        </div>
        <button
          className="primary"
          onClick={() =>
            leaveDraft(() => {
              setSelected("");
              setDraft(emptyProfile());
              setMembers([]);
            })
          }
        >
          <PlusGlyph />
          New profile
        </button>
      </header>
      {error && <div className="banner banner-error">{error}</div>}
      <div className="profiles-layout">
        <section className="profile-list-card" aria-label="Profile list">
          <div className="profile-list-head">
            <span>Profiles</span>
            <span className="mono">{overview.profiles?.length ?? 0}</span>
          </div>
          {(overview.profiles ?? []).map((profile) => {
            const profileMembers = (overview.tunnels ?? []).filter((view) =>
              (view.tunnel.profiles ?? []).includes(profile.id),
            );
            return (
              <button key={profile.id} aria-pressed={selected === profile.id} onClick={() => chooseProfile(profile.id)}>
                <span>
                  <strong>{profile.name}</strong>
                  <small>
                    {profileMembers.length} tunnels · {(profile.autostart ?? []).length} auto-start
                  </small>
                </span>
                {overview.activeProfile === profile.id && <span className="active-pill">Active</span>}
              </button>
            );
          })}
          <div className="profile-list-help">
            A tunnel can belong to several profiles. The active profile filters the tunnel list and the tray; “All
            profiles” shows everything.
          </div>
        </section>
        <div className="profile-content">
          <section className="card profile-editor">
            <div className="profile-editor-head">
              <div className="heading-with-status">
                <h2>{draft.id ? draft.name : "New profile"}</h2>
                {dirty && <span className="unsaved-pill">Unsaved changes</span>}
              </div>
              <div className="actions">
                {draft.id && (
                  <>
                    <button
                      className={overview.activeProfile === draft.id ? "active-button" : "outline-strong"}
                      onClick={() => run(Service.ActivateProfile(draft.id))}
                    >
                      {overview.activeProfile === draft.id ? "Active" : "Activate"}
                    </button>
                    <button onClick={() => run(Service.StartProfile(draft.id))}>Start all</button>
                    <button
                      onClick={() => {
                        Service.StopProfile(draft.id);
                        refresh();
                      }}
                    >
                      Stop all
                    </button>
                    <button
                      onClick={() =>
                        Service.DuplicateProfile(draft.id).then((id) => {
                          refresh();
                          setSelected(id);
                        })
                      }
                    >
                      Duplicate
                    </button>
                  </>
                )}
                <button className="primary" disabled={!draft.name.trim()} onClick={save}>
                  Save
                </button>
              </div>
            </div>
            <div className="grid profile-fields">
              <label className="field">
                <span>Name</span>
                <input value={draft.name} onChange={(event) => setDraft({ ...draft, name: event.target.value })} />
              </label>
              <label className="field">
                <span>SSH config file</span>
                <input
                  className="mono"
                  value={draft.sshConfig ?? ""}
                  placeholder="~/.ssh/config"
                  onChange={(event) => setDraft({ ...draft, sshConfig: event.target.value })}
                />
                <small>This replaces ~/.ssh/config for tunnels in this profile.</small>
              </label>
              <label className="field full">
                <span>Description</span>
                <input
                  value={draft.description ?? ""}
                  onChange={(event) => setDraft({ ...draft, description: event.target.value })}
                />
              </label>
            </div>
            <div className="activation-options">
              <span className="field-label">When this profile becomes active</span>
              <div>
                <label className="check">
                  <input
                    type="radio"
                    checked={draft.onActivate !== "start_autostart"}
                    onChange={() => setDraft({ ...draft, onActivate: "filter" })}
                  />
                  Only filter the list
                </label>
                <label className="check">
                  <input
                    type="radio"
                    checked={draft.onActivate === "start_autostart"}
                    onChange={() => setDraft({ ...draft, onActivate: "start_autostart" })}
                  />
                  Also start its auto-start tunnels
                </label>
              </div>
              <label className="check">
                <input
                  type="checkbox"
                  checked={!!draft.stopNonMembers}
                  onChange={(event) => setDraft({ ...draft, stopNonMembers: event.target.checked })}
                />
                Stop running tunnels that are not in this profile
              </label>
            </div>
          </section>
          <section className="card profile-tunnels">
            <div className="section-line">
              <h2>Tunnels</h2>
              <span className="muted">
                {members.length} of {overview.tunnels?.length ?? 0} tunnels
              </span>
            </div>
            {(overview.tunnels ?? []).map((view) => {
              const also = (view.tunnel.profiles ?? [])
                .filter((id) => id !== draft.id)
                .map((id) => overview.profiles?.find((profile) => profile.id === id)?.name ?? id);
              return (
                <div
                  className={`member-row ${members.includes(view.tunnel.id) ? "member-row-selected" : ""}`}
                  key={view.tunnel.id}
                >
                  <label className="check member-name">
                    <input
                      type="checkbox"
                      checked={members.includes(view.tunnel.id)}
                      onChange={(event) => toggleMember(view.tunnel.id, event.target.checked)}
                    />
                    <span className={`status-dot status-dot-${view.status.state}`} />
                    <strong>{view.tunnel.name}</strong>
                    <span className="muted">{view.tunnel.type === "ssh" ? "SSH" : view.tunnel.type}</span>
                  </label>
                  <span className="muted member-also">{also.length ? `Also in ${also.join(", ")}` : ""}</span>
                  <label className="switch-label">
                    <input
                      type="checkbox"
                      disabled={!members.includes(view.tunnel.id)}
                      checked={(draft.autostart ?? []).includes(view.tunnel.id)}
                      onChange={(event) =>
                        setDraft({
                          ...draft,
                          autostart: event.target.checked
                            ? [...(draft.autostart ?? []), view.tunnel.id]
                            : (draft.autostart ?? []).filter((id) => id !== view.tunnel.id),
                        })
                      }
                    />
                    <span className="switch-track" />
                    Auto-start
                  </label>
                </div>
              );
            })}
          </section>
          {draft.id && (
            <section className="delete-card">
              <div>
                <strong>Delete profile</strong>
                <p className="muted">Tunnels stay. They are only removed from this profile.</p>
              </div>
              <button className="danger" onClick={() => setDeleteOpen(true)}>
                Delete “{draft.name}”
              </button>
            </section>
          )}
        </div>
      </div>
      <ConfirmDialog
        open={deleteOpen}
        title={`Delete ${draft.name}?`}
        description="Tunnels stay and are only removed from this profile."
        confirmLabel="Delete profile"
        busyLabel="Deleting…"
        onCancel={() => setDeleteOpen(false)}
        onConfirm={async () => {
          await Service.DeleteProfile(draft.id);
          setSelected((overview.profiles ?? []).find((profile) => profile.id !== draft.id)?.id ?? "");
          refresh();
        }}
      />
      <ConfirmDialog
        open={pendingLeave !== null}
        title="Discard unsaved changes?"
        description="Your profile edits will be lost."
        confirmLabel="Discard changes"
        onCancel={() => setPendingLeave(null)}
        onConfirm={async () => {
          const next = pendingLeave;
          setPendingLeave(null);
          next?.();
        }}
      />
    </main>
  );
}

function emptyProfile(): Profile {
  return { id: "", name: "", onActivate: "filter", autostart: [] };
}
