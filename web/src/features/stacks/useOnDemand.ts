import { useCallback, useEffect, useRef, useState } from "preact/hooks";
import { APIError, message } from "../../lib/http";
import { listOnDemand, saveOnDemand, changeOnDemand } from "./onDemandApi";
import type {
  OnDemandAction,
  OnDemandGroup,
  OnDemandUpdate,
} from "./onDemandTypes";
export function useOnDemand(id: string) {
  const [groups, setGroups] = useState<OnDemandGroup[]>();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const generation = useRef(0);
  const pending = useRef(false);
  const reload = useCallback(async () => {
    if (pending.current) return;
    const current = ++generation.current;
    try {
      const value = await listOnDemand(id);
      if (current === generation.current) setGroups(value);
    } catch (cause) {
      if (current === generation.current) setError(message(cause));
    }
  }, [id]);
  useEffect(() => {
    setGroups(undefined);
    setBusy(false);
    setError("");
    pending.current = false;
    void reload();
    const timer = window.setInterval(() => {
      if (!pending.current && document.visibilityState === "visible")
        void reload();
    }, 5000);
    return () => {
      generation.current++;
      window.clearInterval(timer);
    };
  }, [reload]);
  async function mutate(action: () => Promise<unknown>): Promise<boolean> {
    if (pending.current) return false;
    pending.current = true;
    setBusy(true);
    setError("");
    const current = ++generation.current;
    let success = false;
    try {
      await action();
      success = true;
    } catch (cause) {
      if (current === generation.current)
        setError(
          cause instanceof APIError &&
            cause.status === 409 &&
            cause.code === "OnDemandConflict"
            ? "The group or runtime changed. Review the refreshed status before retrying."
            : message(cause),
        );
    } finally {
      if (current === generation.current) {
        pending.current = false;
        setBusy(false);
        await reload();
      }
    }
    return success && current + 1 === generation.current;
  }
  return {
    groups,
    busy,
    error,
    reload,
    save: (groupId: string, update: OnDemandUpdate) =>
      mutate(() => saveOnDemand(id, groupId, update)),
    change: (group: OnDemandGroup, action: OnDemandAction) =>
      mutate(() => changeOnDemand(id, group.id, action, group.revision)),
  };
}
