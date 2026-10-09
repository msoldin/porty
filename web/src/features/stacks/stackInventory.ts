import type { Repository } from "../repository/types";
import type { Stack, StackState } from "./types";
import { isModified } from "./stackStatus";
export type StackFilters = {
  search: string;
  runtime: string;
  deployment: string;
  git: string;
  archive: string;
  sort: string;
};
export function filterStacks(
  stacks: Stack[],
  states: Record<string, StackState | undefined>,
  repo: Repository | null,
  filters: StackFilters,
): Stack[] {
  return stacks
    .filter((stack) => {
      const state = states[stack.id];
      return (
        stack.directoryName
          .toLowerCase()
          .includes(filters.search.toLowerCase()) &&
        (filters.runtime === "all" ||
          (state?.runtime || "unknown") === filters.runtime) &&
        (filters.deployment === "all" ||
          (state?.freshness || "unknown") === filters.deployment) &&
        (filters.git === "all" ||
          (repo !== null &&
            (filters.git === "modified"
              ? isModified(stack, repo)
              : !isModified(stack, repo)))) &&
        (filters.archive === "all" ||
          (filters.archive === "archived"
            ? !!stack.archivedAt
            : !stack.archivedAt))
      );
    })
    .sort(
      (a, b) =>
        a.directoryName.localeCompare(b.directoryName) *
        (filters.sort === "desc" ? -1 : 1),
    );
}
export function inventorySummary(
  stacks: Stack[],
  states: Record<string, StackState | undefined>,
) {
  const active = stacks.filter((stack) => !stack.archivedAt);
  return {
    total: active.length,
    attention: active.filter((stack) =>
      ["unhealthy", "partial", "stopped"].includes(
        states[stack.id]?.runtime || "",
      ),
    ).length,
    unknown: active.filter(
      (stack) =>
        ![
          "running",
          "on_demand",
          "sleeping",
          "unhealthy",
          "partial",
          "stopped",
        ].includes(states[stack.id]?.runtime || ""),
    ).length,
    changes: active.filter(
      (stack) => states[stack.id]?.freshness === "changes_pending",
    ).length,
  };
}
