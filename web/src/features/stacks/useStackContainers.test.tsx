import { act, renderHook } from "@testing-library/preact";
import { afterEach, expect, it, vi } from "vitest";
import { listStackContainers } from "./api";
import { useStackContainers } from "./useStackContainers";

vi.mock("./api", () => ({ listStackContainers: vi.fn() }));

const app = {
  id: "id-a",
  name: "app-1",
  service: "app",
  state: "running",
  health: "",
  image: "app:latest",
  networks: [],
  ports: [],
};

afterEach(() => {
  vi.useRealTimers();
  vi.clearAllMocks();
  vi.unstubAllGlobals();
  Object.defineProperty(document, "hidden", {
    configurable: true,
    value: false,
  });
});

it("requests containers only while Overview is enabled and refreshes every five seconds", async () => {
  vi.useFakeTimers();
  vi.mocked(listStackContainers).mockResolvedValue([app]);
  const { result, rerender, unmount } = renderHook(
    ({ enabled }) => useStackContainers("one", enabled, ""),
    { initialProps: { enabled: false } },
  );
  expect(listStackContainers).not.toHaveBeenCalled();
  rerender({ enabled: true });
  await act(async () => {
    await Promise.resolve();
  });
  expect(result.current.containers).toEqual([app]);
  expect(listStackContainers).toHaveBeenCalledTimes(1);
  await act(async () => {
    await vi.advanceTimersByTimeAsync(5000);
  });
  expect(listStackContainers).toHaveBeenCalledTimes(2);
  rerender({ enabled: false });
  await act(async () => {
    await vi.advanceTimersByTimeAsync(5000);
  });
  expect(listStackContainers).toHaveBeenCalledTimes(2);
  unmount();
});

it("pauses while hidden and refreshes when visible again", async () => {
  vi.useFakeTimers();
  vi.mocked(listStackContainers).mockResolvedValue([app]);
  const { unmount } = renderHook(() => useStackContainers("one", true, ""));
  await act(async () => {
    await Promise.resolve();
  });
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
  expect(listStackContainers).toHaveBeenCalledTimes(1);
  Object.defineProperty(document, "hidden", {
    configurable: true,
    value: false,
  });
  await act(async () => {
    document.dispatchEvent(new Event("visibilitychange"));
    await Promise.resolve();
  });
  expect(listStackContainers).toHaveBeenCalledTimes(2);
  unmount();
});

it("drops stale rows after a failed read and clears the error after recovery", async () => {
  vi.useFakeTimers();
  vi.mocked(listStackContainers)
    .mockResolvedValueOnce([app])
    .mockRejectedValueOnce(new Error("Docker unavailable"))
    .mockResolvedValueOnce([]);
  const { result, unmount } = renderHook(() =>
    useStackContainers("one", true, ""),
  );
  await act(async () => {
    await Promise.resolve();
  });
  expect(result.current.containers).toEqual([app]);
  await act(async () => {
    await vi.advanceTimersByTimeAsync(5000);
  });
  expect(result.current.containers).toBeUndefined();
  expect(result.current.error).toContain("Docker unavailable");
  await act(async () => {
    await vi.advanceTimersByTimeAsync(5000);
  });
  expect(result.current.containers).toEqual([]);
  expect(result.current.error).toBe("");
  unmount();
});

it("clears old-stack rows and refreshes for a completed action", async () => {
  vi.mocked(listStackContainers).mockResolvedValue([app]);
  const { result, rerender, unmount } = renderHook(
    ({ id, keyValue }) => useStackContainers(id, true, keyValue),
    { initialProps: { id: "one", keyValue: "" } },
  );
  await act(async () => {
    await Promise.resolve();
  });
  expect(result.current.containers).toEqual([app]);
  rerender({ id: "two", keyValue: "" });
  expect(result.current.containers).toBeUndefined();
  await act(async () => {
    await Promise.resolve();
  });
  expect(listStackContainers).toHaveBeenLastCalledWith("two");
  rerender({ id: "two", keyValue: "op-done" });
  await act(async () => {
    await Promise.resolve();
  });
  expect(listStackContainers).toHaveBeenCalledTimes(3);
  unmount();
});

it("ignores a late result after unmount", async () => {
  let finish!: (value: (typeof app)[]) => void;
  vi.mocked(listStackContainers).mockImplementationOnce(
    () =>
      new Promise((resolve) => {
        finish = resolve;
      }),
  );
  const { unmount } = renderHook(() => useStackContainers("one", true, ""));
  unmount();
  await act(async () => {
    finish([app]);
    await Promise.resolve();
  });
  expect(listStackContainers).toHaveBeenCalledTimes(1);
});
