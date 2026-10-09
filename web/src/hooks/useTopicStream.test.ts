import { act, renderHook } from "@testing-library/preact";
import { afterEach, it, expect, vi } from "vitest";
import { useTopicStream } from "./useTopicStream";
import { useOperationStream } from "../features/operations/useOperationStream";
import { refreshSession } from "../lib/http";
vi.mock("../lib/http", () => ({
  refreshSession: vi.fn().mockResolvedValue(undefined),
}));
class Socket {
  static instances: Socket[] = [];
  onopen = () => {};
  onclose = () => {};
  onerror = () => {};
  onmessage = (_event: { data: string }) => {};
  send = vi.fn();
  close = vi.fn();
  constructor() {
    Socket.instances.push(this);
  }
}
afterEach(() => {
  vi.unstubAllGlobals();
  vi.useRealTimers();
  vi.clearAllMocks();
  Socket.instances = [];
});
it("refreshes after a stream gap and reconnects with an authoritative read", async () => {
  vi.useFakeTimers();
  vi.stubGlobal("WebSocket", Socket);
  const refresh = vi.fn(),
    event = vi.fn();
  const { unmount } = renderHook(() =>
    useTopicStream("alerts", event, refresh),
  );
  const socket = Socket.instances[0];
  act(() => socket.onopen());
  expect(refresh).toHaveBeenCalledTimes(1);
  act(() =>
    socket.onmessage({
      data: JSON.stringify({
        type: "alert",
        subscriptionId: "alerts",
        sequence: 4,
        payload: { id: "a" },
      }),
    }),
  );
  expect(event).toHaveBeenCalledTimes(1);
  act(() =>
    socket.onmessage({
      data: JSON.stringify({ type: "gap", subscriptionId: "alerts" }),
    }),
  );
  expect(refresh).toHaveBeenCalledTimes(2);
  act(() => socket.onclose());
  await act(async () => {
    await vi.advanceTimersByTimeAsync(1000);
  });
  expect(refreshSession).toHaveBeenCalledTimes(1);
  act(() => Socket.instances[1].onopen());
  expect(refresh).toHaveBeenCalledTimes(3);
  expect(JSON.parse(Socket.instances[1].send.mock.calls[0][0])).toMatchObject({
    topic: "alerts",
    since: 0,
  });
  unmount();
  expect(Socket.instances[1].close).toHaveBeenCalled();
});
it("preserves operation filtering and deduplicates replayed revisions", () => {
  vi.stubGlobal("WebSocket", Socket);
  const operation = vi.fn();
  renderHook(() => useOperationStream(operation, vi.fn()));
  const socket = Socket.instances[0];
  for (const topic of ["alerts", "operations", "operations"])
    act(() =>
      socket.onmessage({
        data: JSON.stringify({
          type: "operation",
          subscriptionId: topic,
          sequence: 1,
          payload: { id: "old-op", status: "failed" },
        }),
      }),
    );
  expect(operation).toHaveBeenCalledExactlyOnceWith({
    id: "old-op",
    status: "failed",
  });
});

it("opens no disabled stream and ignores callbacks after disabling", () => {
  vi.stubGlobal("WebSocket", Socket);
  const event = vi.fn(),
    refresh = vi.fn();
  const view = renderHook(
    ({ enabled }) => useTopicStream("operations", event, refresh, enabled),
    { initialProps: { enabled: false } },
  );
  expect(Socket.instances).toHaveLength(0);
  view.rerender({ enabled: true });
  const socket = Socket.instances[0];
  act(() => socket.onopen());
  expect(refresh).toHaveBeenCalledTimes(1);
  view.rerender({ enabled: false });
  expect(socket.close).toHaveBeenCalled();
  act(() => {
    socket.onopen();
    socket.onmessage({
      data: JSON.stringify({
        subscriptionId: "operations",
        sequence: 1,
        type: "operation",
      }),
    });
  });
  expect(refresh).toHaveBeenCalledTimes(1);
  expect(event).not.toHaveBeenCalled();
});
