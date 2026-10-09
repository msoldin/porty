import { act, renderHook } from "@testing-library/preact";
import { beforeEach, expect, it, vi } from "vitest";
import { useStackBatchActions } from "./useStackBatchActions";
import {
  getDeploymentReview,
  deployReviewedStack,
} from "./deploymentReviewApi";
vi.mock("./deploymentReviewApi", () => ({
  getDeploymentReview: vi.fn(),
  deployReviewedStack: vi.fn(),
}));
vi.mock("./api", () => ({ runStackAction: vi.fn() }));
const stacks = ["one", "two"].map((id) => ({ id, directoryName: id })) as never;
beforeEach(() => {
  vi.resetAllMocks();
  vi.mocked(getDeploymentReview).mockImplementation(async (id) => ({
    stackId: id,
    sourceRevision: id,
    uncommittedChanges: false,
  }));
});
it("keeps rejected stacks selected after partial acceptance", async () => {
  vi.mocked(deployReviewedStack).mockImplementation(async (id) => {
    if (id === "two") throw Error("conflict");
    return { id: "op-one", scopeId: id, status: "queued" } as never;
  });
  const onAccepted = vi.fn();
  const view = renderHook(() =>
    useStackBatchActions({ stacks, states: {}, operations: [], onAccepted }),
  );
  act(() => view.result.current.setSelectedIds(new Set(["one", "two"])));
  await act(async () => view.result.current.request("deploy"));
  expect(deployReviewedStack).not.toHaveBeenCalled();
  await act(async () => view.result.current.confirm());
  expect(view.result.current.selectedIds).toEqual(new Set(["two"]));
  expect(onAccepted).toHaveBeenCalledWith([
    expect.objectContaining({ id: "op-one" }),
  ]);
});
it("never submits a replacement selection after confirmation opens", async () => {
  const view = renderHook(() =>
    useStackBatchActions({
      stacks,
      states: {},
      operations: [],
      onAccepted: vi.fn(),
    }),
  );
  act(() => view.result.current.setSelectedIds(new Set(["one"])));
  await act(async () => view.result.current.request("deploy"));
  act(() => view.result.current.setSelectedIds(new Set(["two"])));
  await act(async () => view.result.current.confirm());
  expect(deployReviewedStack).not.toHaveBeenCalled();
  expect(view.result.current.error).toContain("changed");
});

it("bounds review reads to four even when a cancelled review is restarted", async () => {
  const many = Array.from({ length: 8 }, (_, index) => ({
    id: String(index),
    directoryName: String(index),
  })) as never;
  let active = 0,
    max = 0;
  const pending: (() => void)[] = [];
  vi.mocked(getDeploymentReview).mockImplementation(
    (id) =>
      new Promise((resolve) => {
        active++;
        max = Math.max(max, active);
        pending.push(() => {
          active--;
          resolve({
            stackId: id,
            sourceRevision: id,
            uncommittedChanges: false,
          });
        });
      }),
  );
  const view = renderHook(() =>
    useStackBatchActions({
      stacks: many,
      states: {},
      operations: [],
      onAccepted: vi.fn(),
    }),
  );
  act(() =>
    view.result.current.setSelectedIds(
      new Set(Array.from({ length: 8 }, (_, i) => String(i))),
    ),
  );
  let first!: Promise<void>, second!: Promise<void>;
  await act(async () => {
    first = view.result.current.request("deploy");
    await Promise.resolve();
  });
  act(() => view.result.current.cancel());
  await act(async () => {
    second = view.result.current.request("deploy");
    await Promise.resolve();
  });
  expect(active).toBe(4);
  for (let i = 0; i < 5; i++)
    await act(async () => {
      pending.splice(0).forEach((finish) => finish());
      for (let j = 0; j < 8; j++) await Promise.resolve();
    });
  await act(async () => {
    await first;
    await second;
  });
  expect(max).toBe(4);
});
