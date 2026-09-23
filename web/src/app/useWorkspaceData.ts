import { useEffect, useState } from "preact/hooks";
import { APIError, message } from "../lib/http";
import { listStacksWithState } from "../features/stacks/api";
import type { Stack } from "../features/stacks/types";
import { getRepositoryStatus, listCommits } from "../features/repository/api";
import type { Repository, Commit } from "../features/repository/types";
import { listOperations } from "../features/operations/api";
import { useOperationStream } from "../features/operations/useOperationStream";
import type { Operation } from "../features/operations/types";
import { listAudit } from "../features/audit/api";
import type { AuditEvent } from "../features/audit/types";

export function useWorkspaceData(logout: () => void) {
  const [stacks, setStacks] = useState<Stack[]>([]);
  const [repo, setRepo] = useState<Repository | null>(null);
  const [commits, setCommits] = useState<Commit[]>([]);
  const [operations, setOperations] = useState<Operation[]>([]);
  const [audit, setAudit] = useState<AuditEvent[]>([]);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);

  async function refresh(): Promise<void> {
    const result = await Promise.allSettled([
      listStacksWithState(),
      getRepositoryStatus(),
      listCommits(),
      listOperations(),
      listAudit(),
    ]);
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
    const failed = result
      .slice(0, 4)
      .find((value) => value.status === "rejected");
    setError(failed?.status === "rejected" ? message(failed.reason) : "");
    setLoading(false);
  }

  useEffect(() => {
    refresh();
  }, []);
  const stream = useOperationStream((operation) => {
    setOperations((values) =>
      [operation, ...values.filter((value) => value.id !== operation.id)].slice(
        0,
        50,
      ),
    );
    if (["succeeded", "failed", "cancelled"].includes(operation.status))
      refresh();
  }, refresh);

  function addOperation(operation: Operation): void {
    setOperations((values) => [
      operation,
      ...values.filter((value) => value.id !== operation.id),
    ]);
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
    refresh,
    stream,
    addOperation,
  };
}
