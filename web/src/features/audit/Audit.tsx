import { Empty } from "../../components/Feedback";
import type { AuditEvent } from "./types";

export function Audit({ events }: { events: AuditEvent[] }) {
  return (
    <div class="detail-content">
      <h1>Audit</h1>
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
