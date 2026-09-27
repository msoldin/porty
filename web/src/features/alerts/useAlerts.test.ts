import { act, renderHook } from "@testing-library/preact";
import { it, expect, vi, afterEach } from "vitest";
import { useAlerts } from "./useAlerts";
import { listAlerts, acknowledgeAlert } from "./api";
import { useTopicStream } from "../../hooks/useTopicStream";
vi.mock("./api", () => ({
  listAlerts: vi.fn(),
  acknowledgeAlert: vi.fn(),
  resolveAlert: vi.fn(),
}));
vi.mock("../../hooks/useTopicStream", () => ({ useTopicStream: vi.fn() }));
afterEach(() => vi.clearAllMocks());
const page = { items: [], total: 0, unacknowledgedCount: 3 };
it("does not overwrite a newer response with a stale request", async () => {
  let finish!: (value: typeof page) => void;
  vi.mocked(listAlerts)
    .mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          finish = resolve;
        }),
    )
    .mockResolvedValue(page);
  const { result, rerender } = renderHook(({ id }) => useAlerts(id), {
    initialProps: { id: "old" },
  });
  await act(async () => {});
  rerender({ id: "new" });
  await act(async () => {});
  expect(result.current.page?.unacknowledgedCount).toBe(3);
  await act(async () => {
    finish({ ...page, unacknowledgedCount: 8 });
  });
  expect(result.current.page?.unacknowledgedCount).toBe(3);
});
it("refreshes after a stream gap", async () => {
  vi.mocked(listAlerts).mockResolvedValue(page);
  renderHook(() => useAlerts());
  await act(async () => {});
  const refresh = vi.mocked(useTopicStream).mock.calls.at(-1)![2];
  await act(async () => {
    refresh();
  });
  expect(listAlerts).toHaveBeenCalledTimes(2);
});
it("reloads the server badge count after acknowledgment", async () => {
  vi.mocked(listAlerts)
    .mockResolvedValueOnce(page)
    .mockResolvedValue({ ...page, unacknowledgedCount: 2 });
  const { result } = renderHook(() => useAlerts());
  await act(async () => {});
  await act(async () => {
    await result.current.acknowledge({ id: "a", revision: 2 } as never);
  });
  expect(acknowledgeAlert).toHaveBeenCalledWith("a", 2);
  expect(result.current.page?.unacknowledgedCount).toBe(2);
});
