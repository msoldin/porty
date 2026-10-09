import { fireEvent, render, screen, waitFor } from "@testing-library/preact";
import { useState } from "preact/hooks";
import { expect, it } from "vitest";
import { Dialog } from "./Dialog";

it("names a confirmation and returns focus after cancellation", async () => {
  function Fixture() {
    const [open, setOpen] = useState(false);
    return (
      <>
        <button onClick={() => setOpen(true)}>Review</button>
        <Dialog
          open={open}
          title="Stop monitoring?"
          onClose={() => setOpen(false)}
        >
          <button>Stop stack</button>
        </Dialog>
      </>
    );
  }
  render(<Fixture />);
  const trigger = screen.getByRole("button", { name: "Review" });
  trigger.focus();
  fireEvent.click(trigger);
  const dialog = await screen.findByRole("dialog", {
    name: "Stop monitoring?",
  });
  fireEvent(dialog, new Event("cancel", { cancelable: true }));
  await waitFor(() =>
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument(),
  );
  expect(trigger).toHaveFocus();
});
