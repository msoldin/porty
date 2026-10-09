import { Empty } from "../../components/Feedback";
import type { AuditEvent } from "./types";

export function Audit({ events }: { events: AuditEvent[] }) {
  return (
    <div class="detail-content audit-page">
      <h1>Audit log</h1>
      <p class="muted">
        Recorded actions and their outcomes. Runtime progress and output are
        available in Operations.
      </p>
      {events.map((event) => (
        <article class="commit-row" key={event.id}>
          <code>{event.outcome}</code>
          <div>
            <strong>{event.action}</strong>
            <p class="muted">
              {event.targetId || event.targetType} ·{" "}
              {new Date(event.occurredAt).toLocaleString()}
            </p>
          </div>
        </article>
      ))}
      {!events.length && <Empty>No audit events recorded.</Empty>}
    </div>
  );
}
