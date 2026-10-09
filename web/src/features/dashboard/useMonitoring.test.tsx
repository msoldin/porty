import { act, cleanup, render, screen } from "@testing-library/preact";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { useMonitoring } from "./useMonitoring";
import { getMonitoring } from "./api";
import { snapshotFixture } from "./testFixtures";
import { APIError } from "../../lib/http";
vi.mock("./api", () => ({ getMonitoring: vi.fn() }));

function ClockProbe() {
  const result = useMonitoring(() => {});
  return <span data-testid="server-clock">{result.serverNow}</span>;
}
it("advances server time with monotonic elapsed time instead of the browser clock", async () => {
  let elapsed = 0;
  vi.spyOn(performance, "now").mockImplementation(() => elapsed);
  render(<ClockProbe />);
  await flush();
  expect(screen.getByTestId("server-clock")).toHaveTextContent(
    String(Date.parse(snapshotFixture().serverTime)),
  );
  elapsed = 1000;
  await act(async () => {
    await vi.advanceTimersByTimeAsync(1000);
  });
  expect(screen.getByTestId("server-clock")).toHaveTextContent(
    String(Date.parse(snapshotFixture().serverTime) + 1000),
  );
});
const get = vi.mocked(getMonitoring);
function Probe({ unauthorized = () => {} }: { unauthorized?: () => void }) {
  const result = useMonitoring(unauthorized);
  return (
    <div>
      <span>{result.state?.host.name ?? "Loading"}</span>
      <span>{result.stale ? "Stale" : "Fresh"}</span>
      <span>{result.error}</span>
      <button onClick={result.retry}>Retry</button>
    </div>
  );
}
beforeEach(() => {
  vi.useFakeTimers();
  vi.spyOn(performance, "now").mockImplementation(() => Date.now());
  vi.spyOn(document, "hidden", "get").mockReturnValue(false);
  get.mockReset();
  get.mockResolvedValue(snapshotFixture());
});
afterEach(() => {
  cleanup();
  vi.useRealTimers();
  vi.restoreAllMocks();
});
const flush = async () => {
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
  });
};
it("pauses requests while hidden and catches up on return", async () => {
  render(<Probe />);
  await flush();
  expect(get).toHaveBeenCalledTimes(1);
  vi.spyOn(document, "hidden", "get").mockReturnValue(true);
  await act(async () => {
    document.dispatchEvent(new Event("visibilitychange"));
    await vi.advanceTimersByTimeAsync(10000);
  });
  expect(get).toHaveBeenCalledTimes(1);
  vi.spyOn(document, "hidden", "get").mockReturnValue(false);
  await act(async () => {
    document.dispatchEvent(new Event("visibilitychange"));
  });
  await flush();
  expect(get).toHaveBeenCalledTimes(2);
  expect(get.mock.calls[1][0]).toBe("cursor-a");
});
it("keeps only one request in flight and times it out after five seconds", async () => {
  get.mockImplementation(
    (_cursor, signal) =>
      new Promise((_resolve, reject) =>
        signal.addEventListener("abort", () =>
          reject(new DOMException("Aborted", "AbortError")),
        ),
      ),
  );
  const unauthorized = vi.fn();
  render(<Probe unauthorized={unauthorized} />);
  await flush();
  await act(async () => {
    await vi.advanceTimersByTimeAsync(4000);
  });
  expect(get).toHaveBeenCalledTimes(1);
  await act(async () => {
    await vi.advanceTimersByTimeAsync(1000);
  });
  expect(screen.getByText(/timed out/i)).toBeVisible();
  expect(unauthorized).not.toHaveBeenCalled();
});
it("ignores a response after sign-out or unmount", async () => {
  let resolve!: (value: ReturnType<typeof snapshotFixture>) => void;
  get.mockReturnValue(
    new Promise((done) => {
      resolve = done;
    }),
  );
  const unauthorized = vi.fn();
  const view = render(<Probe unauthorized={unauthorized} />);
  await flush();
  const signal = get.mock.calls[0][1];
  view.unmount();
  resolve(snapshotFixture());
  await flush();
  expect(signal.aborted).toBe(true);
  expect(unauthorized).not.toHaveBeenCalled();
  expect(screen.queryByText("porty-host")).not.toBeInTheDocument();
});
it("marks retained readings stale while a request hangs", async () => {
  render(<Probe />);
  await flush();
  get.mockImplementation(() => new Promise(() => {}));
  await act(async () => {
    await vi.advanceTimersByTimeAsync(6000);
  });
  expect(screen.getByText("Stale")).toBeVisible();
  expect(screen.getByText("porty-host")).toBeVisible();
});
it("clears monitoring data when authentication expires", async () => {
  const unauthorized = vi.fn();
  render(<Probe unauthorized={unauthorized} />);
  await flush();
  get.mockRejectedValue(new APIError(401, "AuthenticationFailed", "Expired"));
  await act(async () => {
    await vi.advanceTimersByTimeAsync(2000);
  });
  expect(unauthorized).toHaveBeenCalledTimes(1);
  expect(screen.queryByText("porty-host")).not.toBeInTheDocument();
});
