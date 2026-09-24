import { useEffect, useState } from "preact/hooks";
import { message } from "../../lib/http";
import { listStackContainers } from "./api";
import type { Container } from "./types";

const refreshInterval = 5000;

type Snapshot = {
  stackId: string;
  containers: Container[] | undefined;
  error: string;
};

export function useStackContainers(
  stackId: string,
  enabled: boolean,
  refreshKey: string,
): { containers: Container[] | undefined; error: string } {
  const [snapshot, setSnapshot] = useState<Snapshot>({
    stackId,
    containers: undefined,
    error: "",
  });

  useEffect(() => {
    if (!enabled) return;
    let stopped = false;
    let loading = false;
    let timer: ReturnType<typeof setTimeout> | undefined;

    async function refresh() {
      if (stopped || loading || document.hidden) return;
      clearTimeout(timer);
      loading = true;
      try {
        const containers = await listStackContainers(stackId);
        if (!stopped) setSnapshot({ stackId, containers, error: "" });
      } catch (cause) {
        if (!stopped)
          setSnapshot({
            stackId,
            containers: undefined,
            error: message(cause),
          });
      } finally {
        loading = false;
        if (!stopped && !document.hidden)
          timer = setTimeout(() => void refresh(), refreshInterval);
      }
    }

    function refreshWhenVisible() {
      if (document.hidden) clearTimeout(timer);
      else void refresh();
    }

    setSnapshot({ stackId, containers: undefined, error: "" });
    void refresh();
    window.addEventListener("focus", refreshWhenVisible);
    document.addEventListener("visibilitychange", refreshWhenVisible);
    return () => {
      stopped = true;
      clearTimeout(timer);
      window.removeEventListener("focus", refreshWhenVisible);
      document.removeEventListener("visibilitychange", refreshWhenVisible);
    };
  }, [stackId, enabled, refreshKey]);

  if (!enabled || snapshot.stackId !== stackId)
    return { containers: undefined, error: "" };
  return { containers: snapshot.containers, error: snapshot.error };
}
