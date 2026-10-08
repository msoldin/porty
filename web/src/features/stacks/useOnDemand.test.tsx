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
