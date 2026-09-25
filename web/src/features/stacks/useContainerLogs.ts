import { useEffect, useState } from "preact/hooks";
import { message } from "../../lib/http";
import { getContainerLogs } from "./api";

type Snapshot = {
  stackId: string;
  containerId: string;
  output: string;
  truncated: boolean;
  loading: boolean;
  error: string;
};

const empty = { output: "", truncated: false, loading: true, error: "" };

export function useContainerLogs(
  stackId: string,
  containerId: string,
  enabled: boolean,
) {
  const [snapshot, setSnapshot] = useState<Snapshot>({
    stackId,
    containerId,
    ...empty,
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
        const result = await getContainerLogs(stackId, containerId);
        if (!stopped)
          setSnapshot({
            stackId,
            containerId,
            ...result,
            loading: false,
            error: "",
          });
      } catch (cause) {
        if (!stopped)
          setSnapshot({
            stackId,
            containerId,
            output: "",
            truncated: false,
            loading: false,
            error: message(cause),
          });
      } finally {
        loading = false;
        if (!stopped && !document.hidden)
          timer = setTimeout(() => void refresh(), 5000);
      }
    }

    function refreshWhenVisible() {
      if (document.hidden) clearTimeout(timer);
      else void refresh();
    }

    setSnapshot({ stackId, containerId, ...empty });
    void refresh();
    window.addEventListener("focus", refreshWhenVisible);
    document.addEventListener("visibilitychange", refreshWhenVisible);
    return () => {
      stopped = true;
      clearTimeout(timer);
      window.removeEventListener("focus", refreshWhenVisible);
      document.removeEventListener("visibilitychange", refreshWhenVisible);
    };
  }, [stackId, containerId, enabled]);

  if (
    !enabled ||
    snapshot.stackId !== stackId ||
    snapshot.containerId !== containerId
  )
    return empty;
  return snapshot;
}
