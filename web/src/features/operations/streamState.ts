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
  if (event.type === "gap") return { ...state, sequence: 0, gap: true };
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
