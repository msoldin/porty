import {
  act,
  fireEvent,
  render,
  screen,
  within,
} from "@testing-library/preact";
import { afterEach, expect, it, vi } from "vitest";
import { OverviewServices } from "./OverviewServices";
import { useOverviewContainers } from "./useOverviewContainers";
import type { OverviewSnapshot } from "./overviewContainers";
import type { Container, Stack } from "./types";

vi.mock("./useOverviewContainers", () => ({ useOverviewContainers: vi.fn() }));

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
