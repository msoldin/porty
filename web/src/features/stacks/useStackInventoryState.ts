import type { StackState } from "./types";
import { useEffect, useRef, useState } from "preact/hooks";
import { getStackState } from "./api";
import { message } from "../../lib/http";
type Snapshot = {
  states: Record<string, StackState | undefined>;
  errors: string[];
  loading: boolean;
};
export function useStackInventoryState(
  ids: string[],
  refreshKey: string,
): Snapshot {
  const key = JSON.stringify([...new Set(ids)].sort());
  const [snapshot, setSnapshot] = useState<Snapshot & { key: string }>({
    key,
    states: {},
    errors: [],
    loading: true,
  });
  const inFlight = useRef<Promise<void> | null>(null);
  useEffect(() => {
    const stackIds = JSON.parse(key) as string[];
    let stopped = false,
      pending = false;
    let timer: ReturnType<typeof setTimeout> | undefined;
    async function refresh(): Promise<void> {
      if (stopped || document.hidden) return;
      clearTimeout(timer);
      if (inFlight.current) {
        pending = true;
        await inFlight.current;
        if (pending && !stopped) {
          pending = false;
          void refresh();
        }
        return;
      }
      const task = (async () => {
        const states: Snapshot["states"] = {};
        const errors: string[] = [];
        let next = 0;
        async function worker() {
          while (!stopped && next < stackIds.length) {
            const id = stackIds[next++];
            try {
              states[id] = await getStackState(id);
            } catch (error) {
              errors.push(`${id}: ${message(error)}`);
            }
          }
        }
        await Promise.all(
          Array.from({ length: Math.min(4, stackIds.length) }, worker),
        );
        if (!stopped) setSnapshot({ key, states, errors, loading: false });
      })();
      inFlight.current = task;
      await task;
      if (inFlight.current === task) inFlight.current = null;
      if (stopped) return;
      if (pending) {
        pending = false;
        void refresh();
      } else if (!document.hidden)
        timer = setTimeout(() => void refresh(), 5000);
    }
    function whenVisible() {
      if (document.hidden) clearTimeout(timer);
      else void refresh();
    }
    setSnapshot((current) => ({
      key,
      states: current.key === key ? current.states : {},
      errors: current.key === key ? current.errors : [],
      loading: true,
    }));
    void refresh();
    window.addEventListener("focus", whenVisible);
    document.addEventListener("visibilitychange", whenVisible);
    return () => {
      stopped = true;
      clearTimeout(timer);
      window.removeEventListener("focus", whenVisible);
      document.removeEventListener("visibilitychange", whenVisible);
    };
  }, [key, refreshKey]);
  return snapshot.key === key
    ? snapshot
    : { states: {}, errors: [], loading: true };
}
