import { expect, it } from "vitest";
import { parseStackRoute, requiresRepository } from "./routes";

it("parses stack and exact container routes with separately decoded IDs", () => {
  expect(parseStackRoute("/stacks/stack%20one")).toEqual({
    stackId: "stack one",
  });
  expect(parseStackRoute("/stacks/stack%20one/containers/full-id-b")).toEqual({
    stackId: "stack one",
    containerId: "full-id-b",
  });
});

it("rejects malformed or ambiguous container hashes", () => {
  for (const route of [
    "/stacks/stack/containers/%E0%A4%A",
    "/stacks/stack/containers/id%2Fother",
    "/stacks/stack/containers/",
    "/stacks/stack/more",
  ])
    expect(parseStackRoute(route)).toBeNull();
});

it("opens logs for the named container without changing existing route identities", () => {
  expect(parseStackRoute("/stacks/one/containers/two/logs")).toEqual({
    stackId: "one",
    containerId: "two",
    containerSection: "logs",
  });
  expect(parseStackRoute("/stacks/one/logs")).toBeNull();
  expect(parseStackRoute("/stacks/one/containers/two/other")).toBeNull();
});

it("gates only repository-dependent destinations", () => {
  for (const route of ["/", "/settings", "/alerts"])
    expect(requiresRepository(route)).toBe(false);
  for (const route of [
    "/stacks",
    "/stacks/one/containers/two",
    "/repository",
    "/operations",
    "/audit",
  ])
    expect(requiresRepository(route)).toBe(true);
});
