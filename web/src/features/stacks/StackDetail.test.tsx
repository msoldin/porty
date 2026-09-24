import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/preact";
import { afterEach, expect, it, vi } from "vitest";
import { StackDetail } from "./StackDetail";
import {
  getStackState,
  listStackContainers,
  runContainerBatchAction,
  runStackAction,
} from "./api";
import { useStackLogs } from "./useStackLogs";
import type { Stack } from "./types";
import type { Operation } from "../operations/types";

vi.mock("./useStackLogs", () => ({ useStackLogs: vi.fn() }));
vi.mock("./api", () => ({
  getStackState: vi.fn(),
  listStackContainers: vi.fn(),
  listDeployments: vi.fn().mockResolvedValue([]),
  runStackAction: vi.fn(),
  runContainerBatchAction: vi.fn(),
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

it("keeps whole-stack Actions separate from selected Services actions", async () => {
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
  vi.mocked(runContainerBatchAction).mockResolvedValue({
    id: "op-b",
    kind: "container_batch_restart",
    scopeType: "stack",
    scopeId: "one",
    status: "queued",
    outputTruncated: false,
  });
  const onAction = vi.fn();
  showStack(stack, { onAction });
  expect(
    await screen.findByRole("checkbox", { name: "Select app-2" }),
  ).toBeInTheDocument();
  expect(screen.getByRole("heading", { name: "Services" })).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Validate" })).toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "Actions" }));
  expect(screen.getByRole("menuitem", { name: "Deploy" })).toBeInTheDocument();
  expect(screen.getByRole("menuitem", { name: "Restart" })).toHaveAttribute(
    "aria-disabled",
    "false",
  );
  expect(screen.getByRole("menuitem", { name: "Stop" })).toHaveAttribute(
    "aria-disabled",
    "false",
  );
  for (const name of [
    "Refresh status",
    "Start stack",
    "Pull images",
    "Recreate containers",
  ])
    expect(screen.queryByRole("button", { name })).toBeNull();
  fireEvent.click(screen.getByRole("checkbox", { name: "Select app-2" }));
  fireEvent.click(screen.getByRole("button", { name: "Restart selected" }));
  await waitFor(() =>
    expect(runContainerBatchAction).toHaveBeenCalledWith(
      "one",
      ["id-b"],
      "restart",
    ),
  );
  expect(runStackAction).not.toHaveBeenCalled();
  expect(onAction).toHaveBeenCalledWith(
    expect.objectContaining({ id: "op-b" }),
  );
});

it("blocks selected container actions with unsaved editor changes", async () => {
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
  fireEvent.click(
    await screen.findByRole("checkbox", { name: "Select app-1" }),
  );
  expect(screen.getByRole("button", { name: "Stop selected" })).toBeDisabled();
  expect(runContainerBatchAction).not.toHaveBeenCalled();
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
    await screen.findByRole("checkbox", { name: "Select app-1" }),
  ).toBeDisabled();
});

it("enables Stop and Restart for a running stack with an earlier successful deployment", async () => {
  vi.mocked(getStackState).mockResolvedValue({
    runtime: "running",
    freshness: "unverifiable",
    hasDeployed: true,
  });
  showStack();
  expect(
    await screen.findByText("RUNNING", { selector: ".state-strip .badge" }),
  ).toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "Actions" }));
  expect(screen.getByRole("menuitem", { name: "Stop" })).toHaveAttribute(
    "aria-disabled",
    "false",
  );
  expect(screen.getByRole("menuitem", { name: "Restart" })).toHaveAttribute(
    "aria-disabled",
    "false",
  );
});

it("shows an active deployment with its matching blue status badge", async () => {
  vi.mocked(getStackState).mockResolvedValue({
    runtime: "running",
    freshness: "current",
    hasDeployed: true,
  });
  showStack(stack, {
    operations: [
      {
        id: "deploying",
        kind: "deploy",
        scopeType: "stack",
        scopeId: "one",
        status: "running",
        outputTruncated: false,
      },
    ],
  });
  const badge = await screen.findByText("Deploying", {
    selector: ".state-strip .badge",
  });
  expect(badge).toHaveClass("blue");
});

it("disables Stop and Restart for a running stack without a successful deployment", async () => {
  vi.mocked(getStackState).mockResolvedValue({
    runtime: "running",
    freshness: "never_deployed",
    hasDeployed: false,
  });
  showStack();
  expect(
    await screen.findByText("RUNNING", { selector: ".state-strip .badge" }),
  ).toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "Actions" }));
  expect(screen.getByRole("menuitem", { name: "Stop" })).toHaveAttribute(
    "aria-disabled",
    "true",
  );
  expect(screen.getByRole("menuitem", { name: "Restart" })).toHaveAttribute(
    "aria-disabled",
    "true",
  );
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
  fireEvent.click(screen.getByRole("button", { name: "Actions" }));
  expect(screen.getByRole("menuitem", { name: "Stop" })).toHaveAttribute(
    "aria-disabled",
    "false",
  );
  await act(async () => {
    await vi.advanceTimersByTimeAsync(5000);
  });
  expect(
    screen.getByText("STOPPED", { selector: ".state-strip .badge" }),
  ).toBeInTheDocument();
  expect(screen.getByRole("menuitem", { name: "Stop" })).toHaveAttribute(
    "aria-disabled",
    "true",
  );
  expect(screen.getByRole("menuitem", { name: "Restart" })).toHaveAttribute(
    "aria-disabled",
    "true",
  );
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
  fireEvent.click(screen.getByRole("button", { name: "Actions" }));
  expect(screen.getByRole("menuitem", { name: "Restart" })).toHaveAttribute(
    "aria-disabled",
    "false",
  );
  await act(async () => {
    await vi.advanceTimersByTimeAsync(5000);
  });
  expect(
    screen.getByText("UNKNOWN", { selector: ".state-strip .badge" }),
  ).toBeInTheDocument();
  expect(screen.getByRole("menuitem", { name: "Stop" })).toHaveAttribute(
    "aria-disabled",
    "true",
  );
  expect(screen.getByRole("menuitem", { name: "Restart" })).toHaveAttribute(
    "aria-disabled",
    "true",
  );
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
