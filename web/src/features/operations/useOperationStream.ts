import { useTopicStream } from "../../hooks/useTopicStream";
import type { Operation } from "./types";
export function useOperationStream(
  onOperation: (operation: Operation) => void,
  refresh: () => void,
) {
  return useTopicStream(
    "operations",
    (event) => {
      if (event.type === "operation" && typeof event.payload?.id === "string")
        onOperation(event.payload as unknown as Operation);
    },
    refresh,
  );
}
