import { expect, it } from "vitest";
import {
  filterStacks,
  inventorySummary,
  type StackFilters,
} from "./stackInventory";
import type { Stack, StackState } from "./types";
const stacks = ["running", "sleeping", "unknown", "archived"].map((id) => ({
  id,
  directoryName: id,
  archivedAt: id === "archived" ? "today" : undefined,
})) as Stack[];
const states: Record<string, StackState | undefined> = {
  running: { runtime: "running", freshness: "current", hasDeployed: true },
  sleeping: {
    runtime: "sleeping",
    freshness: "changes_pending",
    hasDeployed: true,
  },
};
const filters: StackFilters = {
  search: "",
  runtime: "all",
  deployment: "all",
  git: "all",
  archive: "all",
  sort: "asc",
};
it("keeps runtime, deployment and Git filters independent and includes archived stacks initially", () => {
  expect(filterStacks(stacks, states, null, filters)).toHaveLength(4);
  expect(
    filterStacks(stacks, states, null, { ...filters, runtime: "stopped" }),
  ).toEqual([]);
  const repo = { paths: ["running/compose.yml"] } as never;
  expect(
    filterStacks(stacks, states, repo, {
      ...filters,
      git: "modified",
      deployment: "current",
    }).map((s) => s.id),
  ).toEqual(["running"]);
});
it("counts unknown state separately and never treats sleeping as a failure", () => {
  expect(inventorySummary(stacks, states)).toEqual({
    attention: 0,
    unknown: 1,
    changes: 1,
    total: 3,
  });
  expect(
    inventorySummary(stacks, {
      ...states,
      running: { ...states.running!, runtime: "unhealthy" },
    }).attention,
  ).toBe(1);
});
