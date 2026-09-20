import type { Operation } from "./api";
import { Badge, Empty, Icon, Notice } from "./ui";
export function OperationDrawer({
  operation,
  close,
}: {
  operation: Operation;
  close: () => void;
}) {
  return (
    <aside class="operation-drawer" aria-label="Operation details">
      <div class="drawer-title">
        <h2>
          <Icon name="Operations" />
          Operations
        </h2>
        <button
          class="icon-button"
          aria-label="Close operation"
          onClick={close}
        >
          <Icon name="Close" />
        </button>
      </div>
      <div class="drawer-body">
        <h3>
          {operation.kind} {operation.scopeId}
        </h3>
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
        {operation.errorCode && <Notice>{operation.errorCode}</Notice>}
        <h4>Output</h4>
        <pre class="output">
          {operation.output || "Waiting for operation output…"}
        </pre>
        {operation.outputTruncated && <Notice>Output was truncated.</Notice>}
      </div>
      <div class="drawer-footer">
        <button
          disabled
          title="Operation cancellation is not exposed by this server"
        >
          Cancel operation
        </button>
      </div>
    </aside>
  );
}
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
