import type { Lease } from "../../bindings/github.com/trieu04/devt-tunnel/internal/core/model";

export function leaseDeadline(lease: Lease): string | undefined {
  const expiry = lease.expiresAt.startsWith("0001-") ? undefined : lease.expiresAt;
  return lease.graceUntil && (!expiry || Date.parse(lease.graceUntil) < Date.parse(expiry)) ? lease.graceUntil : expiry;
}

export function leaseIsActive(lease: Lease, now: number): boolean {
  const deadline = leaseDeadline(lease);
  return !lease.revoked && (deadline ? Date.parse(deadline) > now : !!lease.detached);
}
