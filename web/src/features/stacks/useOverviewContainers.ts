import { useEffect, useRef, useState } from "preact/hooks";
import { listStackContainers } from "./api";
import {
  readOverviewContainers,
  type OverviewSnapshot,
} from "./overviewContainers";

const refreshInterval = 5000;

type StoredSnapshot = OverviewSnapshot & { key: string };

export function useOverviewContainers(
  ids: string[],
  refreshKey: string,
): OverviewSnapshot {
  const key = JSON.stringify([...new Set(ids)].sort());
  const [snapshot, setSnapshot] = useState<StoredSnapshot>({
    key,
    rows: [],
    errors: [],
    loading: true,
  });
  const inFlight = useRef<Promise<void> | null>(null);

  useEffect(() => {
    const stackIds = JSON.parse(key) as string[];
    let stopped = false;
    let pending = false;
    let timer: ReturnType<typeof setTimeout> | undefined;

    async function refresh() {
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
      const task = readOverviewContainers(stackIds, listStackContainers).then(
        ({ rows, errors }) => {
          if (!stopped) setSnapshot({ key, rows, errors, loading: false });
        },
      );
      inFlight.current = task;
      await task;
      if (inFlight.current === task) inFlight.current = null;
      if (stopped) return;
      if (pending) {
        pending = false;
        void refresh();
      } else if (!document.hidden) {
        timer = setTimeout(() => void refresh(), refreshInterval);
      }
    }

    function refreshWhenVisible() {
      if (document.hidden) clearTimeout(timer);
      else void refresh();
    }

    setSnapshot((current) => ({
      key,
      rows: current.key === key ? current.rows : [],
      errors: current.key === key ? current.errors : [],
      loading: true,
    }));
    void refresh();
    window.addEventListener("focus", refreshWhenVisible);
    document.addEventListener("visibilitychange", refreshWhenVisible);
    return () => {
      stopped = true;
      clearTimeout(timer);
      window.removeEventListener("focus", refreshWhenVisible);
      document.removeEventListener("visibilitychange", refreshWhenVisible);
    };
  }, [key, refreshKey]);

  if (snapshot.key !== key) return { rows: [], errors: [], loading: true };
  return snapshot;
}
