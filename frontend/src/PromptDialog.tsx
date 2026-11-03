import { FormEvent, useEffect, useRef, useState } from "react";
import { Service, TunnelView } from "./api";
import type { PendingPrompt } from "../bindings/github.com/trieu04/devt-tunnel/internal/host";

/** Shows ssh's own question. Host-key prompts stay visible as typed; everything else is hidden input. */
export function PromptDialog({ prompts, tunnels }: { prompts: PendingPrompt[]; tunnels: TunnelView[] }) {
  const current = prompts[0];
  const [value, setValue] = useState("");
  const input = useRef<HTMLInputElement>(null);

  useEffect(() => {
    setValue("");
    input.current?.focus();
  }, [current?.id]);

  if (!current) return null;
  const { id, prompt } = current;
  const name = tunnels.find((t) => t.tunnel.id === prompt.tunnelId)?.tunnel.name ?? prompt.tunnelId;
  const isHostKey = prompt.kind === "host_key";

  const submit = (e: FormEvent) => {
    e.preventDefault();
    Service.AnswerPrompt(id, value, false);
  };

  return (
    <div className="overlay">
      <form className="dialog" role="dialog" aria-modal="true" aria-labelledby="prompt-title" onSubmit={submit}>
        <h1 id="prompt-title">{name} is asking</h1>
        <label htmlFor="prompt-input" className="prompt-text mono">
          {prompt.text}
        </label>
        <input
          id="prompt-input"
          ref={input}
          type={isHostKey ? "text" : "password"}
          autoComplete="off"
          value={value}
          onChange={(e) => setValue(e.target.value)}
        />
        <div className="dialog-actions">
          <button type="button" onClick={() => Service.AnswerPrompt(id, "", true)}>
            Cancel
          </button>
          <button type="submit" className="primary">
            Send
          </button>
        </div>
      </form>
    </div>
  );
}
