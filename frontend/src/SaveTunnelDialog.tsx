import { FormEvent, useEffect, useRef, useState } from "react";
import { Service, TunnelView } from "./api";

export function SaveTunnelDialog({
  view,
  onClose,
  onSaved,
}: {
  view: TunnelView;
  onClose: () => void;
  onSaved: () => void;
}) {
  const [name, setName] = useState(view.tunnel.name);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const input = useRef<HTMLInputElement>(null);
  useEffect(() => {
    input.current?.select();
  }, []);
  const save = async (event: FormEvent) => {
    event.preventDefault();
    if (busy || !name.trim()) return;
    setBusy(true);
    setError("");
    try {
      await Service.PromoteTunnel(view.tunnel.id, name.trim());
      onSaved();
      onClose();
    } catch (reason) {
      setError(String(reason));
    } finally {
      setBusy(false);
    }
  };
  return (
    <div className="overlay confirm-overlay">
      <form className="dialog" role="dialog" aria-modal="true" aria-labelledby="save-tunnel-title" onSubmit={save}>
        <h1 id="save-tunnel-title">Save ephemeral tunnel</h1>
        <p className="muted">
          Keep this definition in tunnels.toml without restarting the tunnel. Its current leases still expire normally.
        </p>
        <label htmlFor="saved-tunnel-name">Name</label>
        <input
          ref={input}
          id="saved-tunnel-name"
          value={name}
          onChange={(event) => setName(event.target.value)}
          disabled={busy}
          required
        />
        {error && (
          <p className="error-text" role="alert">
            {error}
          </p>
        )}
        <div className="dialog-actions">
          <button type="button" disabled={busy} onClick={onClose}>
            Cancel
          </button>
          <button type="submit" className="primary" disabled={busy || !name.trim()}>
            {busy ? "Saving…" : "Save"}
          </button>
        </div>
      </form>
    </div>
  );
}
