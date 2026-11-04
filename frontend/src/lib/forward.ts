export function splitAddress(value: string): { host: string; port: string } {
  if (value.startsWith("[")) {
    const end = value.indexOf("]:");
    if (end >= 0) return { host: value.slice(0, end + 1), port: value.slice(end + 2) };
  }
  const index = value.lastIndexOf(":");
  return index < 0 ? { host: "", port: value } : { host: value.slice(0, index), port: value.slice(index + 1) };
}

export function joinAddress(host: string, port: string): string {
  return host ? `${host}:${port}` : port;
}
