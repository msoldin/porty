import { fireEvent, render, screen, within } from "@testing-library/preact";
import { expect, it, vi } from "vitest";
import { ServicesTable } from "./ServicesTable";
import type { Container } from "./types";

const containers: Container[] = [
  {
    id: "full-id-a",
    service: "web",
    name: "web-1",
    state: "running",
    health: "healthy",
    image: "ghcr.io/example/web:1.4",
    networks: ["front", "back"],
    ports: [
      {
        host: "127.0.0.1",
        publishedPort: 8080,
        targetPort: 80,
        protocol: "tcp",
      },
    ],
  },
  {
    id: "full-id-b",
    service: "web",
    name: "web-2",
    state: "running",
    health: "unhealthy",
    image: "ghcr.io/example/web:1.4",
    networks: ["back"],
    ports: [
      { host: "::1", publishedPort: 8443, targetPort: 443, protocol: "tcp" },
    ],
  },
  {
    id: "full-id-c",
    service: "worker",
    name: "worker-1",
    state: "exited",
    health: "",
    image: "worker:2.0",
    networks: [],
    ports: [],
  },
];
const props = { error: "", busy: false, archived: false, dirty: false };

it("shows exact Services columns and keeps replicas and runtime metadata distinct", () => {
  render(
    <ServicesTable
      {...props}
      containers={containers}
      onBatchAction={vi.fn()}
    />,
  );
  expect(
    screen
      .getAllByRole("columnheader")
      .map((header) => header.textContent?.trim()),
  ).toEqual(["", "Service", "State", "Image", "Networks", "Ports"]);
  const second = within(
    screen.getByRole("checkbox", { name: "Select web-2" }).closest("tr")!,
  );
  expect(second.getByText("web")).toBeInTheDocument();
  expect(second.getByText("web-2")).toBeInTheDocument();
  expect(second.getByText("RUNNING")).toBeInTheDocument();
  expect(second.getByText("Unhealthy")).toHaveClass("danger");
  expect(second.getByText("ghcr.io/example/web:1.4")).toBeInTheDocument();
  expect(second.getByText("[::1]:8443 → 443/tcp")).toBeInTheDocument();
  const worker = within(
    screen.getByRole("checkbox", { name: "Select worker-1" }).closest("tr")!,
  );
  expect(worker.getByText("STOPPED")).toBeInTheDocument();
  expect(worker.getAllByText("—")).toHaveLength(2);
});

it("sends only selected full IDs and prevents actions for mixed states", () => {
  const onBatchAction = vi.fn();
  render(
    <ServicesTable
      {...props}
      containers={containers}
      onBatchAction={onBatchAction}
    />,
  );
  fireEvent.click(screen.getByRole("checkbox", { name: "Select web-2" }));
  fireEvent.click(screen.getByRole("button", { name: "Restart selected" }));
  expect(onBatchAction).toHaveBeenCalledWith(["full-id-b"], "restart");
  fireEvent.click(screen.getByRole("checkbox", { name: "Select worker-1" }));
  expect(screen.getByRole("button", { name: "Start selected" })).toBeDisabled();
  expect(
    screen.getByRole("button", { name: "Restart selected" }),
  ).toBeDisabled();
  expect(screen.getByRole("button", { name: "Stop selected" })).toBeDisabled();
});

it("searches service metadata, clears selection on search, and prunes disappeared IDs", () => {
  const view = render(
    <ServicesTable
      {...props}
      containers={containers}
      onBatchAction={vi.fn()}
    />,
  );
  fireEvent.click(screen.getByRole("checkbox", { name: "Select web-2" }));
  fireEvent.input(screen.getByRole("textbox", { name: "Search services" }), {
    target: { value: "worker:2.0" },
  });
  expect(screen.queryByRole("checkbox", { name: "Select web-2" })).toBeNull();
  expect(
    screen.getByRole("checkbox", { name: "Select worker-1" }),
  ).not.toBeChecked();
  fireEvent.input(screen.getByRole("textbox", { name: "Search services" }), {
    target: { value: "back" },
  });
  expect(screen.getAllByRole("row")).toHaveLength(3);
  fireEvent.input(screen.getByRole("textbox", { name: "Search services" }), {
    target: { value: "" },
  });
  fireEvent.click(screen.getByRole("checkbox", { name: "Select web-2" }));
  view.rerender(
    <ServicesTable
      {...props}
      containers={[containers[0], containers[2]]}
      onBatchAction={vi.fn()}
    />,
  );
  expect(screen.queryByText("1 container selected")).toBeNull();
});

it("blocks dirty, archived, busy, and unknown selections and shows loading or errors", () => {
  const onBatchAction = vi.fn();
  const view = render(
    <ServicesTable
      {...props}
      containers={[{ ...containers[0], state: "paused" }]}
      onBatchAction={onBatchAction}
    />,
  );
  fireEvent.click(screen.getByRole("checkbox", { name: "Select web-1" }));
  expect(
    screen.getByRole("button", { name: "Restart selected" }),
  ).toBeDisabled();
  view.rerender(
    <ServicesTable
      {...props}
      dirty
      containers={[containers[0]]}
      onBatchAction={onBatchAction}
    />,
  );
  expect(
    screen.getByRole("button", { name: "Restart selected" }),
  ).toBeDisabled();
  view.rerender(
    <ServicesTable
      {...props}
      archived
      containers={[containers[0]]}
      onBatchAction={onBatchAction}
    />,
  );
  expect(
    screen.getByRole("button", { name: "Restart selected" }),
  ).toBeDisabled();
  view.rerender(
    <ServicesTable
      {...props}
      busy
      containers={[containers[0]]}
      onBatchAction={onBatchAction}
    />,
  );
  expect(
    screen.getByRole("button", { name: "Restart selected" }),
  ).toBeDisabled();
  view.rerender(
    <ServicesTable
      {...props}
      containers={undefined}
      onBatchAction={onBatchAction}
    />,
  );
  expect(screen.getByRole("status")).toHaveTextContent("Loading containers");
  view.rerender(
    <ServicesTable
      {...props}
      containers={containers}
      error="Docker unavailable"
      onBatchAction={onBatchAction}
    />,
  );
  expect(screen.getByRole("alert")).toHaveTextContent("Docker unavailable");
  expect(screen.queryByRole("button", { name: "Restart selected" })).toBeNull();
  expect(onBatchAction).not.toHaveBeenCalled();
});

it("shows an empty deployment prompt and caps select-all at twenty services", () => {
  const view = render(
    <ServicesTable {...props} containers={[]} onBatchAction={vi.fn()} />,
  );
  expect(screen.getByText(/Deploy this stack/i)).toBeInTheDocument();
  const many = Array.from({ length: 21 }, (_, index) => ({
    ...containers[0],
    id: "id-" + index,
    name: "web-" + index,
  }));
  view.rerender(
    <ServicesTable {...props} containers={many} onBatchAction={vi.fn()} />,
  );
  const selectAll = screen.getByRole("checkbox", {
    name: "Select all visible services",
  });
  expect(selectAll).toBeDisabled();
  expect(selectAll).toHaveAttribute("title", "Select at most 20 services");
});

it("confirms Stop with the selected container count", () => {
  const confirm = vi.spyOn(window, "confirm").mockReturnValue(false);
  const onBatchAction = vi.fn();
  render(
    <ServicesTable
      {...props}
      containers={containers}
      onBatchAction={onBatchAction}
    />,
  );
  fireEvent.click(screen.getByRole("checkbox", { name: "Select web-1" }));
  fireEvent.click(screen.getByRole("checkbox", { name: "Select web-2" }));
  fireEvent.click(screen.getByRole("button", { name: "Stop selected" }));
  expect(confirm).toHaveBeenCalledWith("Stop 2 selected containers?");
  expect(onBatchAction).not.toHaveBeenCalled();
  confirm.mockRestore();
});
