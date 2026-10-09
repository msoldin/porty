import type { Stack } from "./types";
import type { Operation } from "../operations/types";
import type { StackEditor } from "./useStackEditor";
import { useLayoutEffect, useRef, useState } from "preact/hooks";
import { APIError, message } from "../../lib/http";
import {
  getDeploymentReview,
  deployReviewedStack,
  type DeploymentReview,
} from "./deploymentReviewApi";
export type ReviewOptions = {
  stack: Stack;
  operations: Operation[];
  editor: StackEditor;
  onAccepted: (operation: Operation) => void;
};
type Phase =
  "idle" | "unsaved" | "loading" | "reviewing" | "submitting" | "error";
type State = { phase: Phase; review?: DeploymentReview; error: string };
export function useDeploymentReview(options: ReviewOptions) {
  const latest = useRef(options);
  latest.current = options;
  const [state, setState] = useState<State>({ phase: "idle", error: "" });
  const current = useRef(state);
  const generation = useRef(0);
  function update(next: State) {
    current.current = next;
    setState(next);
  }
  useLayoutEffect(() => {
    generation.current++;
    update({ phase: "idle", error: "" });
    return () => {
      generation.current++;
    };
  }, [options.stack.id]);
  function eligible() {
    const { stack, operations } = latest.current;
    return (
      !stack.archivedAt &&
      !operations.some(
        (op) =>
          op.scopeId === stack.id && ["queued", "running"].includes(op.status),
      )
    );
  }
  function cancel() {
    if (current.current.phase === "submitting") return;
    generation.current++;
    update({ phase: "idle", error: "" });
  }
  async function load(version: number, error = "") {
    if (!eligible()) {
      update({
        phase: "error",
        error:
          "This stack is archived or has an active operation. Review again when it is available.",
      });
      return;
    }
    const id = latest.current.stack.id;
    update({ phase: "loading", error });
    try {
      const review = await getDeploymentReview(id);
      if (version !== generation.current) return;
      if (review.stackId !== id)
        throw new Error("Review does not match this stack");
      update({ phase: "reviewing", review, error });
    } catch (cause) {
      if (version === generation.current)
        update({ phase: "error", error: message(cause) });
    }
  }
  async function request() {
    if (["submitting", "loading"].includes(current.current.phase)) return;
    const version = ++generation.current;
    if (latest.current.editor.dirty) {
      update({ phase: "unsaved", error: "" });
      return;
    }
    await load(version);
  }
  async function saveAndContinue() {
    if (current.current.phase !== "unsaved") return;
    const version = generation.current;
    update({ phase: "loading", error: "" });
    const saved = await latest.current.editor.save();
    if (version !== generation.current) return;
    if (!saved) {
      update({
        phase: "unsaved",
        error:
          "File was not saved. Resolve the editor error before continuing.",
      });
      return;
    }
    await load(version);
  }
  async function discardAndContinue() {
    if (current.current.phase !== "unsaved") return;
    latest.current.editor.discard();
    await load(generation.current);
  }
  async function submit() {
    const { phase, review } = current.current;
    if (phase !== "reviewing" || !review) return;
    if (!eligible()) {
      update({
        phase: "error",
        error: "Stack availability changed. Review again before deploying.",
      });
      return;
    }
    const version = generation.current;
    update({ phase: "submitting", review, error: "" });
    try {
      const operation = await deployReviewedStack(
        review.stackId,
        review.sourceRevision,
      );
      if (version !== generation.current) return;
      update({ phase: "idle", error: "" });
      latest.current.onAccepted(operation);
    } catch (cause) {
      if (version !== generation.current) return;
      if (cause instanceof APIError && cause.status === 412) {
        await load(
          version,
          "Saved configuration changed. Review the latest version and confirm again.",
        );
      } else {
        update({
          phase: "error",
          error: `${message(cause)} Check Operations before trying again; the request may have been accepted.`,
        });
      }
    }
  }
  return {
    ...state,
    request,
    saveAndContinue,
    discardAndContinue,
    cancel,
    submit,
  };
}
