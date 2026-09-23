import type { Operation } from "./types";
import { Icon } from "../../components/Icon";
import { Badge, Notice } from "../../components/Feedback";

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
