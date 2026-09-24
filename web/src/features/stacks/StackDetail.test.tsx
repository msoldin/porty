import { act, fireEvent, render, screen } from "@testing-library/preact";
import { afterEach, expect, it, vi } from "vitest";
import { StackDetail } from "./StackDetail";
import { getStackState } from "./api";
import { useStackLogs } from "./useStackLogs";
import type { Stack } from "./types";

vi.mock("./useStackLogs", () => ({ useStackLogs: vi.fn() }));
vi.mock("./api", () => ({
  getStackState: vi.fn(),
  listDeployments: vi.fn().mockResolvedValue([]),
  runStackAction: vi.fn(),
}));

const stack: Stack = {
  id: "one",
  directoryName: "demo",
  composeProjectName: "demo",
  createdAt: "2026-01-01T00:00:00Z",
  updatedAt: "2026-01-01T00:00:00Z",
};

function showStack(value: Stack = stack) {
  return render(
    <StackDetail
      stack={value}
      repo={null}
      operations={[]}
      dirty={false}
      setDirty={vi.fn()}
      navigate={vi.fn()}
      refresh={vi.fn()}
      onAction={vi.fn()}
    />,
  );
}

afterEach(() => {
  vi.useRealTimers();
  vi.clearAllMocks();
});

it("shows Stop and Restart for a running stack with an earlier successful deployment", async () => {
  vi.mocked(getStackState).mockResolvedValue({
    runtime: "running",
    freshness: "unverifiable",
    hasDeployed: true,
  });
  showStack();
  expect(
    await screen.findByText("running", { selector: ".runtime" }),
  ).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Stop" })).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Restart" })).toBeInTheDocument();
});

it("hides Stop and Restart for a running stack without a successful deployment", async () => {
  vi.mocked(getStackState).mockResolvedValue({
    runtime: "running",
    freshness: "never_deployed",
    hasDeployed: false,
  });
  showStack();
  expect(
    await screen.findByText("running", { selector: ".runtime" }),
  ).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Stop" })).toBeNull();
  expect(screen.queryByRole("button", { name: "Restart" })).toBeNull();
});

it("updates the runtime and hides actions after a manual Docker stop", async () => {
  vi.useFakeTimers();
  vi.mocked(getStackState)
    .mockResolvedValueOnce({
      runtime: "running",
      freshness: "current",
      hasDeployed: true,
    })
    .mockResolvedValueOnce({
      runtime: "stopped",
      freshness: "current",
      hasDeployed: true,
    });
  showStack();
  await act(async () => {
    await Promise.resolve();
  });
  expect(screen.getByRole("button", { name: "Stop" })).toBeInTheDocument();
  await act(async () => {
    await vi.advanceTimersByTimeAsync(5000);
  });
  expect(screen.getByText("stopped")).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Stop" })).toBeNull();
  expect(screen.queryByRole("button", { name: "Restart" })).toBeNull();
});

it("shows Unknown and hides actions when the state refresh fails", async () => {
  vi.useFakeTimers();
  vi.mocked(getStackState)
    .mockResolvedValueOnce({
      runtime: "running",
      freshness: "current",
      hasDeployed: true,
    })
    .mockRejectedValueOnce(new Error("Docker unavailable"));
  showStack();
  await act(async () => {
    await Promise.resolve();
  });
  expect(screen.getByRole("button", { name: "Restart" })).toBeInTheDocument();
  await act(async () => {
    await vi.advanceTimersByTimeAsync(5000);
  });
  expect(
    screen.getByText("Unknown", { selector: ".runtime" }),
  ).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Stop" })).toBeNull();
  expect(screen.queryByRole("button", { name: "Restart" })).toBeNull();
});

it("loads logs on tab entry without a manual load or refresh button", () => {
  vi.mocked(useStackLogs).mockReturnValue({
    output: "",
    status: "loading",
    error: "",
    gap: false,
  });
  showStack();
  fireEvent.click(screen.getByRole("tab", { name: "Logs" }));
  expect(useStackLogs).toHaveBeenLastCalledWith("one", true);
  expect(screen.getByRole("status")).toHaveTextContent("Loading logs");
  expect(
    screen.queryByRole("button", { name: /load logs|refresh logs/i }),
  ).toBeNull();
});

it("keeps output visible during reconnect, gap, and action errors", () => {
  vi.mocked(useStackLogs).mockReturnValue({
    output: "last output",
    status: "reconnecting",
    error: "runtime unavailable",
    gap: true,
  });
  const view = showStack();
  fireEvent.click(screen.getByRole("tab", { name: "Logs" }));
  expect(screen.getByText("last output")).toBeInTheDocument();
  expect(screen.getByRole("status")).toHaveTextContent("Reconnecting to logs");
  expect(
    screen
      .getAllByRole("alert")
      .map((node) => node.textContent)
      .join(" "),
  ).toContain("runtime unavailable");
  expect(screen.getByText(/Some log output was missed/i)).toBeInTheDocument();
  fireEvent.click(screen.getByRole("tab", { name: "Overview" }));
  expect(useStackLogs).toHaveBeenLastCalledWith("one", false);
  fireEvent.click(screen.getByRole("tab", { name: "Logs" }));
  view.rerender(
    <StackDetail
      stack={{ ...stack, id: "two" }}
      repo={null}
      operations={[]}
      dirty={false}
      setDirty={vi.fn()}
      navigate={vi.fn()}
      refresh={vi.fn()}
      onAction={vi.fn()}
    />,
  );
  expect(useStackLogs).toHaveBeenLastCalledWith("two", true);
});
