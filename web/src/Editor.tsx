import { useEffect, useState } from "preact/hooks";
import {
  api,
  message,
  stackPath,
  type Stack,
  type FileEntry,
  type FileContent,
} from "./api";
import { CodeEditor } from "./CodeEditor";
import { Empty, Icon, Notice } from "./ui";

export function Editor({
  stack,
  dirty,
  setDirty,
  refresh,
}: {
  stack: Stack;
  dirty: boolean;
  setDirty: (dirty: boolean) => void;
  refresh: () => void;
}) {
  const root = stackPath(stack.id);
  const [tree, setTree] = useState<FileEntry[]>([]);
  const [file, setFile] = useState<FileContent>();
  const [content, setContent] = useState("");
  const [diff, setDiff] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [commitMessage, setCommitMessage] = useState("");
  const [notice, setNotice] = useState("");
  async function reloadDiff() {
    setDiff((await api<{ diff: string }>(`${root}/diff`)).diff);
  }
  async function open(path: string) {
    if (dirty && !confirm("Discard unsaved changes?")) return;
    setBusy(true);
    setError("");
    try {
      const next = await api<FileContent>(
        `${root}/files?path=${encodeURIComponent(path)}`,
      );
      setFile(next);
      setContent(next.content);
      setDirty(false);
    } catch (error) {
      setError(message(error));
    } finally {
      setBusy(false);
    }
  }
  async function reloadTree() {
    setTree((await api<FileEntry[] | null>(`${root}/tree`)) || []);
  }
  useEffect(() => {
    let active = true;
    Promise.all([
      api<FileEntry[] | null>(`${root}/tree`),
      api<FileContent>(`${root}/files?path=docker-compose.yml`),
      api<{ diff: string }>(`${root}/diff`),
    ])
      .then(([entries, file, difference]) => {
        if (!active) return;
        setTree(entries || []);
        setFile(file);
        setContent(file.content);
        setDiff(difference.diff);
        setDirty(false);
      })
      .catch((error) => active && setError(message(error)));
    return () => {
      active = false;
    };
  }, [root]);
  async function save() {
    if (!file) return;
    setBusy(true);
    setError("");
    const snapshot = content;
    try {
      const result = await api<{ hash: string }>(
        `${root}/files?path=${encodeURIComponent(file.path)}`,
        "PUT",
        { content: snapshot },
        { "If-Match": `"${file.hash}"` },
      );
      setFile({ ...file, content: snapshot, hash: result.hash });
      setDirty(false);
      await reloadDiff();
      refresh();
    } catch (error) {
      setError(message(error));
    } finally {
      setBusy(false);
    }
  }
  async function mutateFile(kind: "create" | "directory" | "move" | "delete") {
    if (dirty && !confirm("Discard unsaved changes?")) return;
    const path =
      kind === "delete"
        ? file?.path
        : prompt(kind === "move" ? "New relative path" : "Relative path");
    if (!path) return;
    if (kind === "delete" && !confirm(`Delete ${path}?`)) return;
    setBusy(true);
    setError("");
    try {
      if (kind === "delete")
        await api(`${root}/files?path=${encodeURIComponent(path)}`, "DELETE");
      else if (kind === "move")
        await api(`${root}/files/move`, "POST", { from: file?.path, to: path });
      else
        await api(`${root}/files`, "POST", {
          path,
          content: "",
          directory: kind === "directory",
        });
      setDirty(false);
      await reloadTree();
      await reloadDiff();
      refresh();
      if (kind === "delete") setFile(undefined);
      else if (kind !== "directory") {
        const next = await api<FileContent>(
          `${root}/files?path=${encodeURIComponent(path)}`,
        );
        setFile(next);
        setContent(next.content);
      }
    } catch (error) {
      setError(message(error));
    } finally {
      setBusy(false);
    }
  }
  return (
    <>
      {error && (
        <Notice>
          {error}{" "}
          {file && (
            <button disabled={busy} onClick={() => open(file.path)}>
              Reload file
            </button>
          )}
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
              disabled={!dirty || busy}
              onClick={save}
            >
              Save file
            </button>
          </div>
          {file ? (
            <CodeEditor
              key={`${file.path}:${file.hash}`}
              initial={file.content}
              readOnly={busy}
              onChange={(value) => {
                setContent(value);
                setDirty(value !== file.content);
              }}
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
            {diff ? (
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
            onSubmit={async (event) => {
              event.preventDefault();
              setBusy(true);
              setError("");
              setNotice("");
              try {
                await api(`${root}/commit`, "POST", { message: commitMessage });
                setCommitMessage("");
                setNotice(`Committed ${stack.directoryName}`);
                await reloadDiff();
                refresh();
              } catch (error) {
                setError(message(error));
              } finally {
                setBusy(false);
              }
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
              <span role="status">{notice}</span>
              <button
                disabled={busy || dirty || !commitMessage.trim() || !diff}
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
