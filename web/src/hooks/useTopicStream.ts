import { useEffect, useRef, useState } from "preact/hooks";
import { refreshSession } from "../lib/http";
export type TopicEvent = {
  type: string;
  sequence: number;
  payload?: Record<string, unknown>;
  subscriptionId: string;
};
export function useTopicStream(
  topic: string,
  onEvent: (event: TopicEvent) => void,
  refresh: () => void,
  enabled = true,
) {
  const callback = useRef(onEvent);
  callback.current = onEvent;
  const refreshRef = useRef(refresh);
  refreshRef.current = refresh;
  const [connection, setConnection] = useState("Connecting");
  const [gap, setGap] = useState(false);
  useEffect(() => {
    if (!enabled) {
      setConnection("Disabled");
      setGap(false);
      return;
    }
    let stopped = false;
    let sequence = 0;
    let socket: WebSocket;
    let timer: ReturnType<typeof setTimeout>;
    let attempts = 0;
    function connect() {
      socket = new WebSocket(
        `${location.protocol === "https:" ? "wss" : "ws"}://${location.host}/api/v1/stream`,
      );
      socket.onopen = () => {
        if (stopped) return;
        attempts = 0;
        setConnection("Connected");
        socket.send(
          JSON.stringify({
            type: "subscribe",
            subscriptionId: topic,
            topic,
            since: sequence,
          }),
        );
        refreshRef.current();
      };
      socket.onmessage = (event) => {
        if (stopped) return;
        try {
          const value = JSON.parse(event.data) as TopicEvent;
          if (value.subscriptionId !== topic) return;
          if (value.type === "gap") {
            sequence = 0;
            setGap(true);
            refreshRef.current();
            return;
          }
          if (value.sequence > sequence) {
            sequence = value.sequence;
            callback.current(value);
          }
        } catch {
          setGap(true);
        }
      };
      socket.onclose = () => {
        if (stopped) return;
        setConnection("Reconnecting");
        setGap(true);
        timer = setTimeout(
          async () => {
            try {
              await refreshSession();
              if (!stopped) connect();
            } catch {
              if (!stopped) setConnection("Disconnected");
            }
          },
          Math.min(30000, 1000 * 2 ** attempts++),
        );
      };
      socket.onerror = () => socket.close();
    }
    connect();
    return () => {
      stopped = true;
      clearTimeout(timer);
      socket.close();
    };
  }, [topic, enabled]);
  return { connection, gap };
}
