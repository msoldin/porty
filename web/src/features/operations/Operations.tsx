import type { Operation } from "./types";
import { Badge, Empty } from "../../components/Feedback";
import { operationPresentation } from "./operationPresentation";
import "./operations.css";
export function Operations({
  operations,
  open,
}: {
  operations: Operation[];
  open: (operation: Operation) => void;
}) {
  const active = operations.filter(
    (operation) => !operationPresentation(operation).terminal,
  );
  const history = operations.filter(
    (operation) => operationPresentation(operation).terminal,
  );
  return (
    <div class="detail-content operations-page">
      <h1>Operations</h1>
      <p class="muted">
        Accepted actions appear as queued. Open an operation to follow progress
        or inspect its result.
      </p>
      {[
        { name: "Active", items: active },
        { name: "History", items: history },
      ].map((group) => (
        <section key={group.name} aria-label={group.name}>
          <h2>
            {group.name} <span class="muted">{group.items.length}</span>
          </h2>
          {group.items.length ? (
            <div class="table-scroll">
              <table>
                <thead>
                  <tr>
                    <th>Action</th>
                    <th>Scope</th>
                    <th>Status</th>
                    <th>Trigger</th>
                    <th>Started</th>
                  </tr>
                </thead>
                <tbody>
                  {group.items.map((operation) => {
                    const state = operationPresentation(operation);
                    return (
                      <tr key={operation.id}>
                        <td>
                          <button
                            class="text-button"
                            onClick={() => open(operation)}
                          >
                            {operation.kind}
                          </button>
                        </td>
                        <td>{operation.scopeId || "Repository"}</td>
                        <td>
                          <Badge tone={state.tone}>{state.label}</Badge>
                        </td>
                        <td>{operation.trigger || "Manual"}</td>
                        <td>
                          {operation.startedAt &&
                          !operation.startedAt.startsWith("0001-")
                            ? new Date(operation.startedAt).toLocaleString()
                            : "Not started"}
                        </td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </div>
          ) : (
            <Empty>
              {group.name === "Active"
                ? "No active operations."
                : "No completed operations yet."}
            </Empty>
          )}
        </section>
      ))}
    </div>
  );
}
