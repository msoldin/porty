import { useEffect, useRef, useState } from "preact/hooks";
import { APIError, message } from "../../lib/http";
import { getMonitoring } from "./api";
import { mergeMonitoring } from "./monitoringState";
import type { MonitoringState } from "./types";

export function useMonitoring(onUnauthorized: () => void) {
  const [state, setState] = useState<MonitoringState>();
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [stale, setStale] = useState(false);
  const [serverNow, setServerNow] = useState<number>();
  const unauthorized = useRef(onUnauthorized);
  unauthorized.current = onUnauthorized;
  const retryRef = useRef<() => void>(() => {});
  useEffect(() => {
    let stopped = false;
    let current: MonitoringState | undefined;
    let receivedAt: number | undefined;
    let active: AbortController | undefined;
    let again = false;
    let timer: ReturnType<typeof setTimeout> | undefined;
    async function poll() {
      if (stopped || document.hidden || active) return;
      const controller = new AbortController();
      active = controller;
      let timedOut = false;
      const deadline = setTimeout(() => {
        timedOut = true;
        controller.abort();
      }, 5000);
      try {
        const response = await getMonitoring(
          current?.cursor,
          controller.signal,
        );
        if (stopped || controller.signal.aborted || document.hidden) return;
        current = mergeMonitoring(current, response);
        receivedAt = performance.now();
        setServerNow(Date.parse(response.serverTime));
        setState(current);
        setStale(false);
        setError("");
      } catch (error) {
        if (stopped) return;
        if (controller.signal.aborted) {
          if (timedOut) setError("Monitoring request timed out. Retrying…");
        } else if (error instanceof APIError && error.status === 401) {
          stopped = true;
          current = undefined;
          setState(undefined);
          unauthorized.current();
        } else {
          if (error instanceof APIError && error.code === "InvalidCursor")
            current = undefined;
          setError(message(error));
        }
      } finally {
        clearTimeout(deadline);
        active = undefined;
        if (!stopped) {
          setLoading(false);
          if (!document.hidden) {
            if (again) {
              again = false;
              void poll();
            } else timer = setTimeout(poll, 2000);
          }
        }
      }
    }
    function resume() {
      clearTimeout(timer);
      if (stopped || document.hidden) return;
      if (active) {
        again = true;
        active.abort();
      } else void poll();
    }
    function visibility() {
      clearTimeout(timer);
      if (document.hidden) {
        again = false;
        active?.abort();
      } else resume();
    }
    retryRef.current = resume;
    document.addEventListener("visibilitychange", visibility);
    const freshness = setInterval(() => {
      if (!stopped && receivedAt !== undefined) {
        setStale(performance.now() - receivedAt >= 6000);
        if (current)
          setServerNow(
            Date.parse(current.serverTime) + performance.now() - receivedAt,
          );
      }
    }, 1000);
    void poll();
    return () => {
      stopped = true;
      retryRef.current = () => {};
      active?.abort();
      clearTimeout(timer);
      clearInterval(freshness);
      document.removeEventListener("visibilitychange", visibility);
    };
  }, []);
  return {
    state,
    loading,
    error,
    stale,
    serverNow,
    retry: () => retryRef.current(),
  };
}
