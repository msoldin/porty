import { useCallback, useEffect, useRef, useState } from "preact/hooks";
import { APIError, message } from "../../lib/http";
import { useTopicStream } from "../../hooks/useTopicStream";
import { listAlerts, acknowledgeAlert, resolveAlert } from "./api";
import type { Alert, AlertPage, AlertView } from "./types";
const changed = "porty:alerts-changed";
export function useAlerts(stackId?: string) {
  const [view, setViewState] = useState<AlertView>("attention");
  const [offset, setOffset] = useState(0);
  const [page, setPage] = useState<AlertPage>();
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const generation = useRef(0);
  const scope = useRef(stackId);
  scope.current = stackId;
  const reload = useCallback(async () => {
    const current = ++generation.current;
    setLoading(true);
    try {
      const value = await listAlerts(stackId, view, offset);
      if (current === generation.current) {
        setPage(value);
        setError("");
      }
    } catch (cause) {
      if (current === generation.current) setError(message(cause));
    } finally {
      if (current === generation.current) setLoading(false);
    }
  }, [stackId, view, offset]);
  useEffect(() => {
    setPage(undefined);
    setBusy(false);
    void reload();
    return () => {
      generation.current++;
    };
  }, [reload]);
  useEffect(() => {
    const refresh = () => void reload();
    window.addEventListener(changed, refresh);
    return () => window.removeEventListener(changed, refresh);
  }, [reload]);
  useTopicStream(
    "alerts",
    (event) => {
      if (event.type === "alert") void reload();
    },
    () => void reload(),
  );
  async function mutate(item: Alert, note?: string) {
    if (busy) return;
    const startedScope = scope.current;
    setBusy(true);
    setError("");
    try {
      if (note === undefined) await acknowledgeAlert(item.id, item.revision);
      else await resolveAlert(item.id, item.revision, note);
      window.dispatchEvent(new Event(changed));
    } catch (cause) {
      if (scope.current === startedScope) {
        if (cause instanceof APIError && cause.status === 409) {
          await reload();
          setError(
            "This alert changed. Review the refreshed record before trying again.",
          );
        } else setError(message(cause));
      }
    } finally {
      if (scope.current === startedScope) setBusy(false);
    }
  }
  return {
    page,
    error,
    loading,
    busy,
    view,
    offset,
    setView: (value: AlertView) => {
      setOffset(0);
      setViewState(value);
    },
    setOffset,
    reload,
    acknowledge: (item: Alert) => mutate(item),
    resolve: (item: Alert, note: string) => mutate(item, note),
  };
}
