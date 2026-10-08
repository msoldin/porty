import {
  render,
  screen,
  fireEvent,
  waitFor,
  act,
} from "@testing-library/preact";
import { expect, it, vi } from "vitest";
import { useOnDemand } from "./useOnDemand";
import { defaultOnDemandPolicy } from "./onDemandTypes";
import { listOnDemand, saveOnDemand } from "./onDemandApi";
import { APIError } from "../../lib/http";
vi.mock("./onDemandApi", () => ({
  listOnDemand: vi.fn(),
  saveOnDemand: vi.fn(),
  changeOnDemand: vi.fn(),
}));
function Fixture() {
  const state = useOnDemand("s");
  return (
    <>
      <span>{state.busy ? "Saving" : "Ready"}</span>
      {state.error && <p role="alert">{state.error}</p>}
      <button onClick={() => void state.reload()}>Reload</button>
      <button
        onClick={() =>
          void state.save("", { ...defaultOnDemandPolicy, expectedRevision: 0 })
        }
      >
        Save
      </button>
    </>
  );
}
it("does not let a concurrent refresh strand a mutation in busy state", async () => {
  vi.mocked(listOnDemand).mockResolvedValue([]);
  let resolve!: (value: never) => void;
  vi.mocked(saveOnDemand).mockReturnValue(
    new Promise((done) => {
      resolve = done;
    }),
  );
  render(<Fixture />);
  await waitFor(() => expect(listOnDemand).toHaveBeenCalledTimes(1));
  fireEvent.click(screen.getByRole("button", { name: "Save" }));
  fireEvent.click(screen.getByRole("button", { name: "Reload" }));
  await act(async () => resolve({} as never));
  await waitFor(() => expect(screen.getByText("Ready")).toBeInTheDocument());
});
it.each([
  ["OnDemandUnavailable", "Deploy the group before enabling on-demand."],
  ["OnDemandHostUnavailable", "Porty must share Docker's host network."],
  ["OperationInProgress", "A stack operation is already running."],
])("shows the server explanation for %s", async (code, explanation) => {
  vi.mocked(listOnDemand).mockResolvedValue([]);
  vi.mocked(saveOnDemand).mockRejectedValue(
    new APIError(409, code, explanation),
  );
  render(<Fixture />);
  fireEvent.click(screen.getByRole("button", { name: "Save" }));
  await waitFor(() =>
    expect(screen.getByRole("alert")).toHaveTextContent(explanation),
  );
  expect(screen.getByText("Ready")).toBeInTheDocument();
});
it("asks for review after a genuine revision conflict", async () => {
  vi.mocked(listOnDemand).mockResolvedValue([]);
  vi.mocked(saveOnDemand).mockRejectedValue(
    new APIError(409, "OnDemandConflict", "Stale revision"),
  );
  render(<Fixture />);
  fireEvent.click(screen.getByRole("button", { name: "Save" }));
  await waitFor(() =>
    expect(screen.getByRole("alert")).toHaveTextContent(
      "Review the refreshed status before retrying.",
    ),
  );
});
