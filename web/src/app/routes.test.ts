import { expect, it } from "vitest";
import { parseStackRoute } from "./routes";

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
