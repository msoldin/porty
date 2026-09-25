import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/preact";
import { afterEach, expect, it, vi } from "vitest";
import { OverviewServices } from "./OverviewServices";
import { useOverviewContainers } from "./useOverviewContainers";
import { runContainerBatchAction } from "./api";
import type { Operation } from "../operations/types";
import type { OverviewSnapshot } from "./overviewContainers";
import type { Container, Stack } from "./types";

vi.mock("./useOverviewContainers", () => ({ useOverviewContainers: vi.fn() }));
vi.mock("./api", () => ({ runContainerBatchAction: vi.fn() }));

const stacks: Stack[] = [
  {
    id: "one",
    directoryName: "alpha",
    composeProjectName: "alpha",
    createdAt: "",
    updatedAt: "",
  },
  {
    id: "two",
    directoryName: "beta",
    composeProjectName: "beta",
    createdAt: "",
    updatedAt: "",
  },
  {
    id: "old",
    directoryName: "old-app",
    composeProjectName: "old",
    archivedAt: "2026-09-01T00:00:00Z",
    createdAt: "",
    updatedAt: "",
  },
];

const container = (
  id: string,
  service: string,
  state: string,
  health = "",
): Container => ({
  id,
  name: id,
  service,
  state,
  health,
  image: service + ":2",
  networks: service === "web" ? ["front"] : [],
  ports:
    service === "web"
      ? [
          {
            host: "127.0.0.1",
            publishedPort: 8080,
            targetPort: 80,
            protocol: "tcp",
          },
        ]
      : [],
});

const web1 = container("web-1", "web", "running", "healthy");
const web2 = container("web-2", "web", "running", "unhealthy");
const worker = container("worker-1", "worker", "exited");
const old = container("old-1", "legacy", "running");

const loaded: OverviewSnapshot = {
  rows: [
    { stackId: "one", container: web1 },
    { stackId: "one", container: web2 },
    { stackId: "two", container: worker },
    { stackId: "old", container: old },
  ],
  errors: [],
  loading: false,
};

function show(snapshot: OverviewSnapshot = loaded) {
  vi.mocked(useOverviewContainers).mockReturnValue(snapshot);
  return render(
    <OverviewServices
      stacks={stacks}
      operations={[]}
      navigate={vi.fn()}
      onOperationsAccepted={vi.fn()}
    />,
  );
}

afterEach(() => vi.clearAllMocks());

it("shows every stack's containers as distinct searchable rows", () => {
  show();
  const table = within(
    screen.getByRole("region", { name: "All services table" }),
  );
  expect(table.getAllByRole("row")).toHaveLength(5);
  expect(
    table
      .getAllByRole("columnheader")
      .map((header) => header.textContent?.trim()),
  ).toEqual(["", "Stack", "Service", "State", "Image", "Networks", "Ports"]);
  const replica = within(
    table.getByRole("checkbox", { name: "Select alpha web-2" }).closest("tr")!,
  );
  expect(replica.getByText("web")).toBeInTheDocument();
  expect(replica.getByText("web-2")).toBeInTheDocument();
  expect(replica.getByText("Unhealthy")).toBeInTheDocument();
  expect(replica.getByText("web:2")).toBeInTheDocument();
  expect(replica.getByText("127.0.0.1:8080 → 80/tcp")).toBeInTheDocument();
  expect(table.getAllByRole("link", { name: "alpha" })[0]).toHaveAttribute(
    "href",
    "#/stacks/one",
  );
  expect(
    within(
      table
        .getByRole("checkbox", { name: "Select old-app old-1" })
        .closest("tr")!,
    ).getByText("Archived"),
  ).toBeInTheDocument();
  expect(
    table.getByRole("checkbox", { name: "Select old-app old-1" }),
  ).toBeDisabled();
  expect(screen.getByText(/4 containers/)).toBeInTheDocument();
  expect(screen.getByText(/3 running/)).toBeInTheDocument();
});

it("filters services independently by stack, metadata, and state", () => {
  show();
  const search = screen.getByRole("textbox", { name: "Search all services" });
  fireEvent.input(search, { target: { value: "beta" } });
  expect(screen.getAllByRole("row")).toHaveLength(2);
  expect(screen.getByText("worker-1")).toBeInTheDocument();
  fireEvent.input(search, { target: { value: "front" } });
  expect(screen.getAllByRole("row")).toHaveLength(3);
  fireEvent.input(search, { target: { value: "" } });
  fireEvent.change(
    screen.getByRole("combobox", { name: "Filter services state" }),
    { target: { value: "unhealthy" } },
  );
  expect(screen.getAllByRole("row")).toHaveLength(2);
  expect(screen.getByText("web-2")).toBeInTheDocument();
});

it("removes selected rows that disappear or stop matching after polling", async () => {
  const view = show();
  fireEvent.input(
    screen.getByRole("textbox", { name: "Search all services" }),
    { target: { value: "front" } },
  );
  fireEvent.click(screen.getByRole("checkbox", { name: "Select alpha web-1" }));
  expect(screen.getByText("1 container selected")).toBeInTheDocument();
  vi.mocked(useOverviewContainers).mockReturnValue({
    ...loaded,
    rows: [{ stackId: "one", container: { ...web1, networks: ["back"] } }],
  });
  await act(async () =>
    view.rerender(
      <OverviewServices
        stacks={stacks}
        operations={[]}
        navigate={vi.fn()}
        onOperationsAccepted={vi.fn()}
      />,
    ),
  );
  expect(screen.queryByText("1 container selected")).toBeNull();
  expect(
    screen.getByText("No services match your filters."),
  ).toBeInTheDocument();
});

it("marks partial reads and distinguishes loading from an empty inventory", () => {
  const view = show({ rows: [], errors: [], loading: true });
  expect(screen.getByRole("status")).toHaveTextContent("Loading services");
  vi.mocked(useOverviewContainers).mockReturnValue({
    rows: [],
    errors: [],
    loading: false,
  });
  view.rerender(
    <OverviewServices
      stacks={stacks}
      operations={[]}
      navigate={vi.fn()}
      onOperationsAccepted={vi.fn()}
    />,
  );
  expect(screen.getByText("No containers exist yet.")).toBeInTheDocument();
  vi.mocked(useOverviewContainers).mockReturnValue({
    rows: [{ stackId: "one", container: web1 }],
    errors: [{ stackId: "two", message: "Docker unavailable" }],
    loading: false,
  });
  view.rerender(
    <OverviewServices
      stacks={stacks}
      operations={[]}
      navigate={vi.fn()}
      onOperationsAccepted={vi.fn()}
    />,
  );
  expect(screen.getByText(/Partial inventory/)).toHaveTextContent(
    "beta: Docker unavailable",
  );
  expect(
    screen.getByRole("checkbox", { name: "Select alpha web-1" }),
  ).toBeInTheDocument();
});

it("disables select-all above twenty visible containers", () => {
  show({
    rows: Array.from({ length: 21 }, (_, index) => ({
      stackId: "one",
      container: container("item-" + index, "web", "running"),
    })),
    errors: [],
    loading: false,
  });
  expect(
    screen.getByRole("checkbox", { name: "Select all visible services" }),
  ).toBeDisabled();
});

it("submits full container IDs once per owning stack", async () => {
  const accepted: Operation[] = [];
  const onOperationsAccepted = vi.fn();
  vi.mocked(useOverviewContainers).mockReturnValue({
    ...loaded,
    rows: [
      { stackId: "one", container: web1 },
      { stackId: "one", container: web2 },
      { stackId: "two", container: { ...worker, state: "running" } },
    ],
  });
  vi.mocked(runContainerBatchAction).mockImplementation(async (id) => {
    const operation: Operation = {
      id,
      kind: "container_batch_restart",
      scopeType: "stack",
      scopeId: id,
      status: "queued",
      outputTruncated: false,
    };
    accepted.push(operation);
    return operation;
  });
  render(
    <OverviewServices
      stacks={stacks}
      operations={[]}
      navigate={vi.fn()}
      onOperationsAccepted={onOperationsAccepted}
    />,
  );
  for (const name of ["alpha web-1", "alpha web-2", "beta worker-1"]) {
    fireEvent.click(screen.getByRole("checkbox", { name: "Select " + name }));
  }
  fireEvent.click(screen.getByRole("button", { name: "Restart selected" }));
  await waitFor(() => expect(runContainerBatchAction).toHaveBeenCalledTimes(2));
  expect(runContainerBatchAction).toHaveBeenCalledWith(
    "one",
    ["web-1", "web-2"],
    "restart",
  );
  expect(runContainerBatchAction).toHaveBeenCalledWith(
    "two",
    ["worker-1"],
    "restart",
  );
  expect(onOperationsAccepted).toHaveBeenCalledWith(accepted);
});

it("enables start only for stopped rows and blocks actions during stack operations", () => {
  const active: Operation = {
    id: "active",
    kind: "deploy",
    scopeType: "stack",
    scopeId: "one",
    status: "running",
    outputTruncated: false,
  };
  vi.mocked(useOverviewContainers).mockReturnValue(loaded);
  const view = render(
    <OverviewServices
      stacks={stacks}
      operations={[]}
      navigate={vi.fn()}
      onOperationsAccepted={vi.fn()}
    />,
  );
  fireEvent.click(
    screen.getByRole("checkbox", { name: "Select beta worker-1" }),
  );
  expect(screen.getByRole("button", { name: "Start selected" })).toBeEnabled();
  expect(screen.getByRole("button", { name: "Stop selected" })).toBeDisabled();
  fireEvent.click(screen.getByRole("checkbox", { name: "Select alpha web-1" }));
  expect(screen.getByRole("button", { name: "Start selected" })).toBeDisabled();
  expect(
    screen.getByRole("button", { name: "Restart selected" }),
  ).toBeDisabled();
  fireEvent.click(
    screen.getByRole("checkbox", { name: "Select beta worker-1" }),
  );
  view.rerender(
    <OverviewServices
      stacks={stacks}
      operations={[active]}
      navigate={vi.fn()}
      onOperationsAccepted={vi.fn()}
    />,
  );
  expect(
    screen.getByRole("button", { name: "Restart selected" }),
  ).toBeDisabled();
  expect(
    screen.getByRole("checkbox", { name: "Select old-app old-1" }),
  ).toBeDisabled();
});

it("confirms stop and retains only selections from failed stack requests", async () => {
  const confirm = vi.spyOn(window, "confirm").mockReturnValue(true);
  const onOperationsAccepted = vi.fn();
  vi.mocked(useOverviewContainers).mockReturnValue({
    ...loaded,
    rows: [
      { stackId: "one", container: web1 },
      { stackId: "two", container: { ...worker, state: "running" } },
    ],
  });
  vi.mocked(runContainerBatchAction).mockImplementation(async (id) => {
    if (id === "two") throw new Error("Docker unavailable");
    return {
      id: "op-one",
      kind: "container_batch_stop",
      scopeType: "stack",
      scopeId: id,
      status: "queued",
      outputTruncated: false,
    };
  });
  render(
    <OverviewServices
      stacks={stacks}
      operations={[]}
      navigate={vi.fn()}
      onOperationsAccepted={onOperationsAccepted}
    />,
  );
  fireEvent.click(screen.getByRole("checkbox", { name: "Select alpha web-1" }));
  fireEvent.click(
    screen.getByRole("checkbox", { name: "Select beta worker-1" }),
  );
  fireEvent.click(screen.getByRole("button", { name: "Stop selected" }));
  expect(confirm).toHaveBeenCalledWith(
    "Stop 2 selected containers across 2 stacks?",
  );
  await waitFor(() => expect(onOperationsAccepted).toHaveBeenCalledTimes(1));
  expect(
    screen.getByRole("checkbox", { name: "Select alpha web-1" }),
  ).not.toBeChecked();
  expect(
    screen.getByRole("checkbox", { name: "Select beta worker-1" }),
  ).toBeChecked();
  expect(screen.getByRole("status")).toHaveTextContent("alpha: accepted");
  expect(screen.getByRole("status")).toHaveTextContent(
    "beta: Docker unavailable",
  );
  confirm.mockRestore();
});

it("refreshes after a container operation completes", () => {
  const operation: Operation = {
    id: "op-1",
    kind: "container_batch_restart",
    scopeType: "stack",
    scopeId: "one",
    status: "running",
    outputTruncated: false,
  };
  const view = show();
  expect(vi.mocked(useOverviewContainers).mock.lastCall?.[1]).toBe("");
  view.rerender(
    <OverviewServices
      stacks={stacks}
      operations={[{ ...operation, status: "succeeded" }]}
      navigate={vi.fn()}
      onOperationsAccepted={vi.fn()}
    />,
  );
  expect(vi.mocked(useOverviewContainers).mock.lastCall?.[1]).toBe(
    "op-1:succeeded",
  );
});

it("disables actions when the selected container state is unknown", () => {
  show({
    ...loaded,
    rows: [{ stackId: "one", container: { ...web1, state: "unknown" } }],
  });
  fireEvent.click(screen.getByRole("checkbox", { name: "Select alpha web-1" }));
  for (const name of ["Start selected", "Stop selected", "Restart selected"]) {
    expect(screen.getByRole("button", { name })).toBeDisabled();
  }
});
