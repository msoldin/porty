import { act, render, screen, within } from "@testing-library/preact";
import { afterEach, expect, it, vi } from "vitest";
import { Dashboard } from "./Dashboard";
import { getStackState } from "./api";
import type { Stack } from "./types";

vi.mock("./api", () => ({ getStackState: vi.fn(), createStack: vi.fn() }));

const stacks: Stack[] = [
  {
    id: "one",
    directoryName: "alpha",
    composeProjectName: "alpha",
    createdAt: "2026-01-01T00:00:00Z",
    updatedAt: "2026-01-01T00:00:00Z",
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
  expect(alpha.getByText("running")).toBeInTheDocument();
  expect(beta.getByText("running")).toBeInTheDocument();

  await act(async () => {
    await vi.advanceTimersByTimeAsync(5000);
  });
  expect(alpha.getByText("stopped")).toBeInTheDocument();
  expect(beta.getByText("running")).toBeInTheDocument();
});
