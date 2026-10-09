import {
  render,
  screen,
  fireEvent,
  within,
  waitFor,
} from "@testing-library/preact";
import { it, expect, vi } from "vitest";
import { StackSettings } from "./StackSettings";
import { deleteStack, renameStack } from "./api";
import type { Stack } from "./types";
vi.mock("./AutoUpdateSettings", () => ({ AutoUpdateSettings: () => null }));
vi.mock("./OnDemandSettings", () => ({ OnDemandSettings: () => null }));
vi.mock("./api", () => ({
  stackPath: (id: string) => `/stacks/${id}`,
  listEnvironmentKeys: vi.fn().mockResolvedValue([]),
  setEnvironmentValue: vi.fn(),
  renameStack: vi.fn(),
  deleteStack: vi.fn(),
}));
const stack = {
  id: "s",
  directoryName: "monitoring",
  composeProjectName: "porty-monitoring",
} as Stack;
it("names removed files and retained volumes before deletion", async () => {
  const onChanged = vi.fn();
  vi.mocked(deleteStack).mockRejectedValueOnce(new Error("Stop failed"));
  render(<StackSettings stack={stack} onChanged={onChanged} />);
  fireEvent.click(screen.getByRole("button", { name: "Delete stack" }));
  const dialog = screen.getByRole("dialog", { name: "Delete monitoring?" });
  expect(dialog).toHaveTextContent("monitoring/");
  expect(dialog).toHaveTextContent(/volumes.*retained/i);
  expect(deleteStack).not.toHaveBeenCalled();
  fireEvent.click(within(dialog).getByRole("button", { name: "Delete stack" }));
  expect(await within(dialog).findByRole("alert")).toHaveTextContent(
    "Stop failed",
  );
  expect(onChanged).not.toHaveBeenCalled();
});
it("retains the requested stack name after a rejected save", async () => {
  vi.mocked(renameStack).mockRejectedValueOnce(
    new Error("Name already exists"),
  );
  render(<StackSettings stack={stack} onChanged={vi.fn()} />);
  fireEvent.input(screen.getByLabelText("Stack name"), {
    target: { value: "new-name" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Rename stack" }));
  await waitFor(() =>
    expect(screen.getByRole("alert")).toHaveTextContent("Name already exists"),
  );
  expect(screen.getByLabelText("Stack name")).toHaveValue("new-name");
});
