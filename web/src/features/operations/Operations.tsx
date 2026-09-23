import type { Operation } from "./types";
import { Badge, Empty } from "../../components/Feedback";

export function Operations({
  operations,
  open,
}: {
  operations: Operation[];
  open: (operation: Operation) => void;
}) {
  return (
    <div class="detail-content">
      <h1>Operations</h1>
      <div class="table-scroll">
        <table>
          <thead>
            <tr>
              <th>Action</th>
              <th>Scope</th>
              <th>Status</th>
              <th>Started</th>
            </tr>
          </thead>
          <tbody>
            {operations.map((operation) => (
              <tr key={operation.id}>
                <td>
                  <button class="text-button" onClick={() => open(operation)}>
                    {operation.kind}
                  </button>
                </td>
                <td>{operation.scopeId || "Repository"}</td>
                <td>
                  <Badge
                    tone={
                      operation.status === "failed"
                        ? "danger"
                        : operation.status === "succeeded"
                          ? "success"
                          : "blue"
                    }
                  >
                    {operation.status}
                  </Badge>
                </td>
                <td>
                  {operation.startedAt
                    ? new Date(operation.startedAt).toLocaleString()
                    : "Queued"}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      {!operations.length && <Empty>No operations yet.</Empty>}
    </div>
  );
}
