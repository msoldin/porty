import type { Container, ContainerAction } from "./types";
import type { Operation } from "../operations/types";
import { useEffect, useRef, useState } from "preact/hooks";
import { runContainerBatchAction } from "./api";
import { message } from "../../lib/http";
export type ContainerTarget = {
  stackId: string;
  stackName: string;
  container: Container;
  archived: boolean;
  busy: boolean;
};
type Review = {
  kind: ContainerAction;
  targets: ContainerTarget[];
  signature: string;
};
function signature(targets: ContainerTarget[]) {
  return JSON.stringify(
    targets
      .map((t) => [
        t.stackId,
        t.container.id,
        t.container.state,
        t.container.onDemandSleeping,
        t.archived,
        t.busy,
      ])
      .sort((a, b) => JSON.stringify(a).localeCompare(JSON.stringify(b))),
  );
}
function eligible(targets: ContainerTarget[], kind: ContainerAction) {
  return (
    targets.length > 0 &&
    targets.length <= 20 &&
    targets.every(
      (t) =>
        !t.archived &&
        !t.busy &&
        (kind === "start"
          ? ["created", "exited"].includes(t.container.state)
          : t.container.state === "running"),
    )
  );
}
export function useContainerBatchActions(options: {
  targets: ContainerTarget[];
  onAccepted: (operations: Operation[]) => void;
}) {
  const latest = useRef(options);
  latest.current = options;
  const live = useRef(true);
  useEffect(
    () => () => {
      live.current = false;
    },
    [],
  );
  const [review, setReview] = useState<Review>();
  const captured = useRef<Review>();
  const [pending, setPending] = useState(false);
  const busy = useRef(false);
  const [error, setError] = useState("");
  const [outcomes, setOutcomes] = useState<string[]>([]);
  function cancel() {
    if (busy.current) return;
    captured.current = undefined;
    setReview(undefined);
    setError("");
  }
  function request(kind: ContainerAction) {
    const targets = latest.current.targets;
    if (busy.current || !eligible(targets, kind)) return;
    const next = {
      kind,
      targets: targets.map((t) => ({ ...t, container: { ...t.container } })),
      signature: signature(targets),
    };
    captured.current = next;
    setReview(next);
    setError("");
  }
  async function confirm() {
    const value = captured.current;
    if (!value || busy.current) return;
    if (
      signature(latest.current.targets) !== value.signature ||
      !eligible(latest.current.targets, value.kind)
    ) {
      setError(
        "Selected containers or their state changed. Cancel and review again.",
      );
      return;
    }
    busy.current = true;
    setPending(true);
    const groups = new Map<string, ContainerTarget[]>();
    for (const target of value.targets) {
      const group = groups.get(target.stackId) || [];
      group.push(target);
      groups.set(target.stackId, group);
    }
    const entries = [...groups];
    const results = await Promise.allSettled(
      entries.map(([id, targets]) =>
        runContainerBatchAction(
          id,
          targets.map((t) => t.container.id),
          value.kind,
        ),
      ),
    );
    if (!live.current) return;
    const accepted: Operation[] = [];
    setOutcomes(
      results.map((result, index) => {
        const name = entries[index][1][0].stackName;
        if (result.status === "rejected")
          return `${name}: ${message(result.reason)}`;
        accepted.push(result.value);
        return `${name}: accepted`;
      }),
    );
    if (accepted.length) latest.current.onAccepted(accepted);
    busy.current = false;
    setPending(false);
    captured.current = undefined;
    setReview(undefined);
  }
  return { review, pending, error, outcomes, request, confirm, cancel };
}
