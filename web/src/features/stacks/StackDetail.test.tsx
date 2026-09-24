import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/preact";
import { afterEach, expect, it, vi } from "vitest";
import { StackDetail } from "./StackDetail";
import { getStackState, listStackContainers, runContainerAction } from "./api";
import { useStackLogs } from "./useStackLogs";
import type { Stack } from "./types";
import type { Operation } from "../operations/types";

vi.mock("./useStackLogs", () => ({ useStackLogs: vi.fn() }));
vi.mock("./api", () => ({
  getStackState: vi.fn(),
  listStackContainers: vi.fn(),
  listDeployments: vi.fn().mockResolvedValue([]),
  runStackAction: vi.fn(),
  runContainerAction: vi.fn(),
}));

const stack: Stack = {
  id: "one",
  directoryName: "demo",
  composeProjectName: "demo",
  createdAt: "2026-01-01T00:00:00Z",
  updatedAt: "2026-01-01T00:00:00Z",
};

function showStack(
  value: Stack = stack,
  options: {
    dirty?: boolean;
    operations?: Operation[];
    onAction?: (operation: Operation) => void;
  } = {},
) {
  return render(
    <StackDetail
      stack={value}
      repo={null}
      operations={options.operations || []}
      dirty={options.dirty || false}
      setDirty={vi.fn()}
      navigate={vi.fn()}
      refresh={vi.fn()}
      onAction={options.onAction || vi.fn()}
    />,
  );
}

afterEach(() => {
  vi.useRealTimers();
  vi.clearAllMocks();
});

it("lists each container in Overview while keeping whole-stack header actions", async () => {
  vi.mocked(getStackState).mockResolvedValue({
    runtime: "running",
    freshness: "current",
    hasDeployed: true,
  });
  vi.mocked(listStackContainers).mockResolvedValue([
    {
      id: "id-a",
      name: "app-1",
      service: "app",
      state: "running",
      health: "healthy",
      image: "app:latest",
      networks: [],
      ports: [],
    },
    {
      id: "id-b",
      name: "app-2",
      service: "app",
      state: "running",
      health: "healthy",
      image: "app:latest",
      networks: [],
      ports: [],
    },
  ]);
  vi.mocked(runContainerAction).mockResolvedValue({
    id: "op-b",
    kind: "container_stop",
    scopeType: "stack",
    scopeId: "one",
    status: "queued",
    outputTruncated: false,
  });
  const onAction = vi.fn();
  showStack(stack, { onAction });
  expect(
    await screen.findByRole("button", { name: "Stop app-2" }),
  ).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Stop" })).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Restart" })).toBeInTheDocument();
  for (const name of [
    "Refresh status",
    "Start stack",
    "Pull images",
    "Recreate containers",
  ])
    expect(screen.queryByRole("button", { name })).toBeNull();
  fireEvent.click(screen.getByRole("button", { name: "Stop app-2" }));
  await waitFor(() =>
    expect(runContainerAction).toHaveBeenCalledWith("one", "id-b", "stop"),
  );
  expect(onAction).toHaveBeenCalledWith(
    expect.objectContaining({ id: "op-b" }),
  );
});

it("blocks container actions with unsaved editor changes", async () => {
  vi.mocked(listStackContainers).mockResolvedValue([
    {
      id: "id-a",
      name: "app-1",
      service: "app",
      state: "running",
      health: "",
      image: "app:latest",
      networks: [],
      ports: [],
    },
  ]);
  showStack(stack, { dirty: true });
  fireEvent.click(await screen.findByRole("button", { name: "Stop app-1" }));
  expect(runContainerAction).not.toHaveBeenCalled();
  expect(screen.getByRole("alert")).toHaveTextContent(
    "Save or discard your editor changes",
  );
});

it("disables container actions while a stack operation is active", async () => {
  vi.mocked(listStackContainers).mockResolvedValue([
    {
      id: "id-a",
      name: "app-1",
      service: "app",
      state: "running",
      health: "",
      image: "app:latest",
      networks: [],
      ports: [],
    },
  ]);
  showStack(stack, {
    operations: [
      {
        id: "busy",
        kind: "deploy",
        scopeType: "stack",
        scopeId: "one",
        status: "running",
        outputTruncated: false,
      },
    ],
  });
  expect(
    await screen.findByRole("button", { name: "Stop app-1" }),
  ).toBeDisabled();
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
