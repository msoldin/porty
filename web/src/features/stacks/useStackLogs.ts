import { useEffect, useState } from "preact/hooks";
import { message, refreshSession } from "../../lib/http";
import { runStackAction } from "./api";

type LogStatus = "loading" | "connected" | "reconnecting" | "disconnected";

export function useStackLogs(stackId: string, enabled: boolean) {
  const [output, setOutput] = useState("");
  const [status, setStatus] = useState<LogStatus>("loading");
  const [error, setError] = useState("");
  const [gap, setGap] = useState(false);

  useEffect(() => {
    if (!enabled) return;
    let stopped = false;
    let socket: WebSocket | null = null;
    let refreshTimer: ReturnType<typeof setTimeout> | undefined;
    let reconnectTimer: ReturnType<typeof setTimeout> | undefined;
    let attempts = 0;
    let sequence = 0;
    let loading = false;
    let refreshRequested = false;

    async function load() {
      if (stopped || socket?.readyState !== WebSocket.OPEN) return;
      if (loading) {
        refreshRequested = true;
        return;
      }
      clearTimeout(refreshTimer);
      loading = true;
      const currentSocket = socket;
      try {
        await runStackAction(stackId, "logs");
        if (!stopped && socket === currentSocket) {
          setError("");
          setStatus("connected");
        }
      } catch (cause) {
        if (!stopped && socket === currentSocket) setError(message(cause));
      } finally {
        loading = false;
        if (!stopped && socket?.readyState === WebSocket.OPEN) {
          if (refreshRequested || socket !== currentSocket) {
            refreshRequested = false;
            void load();
          } else {
            refreshTimer = setTimeout(() => void load(), 5000);
          }
        }
      }
    }

    function scheduleReconnect() {
      reconnectTimer = setTimeout(
        async () => {
          if (stopped) return;
          try {
            await refreshSession();
            if (!stopped) connect();
          } catch (cause) {
            if (stopped) return;
            setError(message(cause));
            setStatus("disconnected");
            scheduleReconnect();
          }
        },
        Math.min(30000, 1000 * 2 ** attempts++),
      );
    }

    function connect() {
      const currentSocket = new WebSocket(
        `${location.protocol === "https:" ? "wss" : "ws"}://${location.host}/api/v1/stream`,
      );
      socket = currentSocket;
      currentSocket.onopen = () => {
        if (stopped || socket !== currentSocket) return;
        attempts = 0;
        currentSocket.send(
          JSON.stringify({
            type: "subscribe",
            subscriptionId: "logs",
            topic: `logs:${stackId}`,
            since: sequence,
          }),
        );
        void load();
      };
      currentSocket.onmessage = (event) => {
        if (stopped || socket !== currentSocket) return;
        try {
          const value = JSON.parse(event.data);
          if (value.subscriptionId !== "logs") return;
          if (value.type === "gap") {
            sequence = 0;
            setGap(true);
            void load();
          } else if (value.type === "log") {
            if (
              !Number.isSafeInteger(value.sequence) ||
              typeof value.payload?.output !== "string"
            ) {
              setGap(true);
              void load();
              return;
            }
            if (value.sequence <= sequence) return;
            sequence = value.sequence;
            setOutput(value.payload.output.slice(-65536));
            setStatus("connected");
            setGap(false);
          }
        } catch {
          setGap(true);
          void load();
        }
      };
      currentSocket.onclose = () => {
        if (stopped || socket !== currentSocket) return;
        clearTimeout(refreshTimer);
        setStatus("reconnecting");
        setGap(true);
        scheduleReconnect();
      };
      currentSocket.onerror = () => currentSocket.close();
    }

    setOutput("");
    setError("");
    setGap(false);
    setStatus("loading");
    connect();
    return () => {
      stopped = true;
      clearTimeout(refreshTimer);
      clearTimeout(reconnectTimer);
      socket?.close();
    };
  }, [stackId, enabled]);

  return { output, status, error, gap };
}
