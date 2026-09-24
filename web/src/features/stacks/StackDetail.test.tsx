import { fireEvent, render, screen } from "@testing-library/preact";
import { afterEach, expect, it, vi } from "vitest";
import { StackDetail } from "./StackDetail";
import { useStackLogs } from "./useStackLogs";
import type { Stack } from "./types";

vi.mock("./useStackLogs", () => ({ useStackLogs: vi.fn() }));
vi.mock("./api", () => ({
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

afterEach(() => vi.clearAllMocks());

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
