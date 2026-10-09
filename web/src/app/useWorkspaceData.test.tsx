import { act, renderHook, waitFor } from "@testing-library/preact";
import { expect, it, vi } from "vitest";
import { useWorkspaceData } from "./useWorkspaceData";
import { listAudit } from "../features/audit/api";
vi.mock("../features/stacks/api", () => ({
  listStacks: vi.fn().mockResolvedValue([]),
  listStacksWithState: vi.fn().mockResolvedValue([]),
}));
vi.mock("../features/repository/api", () => ({
  getRepositoryStatus: vi.fn().mockResolvedValue(null),
  listCommits: vi.fn().mockResolvedValue([]),
}));
vi.mock("../features/operations/api", () => ({
  listOperations: vi.fn().mockResolvedValue([]),
}));
vi.mock("../features/audit/api", () => ({ listAudit: vi.fn() }));
vi.mock("../features/operations/useOperationStream", () => ({
  useOperationStream: () => ({ connection: "Connected", gap: false }),
}));
it("reports an initial audit failure instead of an empty log and marks retained records stale", async () => {
  vi.mocked(listAudit)
    .mockRejectedValueOnce(new Error("Audit unavailable"))
    .mockResolvedValueOnce([{ id: "one" } as never])
    .mockRejectedValueOnce(new Error("Audit offline"));
  const view = renderHook(() => useWorkspaceData(vi.fn()));
  await waitFor(() =>
    expect(view.result.current.error).toContain("Audit unavailable"),
  );
  expect(view.result.current.resources.audit).toEqual(
    expect.objectContaining({
      error: "Audit unavailable",
      stale: false,
      loaded: false,
    }),
  );
  await act(async () => view.result.current.refresh());
  await act(async () => view.result.current.refresh());
  expect(view.result.current.audit).toEqual([{ id: "one" }]);
  expect(view.result.current.resources.audit).toEqual(
    expect.objectContaining({
      error: "Audit offline",
      stale: true,
      loaded: true,
    }),
  );
});
