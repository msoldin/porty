import type { Stack, StackState } from "./types";
import type { Operation } from "../operations/types";
import { useEffect, useRef, useState } from "preact/hooks";
import { message } from "../../lib/http";
import { runStackAction } from "./api";
import {
  getDeploymentReview,
  deployReviewedStack,
} from "./deploymentReviewApi";
type Options = {
  stacks: Stack[];
  states: Record<string, StackState | undefined>;
  operations: Operation[];
  onAccepted: (operations: Operation[]) => void;
};
type Review = {
  kind: string;
  targets: { id: string; name: string; revision?: string }[];
  selection: string;
};
export function useStackBatchActions(options: Options) {
  const latest = useRef(options);
  latest.current = options;
  const [selectedIds, setSelectedIds] = useState<Set<string>>(new Set());
  const selection = useRef(selectedIds);
  selection.current = selectedIds;
  const [review, setReview] = useState<Review>();
  const captured = useRef<Review>();
  const [pending, setPending] = useState(false);
  const busy = useRef(false);
  const generation = useRef(0);
  const reads = useRef<Promise<void> | undefined>();
  const [error, setError] = useState("");
  const [outcomes, setOutcomes] = useState<string[]>([]);
  useEffect(
    () => () => {
      generation.current++;
    },
    [],
  );
  function eligible(ids: string[], kind: string) {
    const { stacks, states, operations } = latest.current;
    return (
      ids.length > 0 &&
      ids.length <= 20 &&
      ids.every((id) => {
        const stack = stacks.find((s) => s.id === id);
        return (
          stack &&
          !stack.archivedAt &&
          !operations.some(
            (op) =>
              op.scopeId === id && ["queued", "running"].includes(op.status),
          ) &&
          (kind === "deploy" ||
            (states[id]?.runtime === "running" && states[id]?.hasDeployed))
        );
      })
    );
  }
  function cancel() {
    if (busy.current && captured.current) return;
    generation.current++;
    captured.current = undefined;
    setReview(undefined);
    busy.current = false;
    setPending(false);
    setError("");
  }
  async function request(kind: string) {
    if (busy.current || !["deploy", "restart", "stop"].includes(kind)) return;
    const ids = [...selection.current].sort();
    if (!eligible(ids, kind)) return;
    const version = ++generation.current;
    busy.current = true;
    setPending(true);
    setError("");
    captured.current = undefined;
    const next: Review = {
      kind,
      selection: JSON.stringify(ids),
      targets: ids.map((id) => ({
        id,
        name: latest.current.stacks.find((s) => s.id === id)!.directoryName,
      })),
    };
    setReview(next);
    try {
      if (kind === "deploy") {
        const previous = reads.current;
        const task = (async () => {
          await previous;
          if (version !== generation.current) return;
          let index = 0;
          const failures: string[] = [];
          await Promise.all(
            Array.from({ length: Math.min(4, ids.length) }, async () => {
              while (
                index < next.targets.length &&
                version === generation.current
              ) {
                const target = next.targets[index++];
                try {
                  const value = await getDeploymentReview(target.id);
                  if (value.stackId !== target.id)
                    throw Error("Review does not match stack");
                  target.revision = value.sourceRevision;
                } catch (cause) {
                  failures.push(`${target.name}: ${message(cause)}`);
                }
              }
            }),
          );
          if (failures.length) throw Error(failures.join(" · "));
        })();
        // A cancelled review may still have HTTP reads in flight. Serialize the next cycle.
        reads.current = task.catch(() => {});
        await task;
      }
      if (version === generation.current) {
        captured.current = next;
        setReview({ ...next });
      }
    } catch (cause) {
      if (version === generation.current) setError(message(cause));
    } finally {
      if (version === generation.current) {
        busy.current = false;
        setPending(false);
      }
    }
  }
  async function confirm() {
    const value = captured.current;
    if (!value || busy.current) return;
    const ids = value.targets.map((t) => t.id);
    if (
      JSON.stringify([...selection.current].sort()) !== value.selection ||
      value.targets.some(
        (target) =>
          latest.current.stacks.find((stack) => stack.id === target.id)
            ?.directoryName !== target.name,
      ) ||
      !eligible(ids, value.kind)
    ) {
      setError(
        "Selection or stack availability changed. Cancel and review again.",
      );
      return;
    }
    busy.current = true;
    setPending(true);
    const version = generation.current;
    const results = await Promise.allSettled(
      value.targets.map((target) =>
        value.kind === "deploy"
          ? deployReviewedStack(target.id, target.revision!)
          : runStackAction(target.id, value.kind),
      ),
    );
    if (version !== generation.current) return;
    const accepted: Operation[] = [];
    const completed = new Set<string>();
    setOutcomes(
      results.map((result, index) => {
        const target = value.targets[index];
        if (result.status === "fulfilled") {
          accepted.push(result.value);
          completed.add(target.id);
          return `${target.name}: accepted`;
        }
        return `${target.name}: ${message(result.reason)}`;
      }),
    );
    setSelectedIds(
      (current) => new Set([...current].filter((id) => !completed.has(id))),
    );
    if (accepted.length) latest.current.onAccepted(accepted);
    captured.current = undefined;
    setReview(undefined);
    busy.current = false;
    setPending(false);
  }
  return {
    selectedIds,
    setSelectedIds,
    pending,
    submitting: pending && !!captured.current,
    error,
    outcomes,
    review,
    request,
    confirm,
    cancel,
  };
}
