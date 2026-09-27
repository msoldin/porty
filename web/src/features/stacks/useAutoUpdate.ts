import { useCallback, useEffect, useRef, useState } from "preact/hooks";
import { APIError, message } from "../../lib/http";
import {
  getAutoUpdate,
  saveAutoUpdate,
  resumeAutoUpdate,
} from "./autoUpdateApi";
import type { AutoUpdateStatus, PolicyUpdate } from "./autoUpdateTypes";
export function useAutoUpdate(id: string) {
  const [status, setStatus] = useState<AutoUpdateStatus>();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const generation = useRef(0);
  const mutation = useRef(0);
  const reload = useCallback(async () => {
    const current = ++generation.current;
    try {
      const value = await getAutoUpdate(id);
      if (current === generation.current) {
        setStatus(value);
        setError("");
      }
    } catch (cause) {
      if (current === generation.current) setError(message(cause));
    }
  }, [id]);
  useEffect(() => {
    setStatus(undefined);
    setBusy(false);
    void reload();
    return () => {
      generation.current++;
      mutation.current++;
    };
  }, [reload]);
  async function mutate(update?: PolicyUpdate) {
    if (!status || busy) return;
    setBusy(true);
    setError("");
    const current = ++generation.current;
    const mutationId = ++mutation.current;
    try {
      const value = update
        ? await saveAutoUpdate(id, update)
        : await resumeAutoUpdate(id, status.policy.revision);
      if (current === generation.current) setStatus(value);
    } catch (cause) {
      if (current === generation.current) {
        if (cause instanceof APIError && cause.status === 409) {
          await reload();
          if (mutationId === mutation.current)
            setError(
              "The policy changed or recovery is unavailable. Review the refreshed status before trying again.",
            );
        } else setError(message(cause));
      }
    } finally {
      if (mutationId === mutation.current) setBusy(false);
    }
  }
  return {
    status,
    busy,
    error,
    reload,
    save: (update: PolicyUpdate) => mutate(update),
    resume: () => mutate(),
  };
}
