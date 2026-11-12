import type { Tunnel } from "../../../bindings/github.com/trieu04/devt-tunnel/internal/core/model/models";
import { Field, SecretOption, SecretSelect } from "./controls";

export function NgrokFields({
  draft,
  update,
  secrets,
}: {
  draft: Tunnel;
  update: (patch: Partial<Tunnel>) => void;
  secrets: SecretOption[];
}) {
  const ngrok = draft.ngrok ?? { proto: "http", addr: "http://127.0.0.1:3000" };
  const change = (patch: Record<string, unknown>) => update({ ngrok: { ...ngrok, ...patch } });
  return (
    <section className="card grid">
      <h2>ngrok</h2>
      <p className="muted full">Expose a local service using your stored ngrok auth token.</p>
      <Field label="Protocol">
        <select value={ngrok.proto || "http"} onChange={(event) => change({ proto: event.target.value })}>
          <option value="http">HTTP</option>
          <option value="tcp">TCP</option>
          <option value="tls">TLS</option>
        </select>
      </Field>
      <Field label="Local service URL" invalid={!ngrok.addr?.trim()}>
        <input
          value={ngrok.addr ?? ""}
          required
          aria-invalid={!ngrok.addr?.trim()}
          onChange={(event) => change({ addr: event.target.value })}
          placeholder="http://127.0.0.1:3000"
        />
      </Field>
      <Field label="Domain">
        <input
          value={ngrok.domain ?? ""}
          onChange={(event) => change({ domain: event.target.value })}
          placeholder="Optional reserved domain"
        />
      </Field>
      <Field label="Auth token">
        <SecretSelect
          value={ngrok.secret ?? ""}
          kind="ngrok"
          secrets={secrets}
          emptyLabel="Select a token…"
          onChange={(secret) => change({ secret })}
        />
      </Field>
      <Field label="Extra arguments">
        <input value={ngrok.extraArgs ?? ""} onChange={(event) => change({ extraArgs: event.target.value })} />
      </Field>
    </section>
  );
}
