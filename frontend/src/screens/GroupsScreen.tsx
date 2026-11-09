import type { Group } from "../../bindings/github.com/trieu04/devt-tunnel/internal/core/model/models";
import { Overview, Service } from "../api";
import { isActive } from "../lib/filter";
import { LeaveGuard, OrganizeScreen } from "./OrganizeScreen";

export function GroupsScreen({
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
  const groups = overview.groups ?? [];
  return (
    <OrganizeScreen<Group>
      noun="group"
      items={groups}
      empty={() => ({ id: "", name: "" })}
      tunnels={overview.tunnels ?? []}
      initial={initial}
      isMember={(view, id) => (view.tunnel.groups ?? []).includes(id)}
      help="A group is a set of tunnels you start and stop together. A tunnel can be in many groups, whatever its profile."
      membersHelp="Starting the group starts every tunnel checked here."
      deleteHelp="Tunnels stay and keep running. They are only removed from this group."
      meta={(_, members) =>
        `${members.filter((view) => isActive(view.status.state)).length} of ${members.length} running`
      }
      fields={(draft, setDraft) => (
        <>
          <label className="field">
            <span>Name</span>
            <input value={draft.name} onChange={(event) => setDraft({ ...draft, name: event.target.value })} />
          </label>
          <label className="field">
            <span>Description</span>
            <input
              value={draft.description ?? ""}
              onChange={(event) => setDraft({ ...draft, description: event.target.value })}
            />
          </label>
        </>
      )}
      actions={(draft, members, dirty, fail) => (
        <>
          <button
            disabled={dirty || members.length === 0}
            title={dirty ? "Save your changes first" : undefined}
            onClick={() => Service.StartGroup(draft.id).catch(fail).finally(refresh)}
          >
            Start all
          </button>
          <button
            disabled={dirty || !members.some((view) => isActive(view.status.state))}
            title={dirty ? "Save your changes first" : undefined}
            onClick={() => Service.StopGroup(draft.id).catch(fail).finally(refresh)}
          >
            Stop all
          </button>
        </>
      )}
      rowNote={(view, _, draft) => {
        const others = (view.tunnel.groups ?? [])
          .filter((id) => id !== draft.id)
          .map((id) => groups.find((group) => group.id === id)?.name ?? id);
        return others.length ? `Also in ${others.join(", ")}` : null;
      }}
      save={(draft, members) => Service.SaveGroup(draft, members)}
      remove={(id) => Service.DeleteGroup(id)}
      duplicate={(id) => Service.DuplicateGroup(id)}
      refresh={refresh}
      leaveGuard={leaveGuard}
    />
  );
}
