import { api } from "../../lib/http";
import type { Snapshot } from "./types";
export function getMonitoring(
  cursor: string | undefined,
  signal: AbortSignal,
): Promise<Snapshot> {
  return api<Snapshot>(
    "/monitoring" + (cursor ? "?cursor=" + encodeURIComponent(cursor) : ""),
    "GET",
    undefined,
    {},
    signal,
  );
}
