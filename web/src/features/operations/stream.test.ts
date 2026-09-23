import { render } from "@testing-library/preact";
import { h } from "preact";
import { afterEach, expect, it, vi } from "vitest";
import { reduceStream } from "./streamState";
import { useOperationStream } from "./useOperationStream";
import { refreshSession } from "../../lib/http";

vi.mock("../../lib/http", () => ({
  refreshSession: vi.fn().mockResolvedValue(undefined),
}));

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
  vi.clearAllMocks();
});

it("refreshes access before reconnecting an expired idle stream", async () => {
  vi.useFakeTimers();
  class Socket {
    static instances: Socket[] = [];
    onopen: (() => void) | null = null;
    onclose: (() => void) | null = null;
    onmessage: (() => void) | null = null;
    onerror: (() => void) | null = null;
    constructor(_url: string) {
      Socket.instances.push(this);
    }
    send(_value: string) {}
    close() {}
  }
  vi.stubGlobal("WebSocket", Socket);
  function Probe() {
    useOperationStream(
      () => {},
      () => {},
    );
    return null;
  }
  const rendered = render(h(Probe, {}));
  expect(Socket.instances).toHaveLength(1);
  Socket.instances[0].onclose?.();
  await vi.advanceTimersByTimeAsync(1000);
  expect(refreshSession).toHaveBeenCalledOnce();
  expect(Socket.instances).toHaveLength(2);
  rendered.unmount();
});

it("marks explicit stream gaps without claiming output is complete", () => {
  const next = reduceStream(
    { sequence: 5, gap: false, output: "before\n" },
    { type: "gap", sequence: 0, payload: { since: 5 } },
  );
  expect(next.gap).toBe(true);
  expect(next.sequence).toBe(0);
  expect(next.output).toBe("before\n");
});
it("ignores replay duplicates and caps operation output", () => {
  const prior = { sequence: 5, gap: false, output: "before\n" };
  expect(
    reduceStream(prior, {
      type: "operation",
      sequence: 4,
      payload: { output: "old" },
    }),
  ).toEqual(prior);
  const next = reduceStream(prior, {
    type: "operation",
    sequence: 8,
    payload: { output: "x".repeat(100000) },
  });
  expect(next.sequence).toBe(8);
  expect(next.output).toBe("x".repeat(65536));
});
