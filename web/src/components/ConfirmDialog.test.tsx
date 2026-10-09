import { fireEvent, render, screen } from "@testing-library/preact";
import { expect, it, vi } from "vitest";
import { ConfirmDialog } from "./ConfirmDialog";

it("prevents duplicate confirmation while submitting and keeps errors visible", () => {
  const onConfirm = vi.fn();
  const props = {
    open: true,
    title: "Stop monitoring?",
    description: "All three containers will stop.",
    confirmLabel: "Stop stack",
    onConfirm,
    onCancel: vi.fn(),
  };
  const view = render(<ConfirmDialog {...props} />);
  fireEvent.click(screen.getByRole("button", { name: "Stop stack" }));
  view.rerender(<ConfirmDialog {...props} busy />);
  expect(screen.getByRole("button", { name: "Stop stack" })).toBeDisabled();
  fireEvent.click(screen.getByRole("button", { name: "Stop stack" }));
  expect(onConfirm).toHaveBeenCalledTimes(1);
  view.rerender(<ConfirmDialog {...props} error="Docker is unavailable" />);
  expect(screen.getByRole("alert")).toHaveTextContent("Docker is unavailable");
});
