import { act, renderHook } from "@testing-library/preact";
import { expect, it, vi } from "vitest";
import {
  useContainerBatchActions,
  type ContainerTarget,
} from "./useContainerBatchActions";
import { runContainerBatchAction } from "./api";
vi.mock("./api", () => ({ runContainerBatchAction: vi.fn() }));
const target = {
  stackId: "one",
  stackName: "monitoring",
  container: { id: "replica-1", name: "web-1", state: "running" },
  archived: false,
  busy: false,
} as ContainerTarget;
it("rejects changed membership or state instead of submitting replacement replicas", async () => {
  const view = renderHook(
    ({ targets }) => useContainerBatchActions({ targets, onAccepted: vi.fn() }),
    { initialProps: { targets: [target] } },
  );
  act(() => view.result.current.request("restart"));
  view.rerender({
    targets: [
      { ...target, container: { ...target.container, id: "replica-2" } },
    ],
  });
  await act(async () => view.result.current.confirm());
  expect(runContainerBatchAction).not.toHaveBeenCalled();
  expect(view.result.current.error).toContain("changed");
});
