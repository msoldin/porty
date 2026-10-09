import {
  render,
  screen,
  fireEvent,
  within,
  waitFor,
} from "@testing-library/preact";
import { it, expect, vi } from "vitest";
import { EnvironmentRow } from "./EnvironmentRow";
import { deleteEnvironmentValue } from "./api";
vi.mock("./api", () => ({
  getEnvironmentValue: vi
    .fn()
    .mockResolvedValue({ value: "private-value", secret: true }),
  setEnvironmentValue: vi.fn(),
  deleteEnvironmentValue: vi.fn(),
}));
it("keeps secret values masked until deliberately revealed", async () => {
  render(<EnvironmentRow stackId="s" name="API_TOKEN" reload={vi.fn()} />);
  const input = screen.getByLabelText("Value for API_TOKEN");
  await waitFor(() => expect(input).toHaveValue("private-value"));
  expect(input).toHaveAttribute("type", "password");
  expect(screen.queryByText("private-value")).not.toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "Show API_TOKEN" }));
  expect(input).toHaveAttribute("type", "text");
  fireEvent.click(screen.getByRole("button", { name: "Hide API_TOKEN" }));
  expect(input).toHaveAttribute("type", "password");
  fireEvent.click(screen.getByRole("button", { name: "Delete API_TOKEN" }));
  const dialog = screen.getByRole("dialog", { name: "Delete API_TOKEN?" });
  expect(deleteEnvironmentValue).not.toHaveBeenCalled();
  expect(dialog).not.toHaveTextContent("private-value");
  fireEvent.click(within(dialog).getByRole("button", { name: "Cancel" }));
  expect(input).toHaveValue("private-value");
});
