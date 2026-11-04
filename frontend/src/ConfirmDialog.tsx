import { FormEvent, ReactNode, useEffect, useId, useRef, useState } from "react";

type Props = {
  open: boolean;
  title: string;
  description: ReactNode;
  confirmLabel: string;
  busyLabel?: string;
  tone?: "danger" | "primary";
  onConfirm: () => Promise<void>;
  onCancel: () => void;
};

export function ConfirmDialog({
  open,
  title,
  description,
  confirmLabel,
  busyLabel = "Working…",
  tone = "danger",
  onConfirm,
  onCancel,
}: Props) {
  const titleID = useId();
  const cancelButton = useRef<HTMLButtonElement>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const busyRef = useRef(busy);
  const cancelRef = useRef(onCancel);
  busyRef.current = busy;
  cancelRef.current = onCancel;

  useEffect(() => {
    if (!open) return;
    setError("");
    cancelButton.current?.focus();
    const close = (event: KeyboardEvent) => {
      if (event.key === "Escape" && !busyRef.current) cancelRef.current();
    };
    window.addEventListener("keydown", close);
    return () => window.removeEventListener("keydown", close);
  }, [open]);

  if (!open) return null;
  const submit = async (event: FormEvent) => {
    event.preventDefault();
    setBusy(true);
    setError("");
    try {
      await onConfirm();
      onCancel();
    } catch (reason) {
      setError(String(reason));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div
      className="overlay confirm-overlay"
      onMouseDown={(event) => {
        if (event.target === event.currentTarget && !busy) onCancel();
      }}
    >
      <form
        className="dialog confirm-dialog"
        role="alertdialog"
        aria-modal="true"
        aria-labelledby={titleID}
        onSubmit={submit}
      >
        <div>
          <h1 id={titleID}>{title}</h1>
          <p className="muted">{description}</p>
        </div>
        {error && (
          <p className="error-text" role="alert">
            {error}
          </p>
        )}
        <div className="dialog-actions">
          <button ref={cancelButton} type="button" disabled={busy} onClick={onCancel}>
            Cancel
          </button>
          <button type="submit" className={tone === "danger" ? "danger-button" : "primary"} disabled={busy}>
            {busy ? busyLabel : confirmLabel}
          </button>
        </div>
      </form>
    </div>
  );
}
