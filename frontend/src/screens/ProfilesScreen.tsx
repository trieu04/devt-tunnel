import type { Profile } from "../../bindings/github.com/trieu04/devt-tunnel/internal/core/model/models";
import { Overview, Service } from "../api";
import { LeaveGuard, OrganizeScreen } from "./OrganizeScreen";

export function ProfilesScreen({
  overview,
  initial,
  refresh,
  leaveGuard,
}: {
  overview: Overview;
  initial?: string;
  refresh: () => void;
  leaveGuard: LeaveGuard;
}) {
  const profiles = overview.profiles ?? [];
  return (
    <OrganizeScreen<Profile>
      noun="profile"
      items={profiles}
      empty={() => ({ id: "", name: "" })}
      tunnels={overview.tunnels ?? []}
      initial={initial}
      isMember={(view, id) => view.tunnel.profile === id}
      help="A profile holds configuration its tunnels share. Each tunnel uses at most one. To start or stop tunnels together, use Groups."
      membersHelp="A tunnel uses one profile, so checking it here moves it from any other."
      deleteHelp="Tunnels stay and go back to the default SSH config."
      meta={(profile, members) => `${members.length} tunnels · ${profile.sshConfig || "default SSH config"}`}
      fields={(draft, setDraft) => (
        <>
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
            <small>Replaces ~/.ssh/config for tunnels in this profile.</small>
          </label>
          <label className="field full">
            <span>Description</span>
            <input
              value={draft.description ?? ""}
              onChange={(event) => setDraft({ ...draft, description: event.target.value })}
            />
          </label>
        </>
      )}
      rowNote={(view, checked, draft) => {
        const current = view.tunnel.profile;
        if (!current || current === draft.id) return null;
        const name = profiles.find((profile) => profile.id === current)?.name ?? current;
        return checked ? `Moves from ${name}` : `In ${name}`;
      }}
      save={(draft, members) => Service.SaveProfile(draft, members)}
      remove={(id) => Service.DeleteProfile(id)}
      duplicate={(id) => Service.DuplicateProfile(id)}
      refresh={refresh}
      leaveGuard={leaveGuard}
    />
  );
}
