import { useEffect, useLayoutEffect, useRef, useState } from "preact/hooks";
import { message } from "../../lib/http";
import {
  listFiles,
  getStackFile,
  getDiff,
  saveStackFile,
  deleteStackFile,
  moveStackFile,
  createStackFile,
  commitStack,
} from "./api";
import type { FileContent, FileEntry } from "./types";

type EditorState = {
  tree: FileEntry[];
  file?: FileContent;
  content: string;
  diff: string;
  diffStatus: "loading" | "ready" | "error";
  diffError: string;
  dirty: boolean;
  busy: boolean;
  error: string;
  notice: string;
  commitMessage: string;
  editorKey: number;
};
type Options = {
  stackId: string;
  enabled: boolean;
  onDirtyChange: (dirty: boolean) => void;
  onSaved: () => void;
};
const initial: EditorState = {
  tree: [],
  file: undefined,
  content: "",
  diff: "",
  diffStatus: "loading",
  diffError: "",
  dirty: false,
  busy: false,
  error: "",
  notice: "",
  commitMessage: "",
  editorKey: 0,
};
export type StackEditor = ReturnType<typeof useStackEditor>;

export function useStackEditor(options: Options) {
  const { stackId, enabled } = options;
  const callbacks = useRef(options);
  callbacks.current = options;
  const [state, setState] = useState<EditorState>(initial);
  const current = useRef(state);
  const generation = useRef(0);
  const loaded = useRef(false);
  function update(patch: Partial<EditorState>) {
    const next = { ...current.current, ...patch };
    const changed = next.dirty !== current.current.dirty;
    current.current = next;
    setState(next);
    if (changed) callbacks.current.onDirtyChange(next.dirty);
  }
  useLayoutEffect(() => {
    generation.current++;
    loaded.current = false;
    update({ ...initial, editorKey: current.current.editorKey + 1 });
    return () => {
      generation.current++;
    };
  }, [stackId]);
  useEffect(() => {
    if (!enabled || loaded.current) return;
    loaded.current = true;
    const version = generation.current;
    update({ busy: true, error: "" });
    Promise.allSettled([
      listFiles(stackId),
      getStackFile(stackId, "docker-compose.yml"),
      getDiff(stackId),
    ]).then(([tree, file, diff]) => {
      if (version !== generation.current) return;
      update({
        busy: false,
        tree: tree.status === "fulfilled" ? tree.value || [] : [],
        file: file.status === "fulfilled" ? file.value : undefined,
        content: file.status === "fulfilled" ? file.value.content : "",
        diff: diff.status === "fulfilled" ? diff.value : "",
        diffStatus: diff.status === "fulfilled" ? "ready" : "error",
        diffError: diff.status === "rejected" ? message(diff.reason) : "",
        error: [tree, file]
          .filter((value) => value.status === "rejected")
          .map((value) => message((value as PromiseRejectedResult).reason))
          .join(" · "),
        notice: "",
        editorKey: current.current.editorKey + 1,
      });
    });
  }, [stackId, enabled]);
  function setContent(content: string) {
    if (!current.current.busy)
      update({ content, dirty: content !== current.current.file?.content });
  }
  function setCommitMessage(commitMessage: string) {
    update({ commitMessage });
  }
  function discard() {
    if (current.current.busy) return;
    update({
      content: current.current.file?.content || "",
      dirty: false,
      error: "",
      editorKey: current.current.editorKey + 1,
    });
  }
  async function open(path: string): Promise<boolean> {
    if (current.current.busy || current.current.dirty) return false;
    const version = generation.current;
    update({ busy: true, error: "" });
    try {
      const file = await getStackFile(stackId, path);
      if (version !== generation.current) return false;
      update({
        file,
        content: file.content,
        dirty: false,
        editorKey: current.current.editorKey + 1,
      });
      return true;
    } catch (error) {
      if (version === generation.current) update({ error: message(error) });
      return false;
    } finally {
      if (version === generation.current) update({ busy: false });
    }
  }
  async function refreshDiff(version: number, success: string) {
    update({ diffStatus: "loading", diffError: "" });
    try {
      const diff = await getDiff(stackId);
      if (version === generation.current)
        update({ diff, diffStatus: "ready", diffError: "", notice: success });
    } catch (error) {
      if (version === generation.current)
        update({
          diffStatus: "error",
          diffError: message(error),
          notice: success
            ? `${success} Changes could not be refreshed: ${message(error)}`
            : "",
        });
    }
  }
  async function retryDiff() {
    if (current.current.busy) return;
    const version = generation.current;
    update({ busy: true });
    try {
      await refreshDiff(version, "");
    } finally {
      if (version === generation.current) update({ busy: false });
    }
  }
  async function save(): Promise<boolean> {
    const { file, content, busy } = current.current;
    if (!file || busy) return false;
    const version = generation.current;
    update({ busy: true, error: "", notice: "" });
    try {
      const result = await saveStackFile(
        stackId,
        file.path,
        content,
        file.hash,
      );
      if (version !== generation.current) return false;
      update({
        file: { ...file, content, hash: result.hash },
        dirty: current.current.content !== content,
        editorKey: current.current.editorKey + 1,
      });
      callbacks.current.onSaved();
      await refreshDiff(version, "Saved file. Deployment has not changed.");
      return version === generation.current;
    } catch (error) {
      if (version === generation.current) update({ error: message(error) });
      return false;
    } finally {
      if (version === generation.current) update({ busy: false });
    }
  }
  async function commit(): Promise<boolean> {
    const value = current.current;
    if (
      value.busy ||
      value.dirty ||
      value.diffStatus !== "ready" ||
      !value.commitMessage.trim() ||
      !value.diff
    )
      return false;
    const version = generation.current;
    update({ busy: true, error: "", notice: "" });
    try {
      await commitStack(stackId, value.commitMessage);
      if (version !== generation.current) return false;
      update({ commitMessage: "" });
      callbacks.current.onSaved();
      await refreshDiff(
        version,
        "Committed stack files. Deployment has not changed.",
      );
      return version === generation.current;
    } catch (error) {
      if (version === generation.current) update({ error: message(error) });
      return false;
    } finally {
      if (version === generation.current) update({ busy: false });
    }
  }
  async function mutateFile(
    kind: "create" | "directory" | "move" | "delete",
    path: string,
  ): Promise<boolean> {
    const value = current.current;
    if (
      value.busy ||
      value.dirty ||
      !path ||
      (kind === "move" && !value.file) ||
      (kind === "delete" && path === "docker-compose.yml")
    )
      return false;
    const version = generation.current;
    update({ busy: true, error: "", notice: "" });
    try {
      if (kind === "delete") await deleteStackFile(stackId, path);
      else if (kind === "move")
        await moveStackFile(stackId, value.file?.path, path);
      else await createStackFile(stackId, path, kind === "directory");
      if (version !== generation.current) return false;
      callbacks.current.onSaved();
      if (kind === "delete")
        update({ file: undefined, content: "", dirty: false });
      try {
        const tree = await listFiles(stackId);
        if (version !== generation.current) return false;
        update({ tree: tree || [] });
        if (kind !== "delete" && kind !== "directory") {
          const file = await getStackFile(stackId, path);
          if (version !== generation.current) return false;
          update({
            file,
            content: file.content,
            dirty: false,
            editorKey: current.current.editorKey + 1,
          });
        }
      } catch (error) {
        if (version === generation.current)
          update({
            error: `File change saved; reload failed: ${message(error)}`,
          });
      }
      await refreshDiff(
        version,
        "File change saved. Deployment has not changed.",
      );
      return version === generation.current;
    } catch (error) {
      if (version === generation.current) update({ error: message(error) });
      return false;
    } finally {
      if (version === generation.current) update({ busy: false });
    }
  }
  return {
    ...state,
    setContent,
    setCommitMessage,
    discard,
    open,
    save,
    commit,
    mutateFile,
    retryDiff,
  };
}
