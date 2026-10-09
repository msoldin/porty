import { useEffect, useRef, useState } from "preact/hooks";
import { APIError, message } from "../lib/http";
import { listStacks } from "../features/stacks/api";
import type { Stack } from "../features/stacks/types";
import { getRepositoryStatus, listCommits } from "../features/repository/api";
import type { Repository, Commit } from "../features/repository/types";
import { listOperations } from "../features/operations/api";
import { useOperationStream } from "../features/operations/useOperationStream";
import type { Operation } from "../features/operations/types";
import { listAudit } from "../features/audit/api";
import type { AuditEvent } from "../features/audit/types";

type ReadStatus = {
  loading: boolean;
  loaded: boolean;
  stale: boolean;
  error: string;
};
const resourceNames = [
  "stacks",
  "repository",
  "commits",
  "operations",
  "audit",
] as const;
type Resources = Record<(typeof resourceNames)[number], ReadStatus>;

export function useWorkspaceData(logout: () => void, enabled = true) {
  const [stacks, setStacks] = useState<Stack[]>([]);
  const [repo, setRepo] = useState<Repository | null>(null);
  const [commits, setCommits] = useState<Commit[]>([]);
  const [operations, setOperations] = useState<Operation[]>([]);
  const [audit, setAudit] = useState<AuditEvent[]>([]);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);
  const request = useRef(0);
  const enabledRef = useRef(enabled);
  enabledRef.current = enabled;
  const [resources, setResources] = useState<Resources>(
    () =>
      Object.fromEntries(
        resourceNames.map((name) => [
          name,
          { loading: true, loaded: false, stale: false, error: "" },
        ]),
      ) as Resources,
  );

  async function refresh(): Promise<void> {
    if (!enabledRef.current) return;
    const generation = ++request.current;
    setResources(
      (current) =>
        Object.fromEntries(
          resourceNames.map((name) => [
            name,
            { ...current[name], loading: true },
          ]),
        ) as Resources,
    );
    const result = await Promise.allSettled([
      listStacks(),
      getRepositoryStatus(),
      listCommits(),
      listOperations(),
      listAudit(),
    ]);
    if (!enabledRef.current || generation !== request.current) return;
    if (
      result.some(
        (value) =>
          value.status === "rejected" &&
          value.reason instanceof APIError &&
          value.reason.status === 401,
      )
    ) {
      logout();
      return;
    }
    if (result[0].status === "fulfilled") setStacks(result[0].value || []);
    if (result[1].status === "fulfilled") setRepo(result[1].value);
    if (result[2].status === "fulfilled") setCommits(result[2].value || []);
    if (result[3].status === "fulfilled") setOperations(result[3].value || []);
    if (result[4].status === "fulfilled") setAudit(result[4].value || []);
    setResources(
      (current) =>
        Object.fromEntries(
          resourceNames.map((name, index) => {
            const value = result[index];
            return [
              name,
              value.status === "fulfilled"
                ? { loading: false, loaded: true, stale: false, error: "" }
                : {
                    loading: false,
                    loaded: current[name].loaded,
                    stale: current[name].loaded,
                    error: message(value.reason),
                  },
            ];
          }),
        ) as Resources,
    );
    const failed = result.find((value) => value.status === "rejected");
    setError(failed?.status === "rejected" ? message(failed.reason) : "");
    setLoading(false);
  }

  useEffect(() => {
    refresh();
    return () => {
      request.current++;
    };
  }, [enabled]);
  const stream = useOperationStream(
    (operation) => {
      if (!enabledRef.current) return;
      setOperations((values) =>
        [
          operation,
          ...values.filter((value) => value.id !== operation.id),
        ].slice(0, 50),
      );
      if (["succeeded", "failed", "cancelled"].includes(operation.status))
        refresh();
    },
    refresh,
    enabled,
  );

  function addOperations(accepted: Operation[]): void {
    const ids = new Set(accepted.map((operation) => operation.id));
    setOperations((values) =>
      [...accepted, ...values.filter((value) => !ids.has(value.id))].slice(
        0,
        50,
      ),
    );
  }

  function addOperation(operation: Operation): void {
    addOperations([operation]);
  }

  return {
    stacks,
    repo,
    commits,
    operations,
    audit,
    error,
    setError,
    loading,
    resources,
    refresh,
    stream,
    addOperation,
    addOperations,
  };
}
