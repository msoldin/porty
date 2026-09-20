export type StreamState = { sequence: number; gap: boolean; output: string };
export type StreamEvent = {
  type: string;
  sequence: number;
  payload?: Record<string, unknown>;
};
export function reduceStream(
  state: StreamState,
  event: StreamEvent,
): StreamState {
  if (event.type === "gap") return { ...state, gap: true };
  if (event.sequence <= state.sequence) return state;
  return {
    sequence: event.sequence,
    gap: state.gap,
    output:
      typeof event.payload?.output === "string"
        ? event.payload.output.slice(-65536)
        : state.output,
  };
}

import { useEffect, useRef, useState } from "preact/hooks";
import type { Operation } from "./api";
export function useOperationStream(
  onOperation: (operation: Operation) => void,
  refresh: () => void,
) {
  const callback = useRef(onOperation);
  callback.current = onOperation;
  const refreshRef = useRef(refresh);
  refreshRef.current = refresh;
  const [connection, setConnection] = useState("Connecting");
  const [gap, setGap] = useState(false);
  useEffect(() => {
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
        attempts = 0;
        setConnection("Connected");
        socket.send(
          JSON.stringify({
            type: "subscribe",
            subscriptionId: "operations",
            topic: "operations",
            since: sequence,
          }),
        );
        refreshRef.current();
      };
      socket.onmessage = (event) => {
        try {
          const value = JSON.parse(event.data) as StreamEvent & {
            subscriptionId: string;
          };
          if (value.subscriptionId !== "operations") return;
          if (value.type === "gap") {
            setGap(true);
            refreshRef.current();
            return;
          }
          if (
            value.type === "operation" &&
            value.sequence > sequence &&
            typeof value.payload?.id === "string"
          ) {
            sequence = value.sequence;
            callback.current(value.payload as unknown as Operation);
          }
        } catch {
          setGap(true);
        }
      };
      socket.onclose = () => {
        if (stopped) return;
        setConnection("Reconnecting");
        setGap(true);
        timer = setTimeout(connect, Math.min(30000, 1000 * 2 ** attempts++));
      };
      socket.onerror = () => socket.close();
    }
    connect();
    return () => {
      stopped = true;
      clearTimeout(timer);
      socket.close();
    };
  }, []);
  return { connection, gap };
}
