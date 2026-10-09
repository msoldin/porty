import { fireEvent, render, screen, waitFor } from "@testing-library/preact";
import { afterEach, expect, it, vi } from "vitest";
import { DiskMountPath } from "./DiskMountPath";

afterEach(() => vi.unstubAllGlobals());
it("copies the complete path and confirms success", async () => {
  const writeText = vi.fn().mockResolvedValue(undefined);
  vi.stubGlobal("navigator", { clipboard: { writeText } });
  const path = "/mnt/wsl/" + "long-path/".repeat(30);
  render(<DiskMountPath path={path} index={1} />);
  fireEvent.click(screen.getByRole("button", { name: "Copy mount path 1" }));
  await waitFor(() =>
    expect(screen.getByRole("status")).toHaveTextContent("Path copied."),
  );
  expect(writeText).toHaveBeenCalledWith(path);
});
it.each([
  undefined,
  { writeText: vi.fn().mockRejectedValue(new Error("Denied")) },
])(
  "selects the full path when clipboard access is unavailable",
  async (clipboard) => {
    vi.stubGlobal("navigator", { clipboard });
    const path = "/mnt/wsl/shared/disk";
    render(<DiskMountPath path={path} index={2} />);
    fireEvent.click(screen.getByRole("button", { name: "Copy mount path 2" }));
    await waitFor(() =>
      expect(screen.getByRole("status")).toHaveTextContent("Path selected."),
    );
    const field = screen.getByRole("textbox", {
      name: "Mount path 2",
    }) as HTMLInputElement;
    expect(field).toHaveFocus();
    expect(field.selectionStart).toBe(0);
    expect(field.selectionEnd).toBe(path.length);
  },
);
