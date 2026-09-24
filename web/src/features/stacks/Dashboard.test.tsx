import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/preact";
import { afterEach, expect, it, vi } from "vitest";
import { Dashboard } from "./Dashboard";
import { getStackState, runStackAction } from "./api";
import type { Stack } from "./types";

vi.mock("./api", () => ({
  getStackState: vi.fn(),
  createStack: vi.fn(),
  runStackAction: vi.fn(),
}));

const stacks: Stack[] = [
  {
    id: "one",
    directoryName: "alpha",
    composeProjectName: "alpha",
    createdAt: "2026-01-01T00:00:00Z",
    updatedAt: "2026-01-01T00:00:00Z",
    lastDeploymentAt: "2026-09-20T10:00:00Z",
    state: { runtime: "running", freshness: "current", hasDeployed: true },
  },
  {
    id: "two",
    directoryName: "beta",
    composeProjectName: "beta",
    createdAt: "2026-01-01T00:00:00Z",
    updatedAt: "2026-01-01T00:00:00Z",
    state: { runtime: "running", freshness: "current", hasDeployed: true },
  },
];

afterEach(() => {
  vi.useRealTimers();
  vi.clearAllMocks();
});

function showDashboard(onOperationsAccepted = vi.fn()) {
  return render(
    <Dashboard
      stacks={stacks}
      repo={null}
      operations={[]}
      navigate={vi.fn()}
      refresh={vi.fn()}
      onOperationsAccepted={onOperationsAccepted}
    />,
  );
}

it("shows the six Stacks columns, readable states, and recorded deployment time", async () => {
  vi.mocked(getStackState).mockResolvedValue({
    runtime: "running",
    freshness: "current",
    hasDeployed: true,
  });
  showDashboard();
  expect(
    screen
      .getAllByRole("columnheader")
      .map((header) => header.textContent?.trim()),
  ).toEqual(["", "Name", "Runtime", "Remote", "Deployment", "Last deployment"]);
  expect(screen.queryByText("Containers")).toBeNull();
  expect(screen.queryByText("Working tree")).toBeNull();
  const alpha = within(
    screen.getByRole("link", { name: "alpha" }).closest("tr")!,
  );
  expect(await alpha.findByText("RUNNING")).toBeInTheDocument();
  expect(alpha.getByText("Current")).toBeInTheDocument();
  expect(alpha.getByText("Unknown")).toBeInTheDocument();
  expect(alpha.getByText(/2026/)).toBeInTheDocument();
  expect(alpha.getByText(/2026/).closest("time")).toHaveAttribute(
    "datetime",
    "2026-09-20T10:00:00Z",
  );
  const beta = within(
    screen.getByRole("link", { name: "beta" }).closest("tr")!,
  );
  expect(beta.getByText("Never")).toBeInTheDocument();
});

it("submits selected stacks separately and registers every accepted operation", async () => {
  vi.mocked(getStackState).mockResolvedValue({
    runtime: "running",
    freshness: "current",
    hasDeployed: true,
  });
  vi.mocked(runStackAction).mockImplementation(async (id, kind) => ({
    id: `op-${id}`,
    kind,
    scopeType: "stack",
    scopeId: id,
    status: "queued",
    outputTruncated: false,
  }));
  const accepted = vi.fn();
  showDashboard(accepted);
  fireEvent.click(screen.getByRole("checkbox", { name: "Select alpha" }));
  fireEvent.click(screen.getByRole("checkbox", { name: "Select beta" }));
  fireEvent.click(screen.getByRole("button", { name: "Actions" }));
  fireEvent.click(screen.getByRole("menuitem", { name: "Deploy" }));
  await waitFor(() => expect(runStackAction).toHaveBeenCalledTimes(2));
  expect(accepted).toHaveBeenCalledWith([
    expect.objectContaining({ scopeId: "one" }),
    expect.objectContaining({ scopeId: "two" }),
  ]);
  expect(screen.getByRole("status")).toHaveTextContent("alpha");
  expect(screen.getByRole("status")).toHaveTextContent("beta");
});

it("clears selection on filtering and disables runtime actions for mixed or unknown states", async () => {
  vi.mocked(getStackState).mockImplementation(async (id) => ({
    runtime: id === "one" ? "running" : "stopped",
    freshness: "current",
    hasDeployed: true,
  }));
  showDashboard();
  await screen.findByText("STOPPED");
  fireEvent.click(
    screen.getByRole("checkbox", { name: "Select all visible stacks" }),
  );
  fireEvent.click(screen.getByRole("button", { name: "Actions" }));
  expect(screen.getByRole("menuitem", { name: "Restart" })).toHaveAttribute(
    "aria-disabled",
    "true",
  );
  expect(screen.getByRole("menuitem", { name: "Stop" })).toHaveAttribute(
    "aria-disabled",
    "true",
  );
  fireEvent.keyDown(screen.getByRole("menu"), { key: "Escape" });
  fireEvent.input(screen.getByRole("textbox", { name: "Search stacks" }), {
    target: { value: "alpha" },
  });
  expect(
    screen.getByRole("checkbox", { name: "Select alpha" }),
  ).not.toBeChecked();
});

it("reports partial acceptance by stack name without opening an operation", async () => {
  vi.mocked(getStackState).mockResolvedValue({
    runtime: "running",
    freshness: "current",
    hasDeployed: true,
  });
  vi.mocked(runStackAction).mockImplementation(async (id, kind) => {
    if (id === "two") throw new Error("conflict");
    return {
      id: "op-one",
      kind,
      scopeType: "stack",
      scopeId: id,
      status: "queued",
      outputTruncated: false,
    };
  });
  const accepted = vi.fn();
  showDashboard(accepted);
  fireEvent.click(
    screen.getByRole("checkbox", { name: "Select all visible stacks" }),
  );
  fireEvent.click(screen.getByRole("button", { name: "Actions" }));
  fireEvent.click(screen.getByRole("menuitem", { name: "Deploy" }));
  await waitFor(() =>
    expect(accepted).toHaveBeenCalledWith([
      expect.objectContaining({ scopeId: "one" }),
    ]),
  );
  expect(screen.getByRole("status")).toHaveTextContent("alpha: accepted");
  expect(screen.getByRole("status")).toHaveTextContent("beta: conflict");
});

it("keeps the last deployment when Docker state becomes unavailable", async () => {
  vi.useFakeTimers();
  vi.mocked(getStackState)
    .mockResolvedValueOnce({
      runtime: "running",
      freshness: "current",
      hasDeployed: true,
    })
    .mockResolvedValueOnce({
      runtime: "running",
      freshness: "current",
      hasDeployed: true,
    })
    .mockRejectedValue(new Error("Docker offline"));
  showDashboard();
  await act(async () => {
    await Promise.resolve();
  });
  await act(async () => {
    await vi.advanceTimersByTimeAsync(5000);
  });
  const alpha = within(
    screen.getByRole("link", { name: "alpha" }).closest("tr")!,
  );
  expect(alpha.getByText("UNKNOWN")).toBeInTheDocument();
  expect(alpha.getByText(/2026/).closest("time")).toHaveAttribute(
    "datetime",
    "2026-09-20T10:00:00Z",
  );
});

it("limits select-all to twenty visible stacks", () => {
  const many = Array.from({ length: 21 }, (_, index) => ({
    ...stacks[0],
    id: "id-" + index,
    directoryName: "stack-" + index,
  }));
  render(
    <Dashboard
      stacks={many}
      repo={null}
      operations={[]}
      navigate={vi.fn()}
      refresh={vi.fn()}
      onOperationsAccepted={vi.fn()}
    />,
  );
  const selectAll = screen.getByRole("checkbox", {
    name: "Select all visible stacks",
  });
  expect(selectAll).toBeDisabled();
  expect(selectAll).toHaveAttribute("title", "Select at most 20 stacks");
});

it("updates each stack row after an external Docker state change", async () => {
  vi.useFakeTimers();
  const reads = new Map<string, number>();
  vi.mocked(getStackState).mockImplementation(async (id) => {
    const count = (reads.get(id) || 0) + 1;
    reads.set(id, count);
    return {
      runtime: id === "one" && count > 1 ? "stopped" : "running",
      freshness: "current",
      hasDeployed: true,
    };
  });
  render(
    <Dashboard
      stacks={stacks}
      repo={null}
      operations={[]}
      navigate={vi.fn()}
      refresh={vi.fn()}
      onOperationsAccepted={vi.fn()}
    />,
  );
  await act(async () => {
    await Promise.resolve();
  });
  const alpha = within(
    screen.getByRole("link", { name: "alpha" }).closest("tr")!,
  );
  const beta = within(
    screen.getByRole("link", { name: "beta" }).closest("tr")!,
  );
  expect(alpha.getByText("RUNNING")).toBeInTheDocument();
  expect(beta.getByText("RUNNING")).toBeInTheDocument();

  await act(async () => {
    await vi.advanceTimersByTimeAsync(5000);
  });
  expect(alpha.getByText("STOPPED")).toBeInTheDocument();
  expect(beta.getByText("RUNNING")).toBeInTheDocument();
});
