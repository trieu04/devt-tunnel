import { useRef, useState } from "react";
import type { Tunnel } from "../../../bindings/github.com/trieu04/devt-tunnel/internal/core/model/models";
import { Field, SecretOption, SecretSelect } from "./controls";

export function CloudflaredFields({
  draft,
  update,
  secrets,
}: {
  draft: Tunnel;
  update: (patch: Partial<Tunnel>) => void;
  secrets: SecretOption[];
}) {
  const cloudflared = draft.cloudflared ?? {};
  const [mode, setMode] = useState<"named" | "quick">(
    cloudflared.name || cloudflared.hostname || cloudflared.secret ? "named" : "quick",
  );
  const namedTunnel = useRef({
    name: cloudflared.name ?? "",
    hostname: cloudflared.hostname ?? "",
    secret: cloudflared.secret ?? "",
  });
  const change = (patch: Record<string, unknown>) => update({ cloudflared: { ...cloudflared, ...patch } });
  const changeNamed = (patch: Partial<typeof namedTunnel.current>) => {
    namedTunnel.current = { ...namedTunnel.current, ...patch };
    change(patch);
  };
  const chooseMode = (next: "named" | "quick") => {
    if (mode === "named")
      namedTunnel.current = {
        name: cloudflared.name ?? "",
        hostname: cloudflared.hostname ?? "",
        secret: cloudflared.secret ?? "",
      };
    setMode(next);
    change(next === "quick" ? { name: "", hostname: "", secret: "" } : namedTunnel.current);
  };
  return (
    <section className="card cloudflared-card">
      <h2>Cloudflared mode</h2>
      <div className="connection-options tunnel-mode" role="radiogroup" aria-label="Cloudflared mode">
        <button type="button" role="radio" aria-checked={mode === "named"} onClick={() => chooseMode("named")}>
          <span className="radio-dot" />
          <span>
            <strong>Named tunnel</strong>
            <small>Use a stored tunnel token and stable hostname</small>
          </span>
        </button>
        <button type="button" role="radio" aria-checked={mode === "quick"} onClick={() => chooseMode("quick")}>
          <span className="radio-dot" />
          <span>
            <strong>Quick tunnel</strong>
            <small>Get a temporary public URL</small>
          </span>
        </button>
      </div>
      <div className="grid connection-fields">
        {mode === "named" && (
          <>
            <Field label="Tunnel name">
              <input value={cloudflared.name ?? ""} onChange={(event) => changeNamed({ name: event.target.value })} />
            </Field>
            <Field label="Public hostname">
              <input
                value={cloudflared.hostname ?? ""}
                onChange={(event) => changeNamed({ hostname: event.target.value })}
                placeholder="app.example.com"
              />
            </Field>
            <Field label="Tunnel token">
              <SecretSelect
                value={cloudflared.secret ?? ""}
                kind="cloudflared"
                secrets={secrets}
                emptyLabel="Select a token…"
                onChange={(secret) => changeNamed({ secret })}
              />
            </Field>
          </>
        )}
        <Field
          label="Local service URL"
          invalid={!cloudflared.name?.trim() && !cloudflared.secret?.trim() && !cloudflared.url?.trim()}
        >
          <input
            value={cloudflared.url ?? ""}
            aria-invalid={!cloudflared.name?.trim() && !cloudflared.secret?.trim() && !cloudflared.url?.trim()}
            onChange={(event) => change({ url: event.target.value })}
            placeholder="http://127.0.0.1:3000"
          />
        </Field>
        <Field label="Extra arguments">
          <input value={cloudflared.extraArgs ?? ""} onChange={(event) => change({ extraArgs: event.target.value })} />
        </Field>
      </div>
    </section>
  );
}
