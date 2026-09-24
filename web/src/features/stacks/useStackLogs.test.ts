import { act, renderHook } from "@testing-library/preact";
import { afterEach, expect, it, vi } from "vitest";
import { refreshSession } from "../../lib/http";
import { runStackAction } from "./api";
import { useStackLogs } from "./useStackLogs";

vi.mock("./api", () => ({ runStackAction: vi.fn() }));
vi.mock("../../lib/http", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../../lib/http")>()),
  refreshSession: vi.fn().mockResolvedValue(undefined),
}));

class Socket {
  static instances: Socket[] = [];
  static OPEN = 1;
  readyState = Socket.OPEN;
  onopen: (() => void) | null = null;
  onclose: (() => void) | null = null;
  onmessage: ((event: { data: string }) => void) | null = null;
  onerror: (() => void) | null = null;
  sent: string[] = [];
  closed = false;
  constructor(_url: string) {
    Socket.instances.push(this);
  }
  send(value: string) {
    this.sent.push(value);
  }
  close() {
    this.closed = true;
    this.readyState = 3;
  }
  emit(value: unknown) {
    this.onmessage?.({ data: JSON.stringify(value) });
  }
}

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
  vi.clearAllMocks();
  Socket.instances = [];
});

it("does not overlap a slow log request and ignores its result after unmount", async () => {
  vi.useFakeTimers();
  vi.stubGlobal("WebSocket", Socket);
  let finish!: (value: never) => void;
  vi.mocked(runStackAction).mockImplementationOnce(
    () =>
      new Promise((resolve) => {
        finish = resolve;
      }),
  );
  const { unmount } = renderHook(() => useStackLogs("one", true));
  await act(async () => Socket.instances[0].onopen?.());
  await act(async () => {
    await vi.advanceTimersByTimeAsync(20000);
  });
  expect(runStackAction).toHaveBeenCalledTimes(1);
  unmount();
  await act(async () => finish({ id: "late" } as never));
  await act(async () => {
    await vi.advanceTimersByTimeAsync(10000);
  });
  expect(runStackAction).toHaveBeenCalledTimes(1);
  expect(Socket.instances[0].closed).toBe(true);
});

it("retries a failed snapshot and recovers from a stream gap", async () => {
  vi.useFakeTimers();
  vi.stubGlobal("WebSocket", Socket);
  vi.mocked(runStackAction)
    .mockRejectedValueOnce(new Error("runtime unavailable"))
    .mockResolvedValue({ id: "ok" } as never);
  const { result, unmount } = renderHook(() => useStackLogs("one", true));
  await act(async () => Socket.instances[0].onopen?.());
  expect(result.current.error).toContain("runtime unavailable");
  await act(async () => {
    await vi.advanceTimersByTimeAsync(5000);
  });
  expect(runStackAction).toHaveBeenCalledTimes(2);
  expect(result.current.error).toBe("");
  await act(async () =>
    Socket.instances[0].emit({
      type: "log",
      subscriptionId: "logs",
      sequence: 10,
      payload: { output: "current" },
    }),
  );
  await act(async () =>
    Socket.instances[0].emit({
      type: "log",
      subscriptionId: "logs",
      sequence: 9,
      payload: { output: "stale" },
    }),
  );
  expect(result.current.output).toBe("current");
  await act(async () =>
    Socket.instances[0].emit({
      type: "gap",
      subscriptionId: "logs",
      sequence: 11,
    }),
  );
  expect(result.current.gap).toBe(true);
  expect(result.current.output).toBe("current");
  expect(runStackAction).toHaveBeenCalledTimes(3);
  unmount();
});

it("retries failed session refresh while the Logs tab remains open", async () => {
  vi.useFakeTimers();
  vi.stubGlobal("WebSocket", Socket);
  vi.mocked(runStackAction).mockResolvedValue({ id: "ok" } as never);
  vi.mocked(refreshSession).mockRejectedValueOnce(
    new Error("session unavailable"),
  );
  const { result, unmount } = renderHook(() => useStackLogs("one", true));
  await act(async () => Socket.instances[0].onopen?.());
  await act(async () => Socket.instances[0].onclose?.());
  await act(async () => {
    await vi.advanceTimersByTimeAsync(1000);
  });
  expect(result.current.error).toContain("session unavailable");
  await act(async () => {
    await vi.advanceTimersByTimeAsync(2000);
  });
  expect(Socket.instances).toHaveLength(2);
  unmount();
});

it("closes the old stream and subscribes to a new stack", async () => {
  vi.useFakeTimers();
  vi.stubGlobal("WebSocket", Socket);
  vi.mocked(runStackAction).mockResolvedValue({ id: "ok" } as never);
  const { rerender, unmount } = renderHook(({ id }) => useStackLogs(id, true), {
    initialProps: { id: "one" },
  });
  await act(async () => Socket.instances[0].onopen?.());
  rerender({ id: "two" });
  expect(Socket.instances[0].closed).toBe(true);
  await act(async () => Socket.instances[1].onopen?.());
  expect(JSON.parse(Socket.instances[1].sent[0]).topic).toBe("logs:two");
  unmount();
});

it("refreshes after a malformed log event without losing visible output", async () => {
  vi.useFakeTimers();
  vi.stubGlobal("WebSocket", Socket);
  vi.mocked(runStackAction).mockResolvedValue({ id: "ok" } as never);
  const { result, unmount } = renderHook(() => useStackLogs("one", true));
  await act(async () => Socket.instances[0].onopen?.());
  await act(async () =>
    Socket.instances[0].emit({
      type: "log",
      subscriptionId: "logs",
      sequence: 1,
      payload: { output: "safe output" },
    }),
  );
  await act(async () =>
    Socket.instances[0].emit({
      type: "log",
      subscriptionId: "logs",
      sequence: 2,
      payload: { output: 123 },
    }),
  );
  expect(result.current.output).toBe("safe output");
  expect(result.current.gap).toBe(true);
  expect(runStackAction).toHaveBeenCalledTimes(2);
  unmount();
});

it("loads logs on entry and keeps refreshing while the tab is open", async () => {
  vi.useFakeTimers();
  vi.stubGlobal("WebSocket", Socket);
  vi.mocked(runStackAction).mockResolvedValue({
    id: "op1",
    kind: "logs",
    scopeType: "stack",
    scopeId: "one",
    status: "running",
    outputTruncated: false,
  });
  const { result, unmount } = renderHook(() => useStackLogs("one", true));
  expect(result.current.status).toBe("loading");
  await act(async () => Socket.instances[0].onopen?.());
  expect(JSON.parse(Socket.instances[0].sent[0])).toMatchObject({
    topic: "logs:one",
    since: 0,
  });
  expect(runStackAction).toHaveBeenCalledWith("one", "logs");
  await act(async () =>
    Socket.instances[0].emit({
      type: "log",
      subscriptionId: "logs",
      sequence: 1,
      payload: { output: "first line\n" },
    }),
  );
  expect(result.current.output).toBe("first line\n");
  expect(result.current.status).toBe("connected");
  await act(async () => {
    await vi.advanceTimersByTimeAsync(5000);
  });
  expect(runStackAction).toHaveBeenCalledTimes(2);
  unmount();
  expect(Socket.instances[0].closed).toBe(true);
  await act(async () => {
    await vi.advanceTimersByTimeAsync(10000);
  });
  expect(runStackAction).toHaveBeenCalledTimes(2);
});

it("shows errors and reconnects after a stream interruption", async () => {
  vi.useFakeTimers();
  vi.stubGlobal("WebSocket", Socket);
  vi.mocked(runStackAction)
    .mockRejectedValueOnce(new Error("runtime unavailable"))
    .mockResolvedValue({
      id: "op2",
      kind: "logs",
      scopeType: "stack",
      scopeId: "one",
      status: "running",
      outputTruncated: false,
    });
  const { result, unmount } = renderHook(() => useStackLogs("one", true));
  await act(async () => Socket.instances[0].onopen?.());
  expect(result.current.error).toContain("runtime unavailable");
  await act(async () => Socket.instances[0].onclose?.());
  expect(result.current.status).toBe("reconnecting");
  await act(async () => {
    await vi.advanceTimersByTimeAsync(1000);
  });
  expect(Socket.instances).toHaveLength(2);
  await act(async () => Socket.instances[1].onopen?.());
  expect(runStackAction).toHaveBeenCalledTimes(2);
  expect(result.current.error).toBe("");
  unmount();
});
