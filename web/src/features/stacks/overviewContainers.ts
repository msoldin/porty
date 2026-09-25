import type { Container } from "./types";
import { message } from "../../lib/http";

export type OverviewContainer = { stackId: string; container: Container };
export type OverviewReadError = { stackId: string; message: string };
export type OverviewSnapshot = {
  rows: OverviewContainer[];
  errors: OverviewReadError[];
  loading: boolean;
};

export async function readOverviewContainers(
  ids: string[],
  read: (id: string) => Promise<Container[]>,
): Promise<Pick<OverviewSnapshot, "rows" | "errors">> {
  const rowsByStack: OverviewContainer[][] = Array.from(ids, () => []);
  const errorsByStack: Array<OverviewReadError | undefined> = Array.from({
    length: ids.length,
  });
  let next = 0;
  async function worker() {
    while (next < ids.length) {
      const index = next++;
      const stackId = ids[index];
      try {
        rowsByStack[index] = (await read(stackId)).map((container) => ({
          stackId,
          container,
        }));
      } catch (cause) {
        errorsByStack[index] = { stackId, message: message(cause) };
      }
    }
  }
  await Promise.all(Array.from({ length: Math.min(4, ids.length) }, worker));
  return {
    rows: rowsByStack.flat(),
    errors: errorsByStack.filter(
      (error): error is OverviewReadError => !!error,
    ),
  };
}
