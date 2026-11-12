import type { ReactNode } from "react";

export type SecretOption = { name: string; kind: string; state: string };

export function Field({ label, children, invalid = false }: { label: string; children: ReactNode; invalid?: boolean }) {
  return (
    <label className={`field${invalid ? " field-invalid" : ""}`}>
      <span className="field-label">{label}</span>
      {children}
    </label>
  );
}
export function Toggle({
  label,
  value,
  onChange,
}: {
  label: string;
  value: boolean;
  onChange: (value: boolean) => void;
}) {
  return (
    <label className="check">
      <input type="checkbox" checked={value} onChange={(event) => onChange(event.target.checked)} />
      {label}
    </label>
  );
}

export function SecretSelect({
  value,
  kind,
  secrets,
  emptyLabel,
  onChange,
}: {
  value: string;
  kind: string;
  secrets: SecretOption[];
  emptyLabel: string;
  onChange: (value: string) => void;
}) {
  const available = secrets.filter((secret) => secret.kind === kind);
  const missingCurrent = value && !available.some((secret) => secret.name === value);
  return (
    <select value={value} onChange={(event) => onChange(event.target.value)}>
      <option value="">{emptyLabel}</option>
      {missingCurrent && <option value={value}>{value} · not in Vault</option>}
      {available.map((secret) => (
        <option key={secret.name} value={secret.name}>
          {secret.name}
          {secret.state === "present" ? "" : ` · ${secret.state}`}
        </option>
      ))}
    </select>
  );
}
