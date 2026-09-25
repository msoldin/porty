import { act, renderHook } from "@testing-library/preact";
import { afterEach, expect, it, vi } from "vitest";
import { getContainerLogs } from "./api";
import { useContainerLogs } from "./useContainerLogs";

vi.mock("./api", () => ({ getContainerLogs: vi.fn() }));

afterEach(() => {
  vi.useRealTimers();
  vi.clearAllMocks();
  Object.defineProperty(document, "hidden", {
    configurable: true,
    value: false,
  });
});

it("loads only for the active Logs tab and polls the selected ID", async () => {
  vi.useFakeTimers();
  vi.mocked(getContainerLogs).mockResolvedValue({
    output: "ready\n",
    truncated: false,
  });
  const { result, rerender, unmount } = renderHook(
    ({ enabled }) => useContainerLogs("one", "full-id-b", enabled),
    { initialProps: { enabled: false } },
  );
  expect(getContainerLogs).not.toHaveBeenCalled();
  rerender({ enabled: true });
  await act(async () => {
    await Promise.resolve();
  });
  expect(result.current.output).toBe("ready\n");
  expect(getContainerLogs).toHaveBeenCalledWith("one", "full-id-b");
  await act(async () => {
    await vi.advanceTimersByTimeAsync(5000);
  });
  expect(getContainerLogs).toHaveBeenCalledTimes(2);
  rerender({ enabled: false });
  await act(async () => {
    await vi.advanceTimersByTimeAsync(5000);
  });
  expect(getContainerLogs).toHaveBeenCalledTimes(2);
  unmount();
});

it("pauses while hidden and refreshes when focus returns", async () => {
  vi.useFakeTimers();
  vi.mocked(getContainerLogs).mockResolvedValue({
    output: "ready",
    truncated: false,
  });
  const { unmount } = renderHook(() =>
    useContainerLogs("one", "full-id-b", true),
  );
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
  expect(getContainerLogs).toHaveBeenCalledTimes(1);
  Object.defineProperty(document, "hidden", {
    configurable: true,
    value: false,
  });
  await act(async () => {
    window.dispatchEvent(new Event("focus"));
    await Promise.resolve();
  });
  expect(getContainerLogs).toHaveBeenCalledTimes(2);
  unmount();
});

it("drops stale responses after a container change and reports truncation", async () => {
  let finishOld!: (value: { output: string; truncated: boolean }) => void;
  vi.mocked(getContainerLogs)
    .mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          finishOld = resolve;
        }),
    )
    .mockResolvedValueOnce({ output: "new output", truncated: true });
  const { result, rerender, unmount } = renderHook(
    ({ id }) => useContainerLogs("one", id, true),
    { initialProps: { id: "full-id-a" } },
  );
  rerender({ id: "full-id-b" });
  await act(async () => {
    await Promise.resolve();
  });
  expect(result.current.output).toBe("new output");
  expect(result.current.truncated).toBe(true);
  await act(async () => {
    finishOld({ output: "stale", truncated: false });
    await Promise.resolve();
  });
  expect(result.current.output).toBe("new output");
  unmount();
});

it("shows a read error and clears stale output", async () => {
  vi.mocked(getContainerLogs).mockRejectedValue(
    new Error("Log driver unavailable"),
  );
  const { result, unmount } = renderHook(() =>
    useContainerLogs("one", "full-id-b", true),
  );
  await act(async () => {
    await Promise.resolve();
  });
  expect(result.current.error).toBe("Log driver unavailable");
  expect(result.current.output).toBe("");
  unmount();
});
