import { fireEvent, render, screen } from "@testing-library/preact";
import { expect, it, vi } from "vitest";
import { ContainerList } from "./ContainerList";
import type { Container } from "./types";

const containers: Container[] = [
  {
    id: "id-a",
    name: "app-1",
    service: "app",
    state: "running",
    health: "healthy",
  },
  {
    id: "id-b",
    name: "app-2",
    service: "app",
    state: "running",
    health: "healthy",
  },
  {
    id: "id-c",
    name: "worker-1",
    service: "worker",
    state: "exited",
    health: "",
  },
];

it("shows every replica and only actions allowed by its state", () => {
  const onAction = vi.fn();
  render(
    <ContainerList
      containers={containers}
      error=""
      busy={false}
      archived={false}
      onAction={onAction}
    />,
  );
  for (const name of [
    "Stop app-1",
    "Restart app-1",
    "Stop app-2",
    "Restart app-2",
    "Start worker-1",
  ])
    expect(screen.getByRole("button", { name })).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Start app-1" })).toBeNull();
  expect(screen.queryByRole("button", { name: "Stop worker-1" })).toBeNull();
  fireEvent.click(screen.getByRole("button", { name: "Restart app-2" }));
  expect(onAction).toHaveBeenCalledWith("id-b", "restart");
});

it("does not offer actions for paused containers or archived stacks", () => {
  const onAction = vi.fn();
  const view = render(
    <ContainerList
      containers={[{ ...containers[0], state: "paused" }]}
      error=""
      busy={false}
      archived={false}
      onAction={onAction}
    />,
  );
  expect(screen.getByText("app-1")).toBeInTheDocument();
  expect(screen.queryByRole("button")).toBeNull();
  view.rerender(
    <ContainerList
      containers={containers}
      error=""
      busy={false}
      archived={true}
      onAction={onAction}
    />,
  );
  expect(screen.queryByRole("button")).toBeNull();
});

it("disables actions while another operation runs", () => {
  render(
    <ContainerList
      containers={containers}
      error=""
      busy={true}
      archived={false}
      onAction={vi.fn()}
    />,
  );
  expect(screen.getByRole("button", { name: "Stop app-1" })).toBeDisabled();
});

it("shows loading, empty, and error states without stale controls", () => {
  const props = { error: "", busy: false, archived: false, onAction: vi.fn() };
  const view = render(<ContainerList {...props} containers={undefined} />);
  expect(screen.getByRole("status")).toHaveTextContent("Loading containers");
  view.rerender(<ContainerList {...props} containers={[]} />);
  expect(screen.getByText(/Deploy this stack/i)).toBeInTheDocument();
  view.rerender(
    <ContainerList
      {...props}
      containers={containers}
      error="Docker unavailable"
    />,
  );
  expect(screen.getByRole("alert")).toHaveTextContent("Docker unavailable");
  expect(screen.queryByRole("button")).toBeNull();
});
