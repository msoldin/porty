import { fireEvent, render, screen } from "@testing-library/preact";
import { useState } from "preact/hooks";
import { expect, it, vi } from "vitest";
import { Editor } from "./Editor";
import { useStackEditor } from "./useStackEditor";
import type { Stack } from "./types";
vi.mock("./api", () => ({
  listFiles: vi.fn().mockResolvedValue([]),
  getStackFile: vi
    .fn()
    .mockResolvedValue({
      path: "docker-compose.yml",
      content: "original",
      hash: "hash",
      size: 8,
    }),
  getDiff: vi.fn().mockResolvedValue(""),
  saveStackFile: vi.fn(),
  deleteStackFile: vi.fn(),
  moveStackFile: vi.fn(),
  createStackFile: vi.fn(),
  commitStack: vi.fn(),
}));
vi.mock("./CodeEditor", () => ({
  CodeEditor: ({
    initial,
    onChange,
  }: {
    initial: string;
    onChange: (value: string) => void;
  }) => {
    const [value, setValue] = useState(initial);
    return (
      <textarea
        aria-label="File contents"
        value={value}
        onInput={(event) => {
          setValue(event.currentTarget.value);
          onChange(event.currentTarget.value);
        }}
      />
    );
  },
}));
it("preserves an unsaved buffer across tab visits and restores saved content only on explicit discard", async () => {
  function Fixture() {
    const [visible, setVisible] = useState(true);
    const model = useStackEditor({
      stackId: "one",
      enabled: visible,
      onDirtyChange: () => {},
      onSaved: () => {},
    });
    return (
      <>
        <button onClick={() => setVisible(!visible)}>Switch tab</button>
        <button onClick={model.discard}>Discard edits</button>
        {visible && (
          <Editor
            stack={{ id: "one", directoryName: "monitoring" } as Stack}
            model={model}
          />
        )}
      </>
    );
  }
  render(<Fixture />);
  const editor = await screen.findByRole("textbox", { name: "File contents" });
  fireEvent.input(editor, { target: { value: "unsaved buffer" } });
  fireEvent.click(screen.getByRole("button", { name: "Switch tab" }));
  fireEvent.click(screen.getByRole("button", { name: "Switch tab" }));
  expect(screen.getByRole("textbox", { name: "File contents" })).toHaveValue(
    "unsaved buffer",
  );
  fireEvent.click(screen.getByRole("button", { name: "Discard edits" }));
  expect(screen.getByRole("textbox", { name: "File contents" })).toHaveValue(
    "original",
  );
  expect(screen.getByRole("button", { name: "Save file" })).toBeDisabled();
});
