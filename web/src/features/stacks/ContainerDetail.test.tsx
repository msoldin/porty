import { fireEvent, render, screen, waitFor } from "@testing-library/preact";
import { afterEach, expect, it, vi } from "vitest";
import { ContainerDetail } from "./ContainerDetail";
import {
  getContainerInspect,
  getContainerLogs,
  listStackContainers,
  runContainerAction,
} from "./api";
import type { Operation } from "../operations/types";
import type { Container, Stack } from "./types";

vi.mock("./api", () => ({
  listStackContainers: vi.fn(),
  runContainerAction: vi.fn(),
  getContainerLogs: vi.fn(),
  getContainerInspect: vi.fn(),
}));

const stack: Stack = {
  id: "stk-one",
  directoryName: "demo",
  composeProjectName: "demo",
  createdAt: "2026-01-01T00:00:00Z",
  updatedAt: "2026-01-01T00:00:00Z",
};

const running: Container = {
  id: "full-id-b",
  name: "web-2",
  service: "web",
  state: "running",
  health: "healthy",
  image: "example/web:2",
  networks: ["front", "back"],
  ports: [
    { host: "127.0.0.1", publishedPort: 8080, targetPort: 80, protocol: "tcp" },
  ],
};

function show(
  options: {
    containerId?: string;
    stack?: Stack;
    operations?: Operation[];
    dirty?: boolean;
    onAction?: (value: Operation) => void;
  } = {},
) {
  return render(
    <ContainerDetail
      stack={options.stack || stack}
      containerId={options.containerId || running.id}
      operations={options.operations || []}
      dirty={options.dirty || false}
      navigate={vi.fn()}
      onAction={options.onAction || vi.fn()}
    />,
  );
}

afterEach(() => {
  vi.clearAllMocks();
  vi.restoreAllMocks();
});

it("shows only the selected replica with its existing container details", async () => {
  vi.mocked(listStackContainers).mockResolvedValue([
    { ...running, id: "full-id-a", name: "web-1" },
    running,
  ]);
  show();
  expect(
    await screen.findByRole("heading", { name: "web-2" }),
  ).toBeInTheDocument();
  expect(screen.getByText("full-id-b")).toBeInTheDocument();
  expect(screen.getByText("example/web:2")).toBeInTheDocument();
  expect(screen.getByText("Healthy")).toBeInTheDocument();
  expect(screen.getByText("front, back")).toBeInTheDocument();
  expect(screen.getByText("127.0.0.1:8080 → 80/tcp")).toBeInTheDocument();
  expect(screen.queryByText("web-1")).toBeNull();
  expect(screen.getByRole("button", { name: "Stop" })).toBeEnabled();
  expect(screen.getByRole("button", { name: "Restart" })).toBeEnabled();
  expect(screen.getByRole("button", { name: "Start" })).toBeDisabled();
});

it("allows Start for exited containers and sends the exact ID", async () => {
  vi.mocked(listStackContainers).mockResolvedValue([
    { ...running, state: "exited", health: "" },
  ]);
  const operation: Operation = {
    id: "op-1",
    kind: "container_start",
    scopeType: "stack",
    scopeId: stack.id,
    status: "queued",
    outputTruncated: false,
  };
  vi.mocked(runContainerAction).mockResolvedValue(operation);
  const onAction = vi.fn();
  show({ onAction });
  fireEvent.click(await screen.findByRole("button", { name: "Start" }));
  await waitFor(() =>
    expect(runContainerAction).toHaveBeenCalledWith(
      "stk-one",
      "full-id-b",
      "start",
    ),
  );
  expect(onAction).toHaveBeenCalledWith(operation);
  expect(screen.getByRole("button", { name: "Stop" })).toBeDisabled();
});

it("disables actions for archived, dirty, busy, and unknown containers", async () => {
  vi.mocked(listStackContainers).mockResolvedValue([
    { ...running, state: "paused" },
  ]);
  const active: Operation = {
    id: "op-busy",
    kind: "deploy",
    scopeType: "stack",
    scopeId: stack.id,
    status: "running",
    outputTruncated: false,
  };
  show({
    stack: { ...stack, archivedAt: "2026-01-02T00:00:00Z" },
    operations: [active],
    dirty: true,
  });
  expect(await screen.findByRole("button", { name: "Start" })).toBeDisabled();
  expect(screen.getByRole("button", { name: "Stop" })).toBeDisabled();
  expect(screen.getByRole("button", { name: "Restart" })).toBeDisabled();
});

it("shows a vanished container without retaining actions", async () => {
  vi.mocked(listStackContainers).mockResolvedValue([
    { ...running, id: "replacement" },
  ]);
  show();
  expect(
    await screen.findByText("Container not found in this stack."),
  ).toBeInTheDocument();
  expect(
    screen.getByRole("link", { name: "Back to demo" }),
  ).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Stop" })).toBeNull();
});

it("confirms Stop and shows a request error without opening an operation", async () => {
  vi.mocked(listStackContainers).mockResolvedValue([running]);
  vi.spyOn(window, "confirm").mockReturnValue(true);
  vi.mocked(runContainerAction).mockRejectedValue(
    new Error("Docker unavailable"),
  );
  const onAction = vi.fn();
  show({ onAction });
  fireEvent.click(await screen.findByRole("button", { name: "Stop" }));
  expect(await screen.findByText("Docker unavailable")).toBeInTheDocument();
  expect(window.confirm).toHaveBeenCalledWith("Stop web-2?");
  expect(onAction).not.toHaveBeenCalled();
});

it("shows only this container's bounded Logs snapshot when the tab opens", async () => {
  vi.mocked(listStackContainers).mockResolvedValue([running]);
  vi.mocked(getContainerLogs).mockResolvedValue({
    output: "web-2 ready\n",
    truncated: true,
  });
  show();
  await screen.findByRole("heading", { name: "web-2" });
  expect(getContainerLogs).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole("tab", { name: "Logs" }));
  expect(await screen.findByText("web-2 ready")).toBeInTheDocument();
  expect(
    screen.getByText("Some log output was truncated."),
  ).toBeInTheDocument();
  expect(getContainerLogs).toHaveBeenCalledWith("stk-one", "full-id-b");
});

it("shows full inspect text on demand and copies the unchanged JSON", async () => {
  vi.mocked(listStackContainers).mockResolvedValue([running]);
  const raw = `{"Size":9007199254740993,"Config":{"Env":["TOKEN=secret"]}}`;
  vi.mocked(getContainerInspect).mockResolvedValue(raw);
  const writeText = vi.fn().mockResolvedValue(undefined);
  Object.defineProperty(navigator, "clipboard", {
    configurable: true,
    value: { writeText },
  });
  show();
  await screen.findByRole("heading", { name: "web-2" });
  expect(getContainerInspect).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole("tab", { name: "Inspect" }));
  expect(await screen.findByText(raw)).toBeInTheDocument();
  expect(getContainerInspect).toHaveBeenCalledTimes(1);
  fireEvent.click(screen.getByRole("button", { name: "Copy JSON" }));
  await waitFor(() => expect(writeText).toHaveBeenCalledWith(raw));
  fireEvent.click(screen.getByRole("tab", { name: "Overview" }));
  fireEvent.click(screen.getByRole("tab", { name: "Inspect" }));
  expect(getContainerInspect).toHaveBeenCalledTimes(1);
  fireEvent.click(screen.getByRole("button", { name: "Refresh inspect" }));
  await waitFor(() => expect(getContainerInspect).toHaveBeenCalledTimes(2));
});

it("shows an inspect read error without hiding the other tabs", async () => {
  vi.mocked(listStackContainers).mockResolvedValue([running]);
  vi.mocked(getContainerInspect).mockRejectedValue(
    new Error("Inspect unavailable"),
  );
  show();
  await screen.findByRole("heading", { name: "web-2" });
  fireEvent.click(screen.getByRole("tab", { name: "Inspect" }));
  expect(await screen.findByText("Inspect unavailable")).toBeInTheDocument();
  expect(screen.getByRole("tab", { name: "Overview" })).toBeInTheDocument();
});

it("does not show one container's inspect when the selected ID changes", async () => {
  vi.mocked(listStackContainers).mockResolvedValue([
    running,
    { ...running, id: "full-id-a", name: "web-1" },
  ]);
  let finishOld!: (value: string) => void;
  vi.mocked(getContainerInspect)
    .mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          finishOld = resolve;
        }),
    )
    .mockResolvedValueOnce('{"Name":"web-1"}');
  const view = show();
  await screen.findByRole("heading", { name: "web-2" });
  fireEvent.click(screen.getByRole("tab", { name: "Inspect" }));
  await waitFor(() =>
    expect(getContainerInspect).toHaveBeenCalledWith("stk-one", "full-id-b"),
  );
  view.rerender(
    <ContainerDetail
      stack={stack}
      containerId="full-id-a"
      operations={[]}
      dirty={false}
      navigate={vi.fn()}
      onAction={vi.fn()}
    />,
  );
  await waitFor(() =>
    expect(getContainerInspect).toHaveBeenCalledWith("stk-one", "full-id-a"),
  );
  finishOld('{"Name":"web-2"}');
  expect(await screen.findByText('{"Name":"web-1"}')).toBeInTheDocument();
  expect(screen.queryByText('{"Name":"web-2"}')).toBeNull();
});
