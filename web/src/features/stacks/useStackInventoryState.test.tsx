import { act, renderHook } from "@testing-library/preact";
import { afterEach, expect, it, vi } from "vitest";
import { getStackState } from "./api";
import { useStackInventoryState } from "./useStackInventoryState";
import type { StackState } from "./types";
vi.mock("./api", () => ({ getStackState: vi.fn() }));
const state: StackState = {
  runtime: "running",
  freshness: "current",
  hasDeployed: true,
};
afterEach(() => {
  vi.useRealTimers();
  vi.resetAllMocks();
  Object.defineProperty(document, "hidden", {
    configurable: true,
    value: false,
  });
});
it("bounds a 25-stack cycle to four reads and waits for completion before polling", async () => {
  vi.useFakeTimers();
  let active = 0,
    max = 0;
  const pending: (() => void)[] = [];
  vi.mocked(getStackState).mockImplementation(
    () =>
      new Promise((resolve) => {
        active++;
        max = Math.max(max, active);
        pending.push(() => {
          active--;
          resolve(state);
        });
      }),
  );
  const ids = Array.from({ length: 25 }, (_, i) => String(i));
  const view = renderHook(() => useStackInventoryState(ids, ""));
  expect(getStackState).toHaveBeenCalledTimes(4);
  await act(async () => {
    await vi.advanceTimersByTimeAsync(15000);
  });
  expect(getStackState).toHaveBeenCalledTimes(4);
  for (let i = 0; i < 7; i++)
    await act(async () => {
      pending.splice(0).forEach((resolve) => resolve());
      await vi.advanceTimersByTimeAsync(0);
    });
  expect(max).toBe(4);
  expect(Object.keys(view.result.current.states)).toHaveLength(25);
  expect(view.result.current.loading).toBe(false);
  view.unmount();
  await act(async () => {
    await vi.advanceTimersByTimeAsync(10000);
  });
  expect(getStackState).toHaveBeenCalledTimes(25);
});
it("drops failed observations and resumes after visibility changes", async () => {
  vi.useFakeTimers();
  vi.mocked(getStackState).mockResolvedValue(state);
  const view = renderHook(() => useStackInventoryState(["one"], ""));
  await act(async () => {
    await vi.advanceTimersByTimeAsync(0);
  });
  expect(view.result.current.states.one).toEqual(state);
  Object.defineProperty(document, "hidden", {
    configurable: true,
    value: true,
  });
  act(() => {
    document.dispatchEvent(new Event("visibilitychange"));
  });
  await act(async () => {
    await vi.advanceTimersByTimeAsync(10000);
  });
  expect(getStackState).toHaveBeenCalledTimes(1);
  vi.mocked(getStackState).mockRejectedValue(new Error("Docker offline"));
  Object.defineProperty(document, "hidden", {
    configurable: true,
    value: false,
  });
  await act(async () => {
    document.dispatchEvent(new Event("visibilitychange"));
    await vi.advanceTimersByTimeAsync(0);
  });
  expect(view.result.current.states.one).toBeUndefined();
  expect(view.result.current.errors).toEqual(["one: Docker offline"]);
});
it("ignores stale reads and starts no more requests after unmount", async () => {
  let finish!: (value: StackState) => void;
  vi.mocked(getStackState).mockImplementation(
    () =>
      new Promise((resolve) => {
        finish = resolve;
      }),
  );
  const view = renderHook(({ ids }) => useStackInventoryState(ids, ""), {
    initialProps: { ids: ["old"] },
  });
  view.rerender({ ids: ["new"] });
  expect(getStackState).toHaveBeenCalledTimes(1);
  view.unmount();
  await act(async () => finish(state));
  expect(getStackState).toHaveBeenCalledTimes(1);
});
