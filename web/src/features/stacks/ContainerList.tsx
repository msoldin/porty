import { Badge, Empty, Notice } from "../../components/Feedback";
import type { Container, ContainerAction } from "./types";

export function ContainerList({
  containers,
  error,
  busy,
  archived,
  onAction,
}: {
  containers: Container[] | undefined;
  error: string;
  busy: boolean;
  archived: boolean;
  onAction: (containerId: string, action: ContainerAction) => void;
}) {
  if (error) return <Notice>{error}</Notice>;
  if (!containers) return <p role="status">Loading containers…</p>;
  if (!containers.length)
    return <Empty>No containers yet. Deploy this stack to create them.</Empty>;

  return (
    <ul class="container-list">
      {containers.map((container) => {
        const actions: ContainerAction[] = archived
          ? []
          : container.state === "running"
            ? ["stop", "restart"]
            : container.state === "created" || container.state === "exited"
              ? ["start"]
              : [];
        return (
          <li key={container.id} class="container-row">
            <div class="container-identity">
              <strong>{container.name}</strong>
              <small>{container.service}</small>
            </div>
            <div class="container-status">
              <Badge
                tone={container.state === "running" ? "success" : "neutral"}
              >
                {container.state}
              </Badge>
              {container.health && (
                <span class="muted">{container.health}</span>
              )}
            </div>
            <div class="container-actions">
              {actions.map((action) => (
                <button
                  key={action}
                  type="button"
                  disabled={busy}
                  aria-label={`${action[0].toUpperCase() + action.slice(1)} ${container.name}`}
                  onClick={() => onAction(container.id, action)}
                >
                  {action[0].toUpperCase() + action.slice(1)}
                </button>
              ))}
            </div>
          </li>
        );
      })}
    </ul>
  );
}
