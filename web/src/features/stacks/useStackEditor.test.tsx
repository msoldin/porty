import { act, renderHook, waitFor } from "@testing-library/preact";
import { beforeEach, expect, it, vi } from "vitest";
import { useStackEditor } from "./useStackEditor";
import {
  listFiles,
  getStackFile,
  getDiff,
  saveStackFile,
  commitStack,
  runStackAction,
} from "./api";
vi.mock("./api", () => ({
  listFiles: vi.fn(),
  getStackFile: vi.fn(),
  getDiff: vi.fn(),
  saveStackFile: vi.fn(),
  commitStack: vi.fn(),
  runStackAction: vi.fn(),
  deleteStackFile: vi.fn(),
  moveStackFile: vi.fn(),
  createStackFile: vi.fn(),
}));
const file = {
  path: "docker-compose.yml",
  content: "original",
  hash: "old-hash",
  size: 8,
};
beforeEach(() => {
  vi.resetAllMocks();
  vi.mocked(listFiles).mockResolvedValue([]);
  vi.mocked(getStackFile).mockResolvedValue(file);
  vi.mocked(getDiff).mockResolvedValue("diff");
});
function setup(enabled = true) {
  return renderHook(
    ({ stackId }) =>
      useStackEditor({
        stackId,
        enabled,
        onDirtyChange: vi.fn(),
        onSaved: vi.fn(),
      }),
    { initialProps: { stackId: "one" } },
  );
}
it("keeps the buffer and old hash after a stale-write rejection", async () => {
  const view = setup();
  await waitFor(() => expect(view.result.current.file).toEqual(file));
  act(() => view.result.current.setContent("edited"));
  vi.mocked(saveStackFile).mockRejectedValue(
    new Error("File changed; reload before saving"),
  );
  await act(async () => {
    expect(await view.result.current.save()).toBe(false);
  });
  expect(saveStackFile).toHaveBeenCalledWith(
    "one",
    file.path,
    "edited",
    "old-hash",
  );
  expect(view.result.current.content).toBe("edited");
  expect(view.result.current.file?.hash).toBe("old-hash");
  expect(view.result.current.dirty).toBe(true);
});
it("reports a successful save when refreshing the diff fails", async () => {
  const view = setup();
  await waitFor(() => expect(view.result.current.file).toEqual(file));
  act(() => view.result.current.setContent("saved"));
  vi.mocked(saveStackFile).mockResolvedValue({ hash: "new-hash" });
  vi.mocked(getDiff).mockRejectedValue(new Error("Offline"));
  await act(async () => {
    expect(await view.result.current.save()).toBe(true);
  });
  expect(view.result.current.file?.hash).toBe("new-hash");
  expect(view.result.current.dirty).toBe(false);
  expect(view.result.current.error).toBe("");
  expect(view.result.current.notice).toContain("Saved");
  expect(view.result.current.notice).toContain("Offline");
  expect(runStackAction).not.toHaveBeenCalled();
});
it("ignores a file response from the previous stack", async () => {
  let finish!: (value: typeof file) => void;
  vi.mocked(getStackFile).mockImplementationOnce(
    () =>
      new Promise((resolve) => {
        finish = resolve;
      }),
  );
  const view = setup();
  view.rerender({ stackId: "two" });
  await waitFor(() => expect(view.result.current.file).toEqual(file));
  await act(async () => finish({ ...file, content: "old stack" }));
  expect(view.result.current.content).toBe("original");
});
it("loads lazily, rejects an unloaded save, and commits saved files without deploying", async () => {
  const blank = setup(false);
  await act(async () => {
    expect(await blank.result.current.save()).toBe(false);
  });
  expect(getStackFile).not.toHaveBeenCalled();
  blank.unmount();
  const view = setup();
  await waitFor(() => expect(view.result.current.file).toEqual(file));
  act(() => view.result.current.setCommitMessage("Update configuration"));
  await act(async () => {
    expect(await view.result.current.commit()).toBe(true);
  });
  expect(commitStack).toHaveBeenCalledWith("one", "Update configuration");
  expect(runStackAction).not.toHaveBeenCalled();
});
