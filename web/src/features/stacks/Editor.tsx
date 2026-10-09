import { useMemo, useState } from "preact/hooks";
import type { Stack } from "./types";
import type { StackEditor } from "./useStackEditor";
import { CodeEditor } from "./CodeEditor";
import { Icon } from "../../components/Icon";
import { Empty, Notice } from "../../components/Feedback";
import { Dialog } from "../../components/Dialog";
import "./editor.css";

export function Editor({ stack, model }: { stack: Stack; model: StackEditor }) {
  const {
    tree,
    file,
    content,
    diff,
    dirty,
    busy,
    error,
    notice,
    commitMessage,
    setContent,
    setCommitMessage,
    save,
  } = model;
  // Preserve the buffer across tab visits without recreating CodeMirror on every keystroke.
  const initial = useMemo(() => content, [model.editorKey]);
  const [intent, setIntent] = useState<
    "open" | "create" | "directory" | "move" | "delete"
  >();
  const [path, setPath] = useState("");
  async function open(path: string) {
    if (dirty) {
      setPath(path);
      setIntent("open");
      return;
    }
    await model.open(path);
  }
  async function mutateFile(kind: "create" | "directory" | "move" | "delete") {
    setPath(kind === "move" || kind === "delete" ? file?.path || "" : "");
    setIntent(kind);
  }
  const intentLabel =
    intent === "open"
      ? "Open file"
      : intent === "directory"
        ? "Create directory"
        : intent === "move"
          ? "Move file"
          : intent === "delete"
            ? "Delete file"
            : "Create file";
  async function applyIntent() {
    if (!intent || !path || busy) return;
    if (dirty) model.discard();
    const success =
      intent === "open"
        ? await model.open(path)
        : await model.mutateFile(intent, path);
    if (success) setIntent(undefined);
  }
  return (
    <>
      <Dialog
        open={!!intent}
        title={intentLabel}
        onClose={() => {
          if (!busy) setIntent(undefined);
        }}
      >
        <form
          onSubmit={(event) => {
            event.preventDefault();
            void applyIntent();
          }}
        >
          {intent === "open" || intent === "delete" ? (
            <p>
              {intentLabel}: <strong>{path}</strong>
            </p>
          ) : (
            <label>
              Relative path
              <input
                aria-label="Relative path"
                value={path}
                required
                onInput={(event) => setPath(event.currentTarget.value)}
                disabled={busy}
              />
            </label>
          )}
          {dirty && (
            <Notice>
              Your unsaved edits will be discarded if you continue.
            </Notice>
          )}
          {intent === "delete" && (
            <p>
              This removes the file from the working tree. Existing Git history
              is retained.
            </p>
          )}
          {error && <Notice>{error}</Notice>}
          <div class="dialog-actions">
            <button
              type="button"
              disabled={busy}
              onClick={() => setIntent(undefined)}
            >
              Cancel
            </button>
            <button
              class={intent === "delete" ? "danger" : "primary"}
              disabled={busy || !path}
            >
              {dirty
                ? `Discard edits and ${intentLabel.toLowerCase()}`
                : intentLabel}
            </button>
          </div>
        </form>
      </Dialog>
      {error && !intent && (
        <Notice>
          {error}{" "}
          {file && (
            <button disabled={busy} onClick={() => open(file.path)}>
              Reload file
            </button>
          )}
        </Notice>
      )}
      {notice && (
        <Notice tone="neutral" role="status">
          {notice}
        </Notice>
      )}
      <div class="editor-layout">
        <aside class="file-tree" aria-label="Files">
          <div class="pane-title">
            Files
            <div class="compact-actions">
              <button
                class="icon-button"
                aria-label="New file"
                disabled={busy}
                onClick={() => mutateFile("create")}
              >
                <Icon name="Plus" />
              </button>
              <button
                class="icon-button"
                aria-label="New directory"
                disabled={busy}
                onClick={() => mutateFile("directory")}
              >
                <Icon name="Folder" />
              </button>
            </div>
          </div>
          <div class="tree-root">
            <Icon name="Folder" />
            {stack.directoryName}
          </div>
          {tree.map((entry) => (
            <button
              key={entry.path}
              class={`file-row ${file?.path === entry.path ? "selected" : ""}`}
              style={{
                paddingLeft: `${18 + entry.path.split("/").length * 10}px`,
              }}
              disabled={busy || entry.isDirectory || !entry.editable}
              onClick={() => open(entry.path)}
              title={entry.path}
            >
              <Icon name={entry.isDirectory ? "Folder" : "File"} />
              {entry.path.split("/").at(-1)}
            </button>
          ))}
        </aside>
        <section class="editor-pane" aria-label="File editor">
          <div class="pane-title">
            <span>
              <Icon name="File" />
              {file?.path || "Select a file"}
            </span>
            <button
              class="small primary"
              disabled={!file || !dirty || busy}
              onClick={save}
            >
              Save file
            </button>
          </div>
          {file ? (
            <CodeEditor
              key={model.editorKey}
              initial={initial}
              readOnly={busy}
              onChange={setContent}
            />
          ) : (
            <Empty>Select an editable file.</Empty>
          )}
          <footer class="editor-status">
            <span>
              <Icon name={dirty ? "File" : "Check"} />
              {dirty ? "Unsaved changes" : "Saved"}
            </span>
            <span>YAML · UTF-8</span>
            <button disabled={!file || busy} onClick={() => mutateFile("move")}>
              Move
            </button>
            <button
              disabled={!file || busy || file.path === "docker-compose.yml"}
              onClick={() => mutateFile("delete")}
            >
              Delete
            </button>
          </footer>
        </section>
        <section class="diff-pane" aria-label="Stack changes">
          <div class="pane-title">
            Changes <span class="muted">Uncommitted changes</span>
          </div>
          <div class="diff-output">
            {model.diffStatus === "loading" ? (
              <p class="diff-message" role="status">
                Loading changes…
              </p>
            ) : model.diffStatus === "error" ? (
              <Notice>
                Changes could not be loaded: {model.diffError}
                <button disabled={busy} onClick={() => void model.retryDiff()}>
                  Retry changes
                </button>
              </Notice>
            ) : diff ? (
              diff.split("\n").map((line, i) => (
                <div
                  key={i}
                  class={
                    line.startsWith("+")
                      ? "addition"
                      : line.startsWith("-")
                        ? "deletion"
                        : line.startsWith("@@")
                          ? "hunk"
                          : ""
                  }
                >
                  {line || " "}
                </div>
              ))
            ) : (
              <Empty>No uncommitted changes.</Empty>
            )}
          </div>
          <form
            class="commit-form"
            onSubmit={(event) => {
              event.preventDefault();
              void model.commit();
            }}
          >
            <label htmlFor="commit-message">Commit changes to repository</label>
            <p class="muted">
              Only files in {stack.directoryName}/ will be committed.
            </p>
            <textarea
              id="commit-message"
              aria-label="Commit message"
              placeholder="Describe your changes"
              maxLength={200}
              value={commitMessage}
              onInput={(e) => setCommitMessage(e.currentTarget.value)}
              required
            />
            <div class="commit-footer">
              <span>Save writes files. Commit records their Git history.</span>
              <button
                disabled={
                  busy ||
                  dirty ||
                  model.diffStatus !== "ready" ||
                  !commitMessage.trim() ||
                  !diff
                }
              >
                <Icon name="Repository" />
                Commit stack
              </button>
            </div>
          </form>
        </section>
      </div>
    </>
  );
}
