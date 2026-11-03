import { useCallback, useEffect, useState } from "react";
import { Overview, Service, onTunnelsChanged } from "./api";

/** Mirrors the Go side's state. Go is the source of truth; the UI never computes states. */
export function useOverview(): { overview: Overview | null; refresh: () => void } {
  const [overview, setOverview] = useState<Overview | null>(null);
  const refresh = useCallback(() => {
    Service.Overview().then(setOverview);
  }, []);
  useEffect(() => {
    refresh();
    return onTunnelsChanged(refresh);
  }, [refresh]);
  return { overview, refresh };
}
