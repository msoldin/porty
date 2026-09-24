import { useEffect, useState } from "preact/hooks";
import { getStackState } from "./api";
import type { StackState } from "./types";

const refreshInterval = 5000;

export function useStackState(stackId: string): StackState | undefined {
  const [state, setState] = useState<StackState>();

  useEffect(() => {
    let stopped = false;
    let loading = false;
    let timer: ReturnType<typeof setTimeout> | undefined;

    async function refresh() {
      if (stopped || loading || document.hidden) return;
      clearTimeout(timer);
      loading = true;
      try {
        const value = await getStackState(stackId);
        if (!stopped) setState(value);
      } catch {
        if (!stopped) setState(undefined);
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

    setState(undefined);
    void refresh();
    window.addEventListener("focus", refreshWhenVisible);
    document.addEventListener("visibilitychange", refreshWhenVisible);
    return () => {
      stopped = true;
      clearTimeout(timer);
      window.removeEventListener("focus", refreshWhenVisible);
      document.removeEventListener("visibilitychange", refreshWhenVisible);
    };
  }, [stackId]);

  return state;
}
