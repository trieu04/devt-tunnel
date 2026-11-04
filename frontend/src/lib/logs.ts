export type LogLine = { seq: number; line: string };

export function applyLogUpdate(current: LogLine[], lines: LogLine[], replace = false): LogLine[] {
  return (replace ? lines : [...current, ...lines]).slice(-2000);
}
