import { act, render, screen } from "@testing-library/preact";
import { afterEach, expect, it, vi } from "vitest";
import { listStackContainers } from "./api";
import { useOverviewContainers } from "./useOverviewContainers";
import type { OverviewSnapshot } from "./overviewContainers";
import type { Container } from "./types";

vi.mock("./api", () => ({ listStackContainers: vi.fn() }));

const item: Container = {
  id: "one-container",
  name: "web-1",
  service: "web",
  state: "running",
  health: "healthy",
  image: "web:1",
  networks: [],
  ports: [],
};

function Probe({ ids, refreshKey }: { ids: string[]; refreshKey: string }) {
  const snapshot = useOverviewContainers(ids, refreshKey);
  return <output data-testid="snapshot">{JSON.stringify(snapshot)}</output>;
}

function snapshot(): OverviewSnapshot {
  return JSON.parse(screen.getByTestId("snapshot").textContent || "{}");
}

async function flush() {
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
  });
}

afterEach(() => {
  vi.useRealTimers();
  vi.clearAllMocks();
  Object.defineProperty(document, "hidden", {
    configurable: true,
    value: false,
  });
});

it("polls after completion and resumes when the page becomes visible", async () => {
  vi.useFakeTimers();
  vi.mocked(listStackContainers).mockResolvedValue([item]);
  const { unmount } = render(<Probe ids={["one"]} refreshKey="" />);
  await flush();
  expect(snapshot().rows).toEqual([{ stackId: "one", container: item }]);
  expect(listStackContainers).toHaveBeenCalledTimes(1);
  await act(async () => {
    await vi.advanceTimersByTimeAsync(5000);
  });
  expect(listStackContainers).toHaveBeenCalledTimes(2);
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
  expect(listStackContainers).toHaveBeenCalledTimes(2);
  Object.defineProperty(document, "hidden", {
    configurable: true,
    value: false,
  });
  await act(async () => {
    document.dispatchEvent(new Event("visibilitychange"));
    await Promise.resolve();
  });
  expect(listStackContainers).toHaveBeenCalledTimes(3);
  unmount();
});

it("drops failed and removed stack rows and refreshes after an operation", async () => {
  vi.mocked(listStackContainers).mockImplementation(async (id) => {
    if (id === "broken") throw new Error("Docker unavailable");
    return id === "one" ? [item] : [];
  });
  const view = render(<Probe ids={["one", "broken"]} refreshKey="" />);
  await flush();
  expect(snapshot().rows).toEqual([{ stackId: "one", container: item }]);
  expect(snapshot().errors).toEqual([
    { stackId: "broken", message: "Docker unavailable" },
  ]);
  view.rerender(<Probe ids={["two"]} refreshKey="op-done" />);
  expect(snapshot().rows).toEqual([]);
  await flush();
  expect(listStackContainers).toHaveBeenLastCalledWith("two");
  expect(snapshot().rows).toEqual([]);
  expect(snapshot().errors).toEqual([]);
  view.unmount();
});

it("serializes a changed refresh key behind an unfinished read and ignores stale results", async () => {
  let finish!: (items: Container[]) => void;
  vi.mocked(listStackContainers)
    .mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          finish = resolve;
        }),
    )
    .mockResolvedValueOnce([]);
  const view = render(<Probe ids={["one"]} refreshKey="" />);
  expect(listStackContainers).toHaveBeenCalledTimes(1);
  view.rerender(<Probe ids={["one"]} refreshKey="completed-operation" />);
  expect(listStackContainers).toHaveBeenCalledTimes(1);
  await act(async () => {
    finish([item]);
    await Promise.resolve();
  });
  await flush();
  expect(listStackContainers).toHaveBeenCalledTimes(2);
  expect(snapshot().rows).toEqual([]);
  view.unmount();
});

it("does not publish a late result after unmount", async () => {
  let finish!: (items: Container[]) => void;
  vi.mocked(listStackContainers).mockImplementationOnce(
    () =>
      new Promise((resolve) => {
        finish = resolve;
      }),
  );
  const view = render(<Probe ids={["one"]} refreshKey="" />);
  view.unmount();
  await act(async () => {
    finish([item]);
    await Promise.resolve();
  });
  expect(listStackContainers).toHaveBeenCalledTimes(1);
});
