import { useState } from "preact/hooks";
import { Badge, Empty } from "../../components/Feedback";
import type { Alert } from "./types";
export type AlertListProps = {
  items: Alert[];
  busy: boolean;
  acknowledge: (item: Alert) => void;
  resolve: (item: Alert, note: string) => void;
  navigate: (path: string) => void;
  openOperation: (id: string) => void;
  openHistory?: (id: string) => void;
};
export function AlertList(props: AlertListProps) {
  if (!props.items.length) return <Empty>No alerts in this view.</Empty>;
  return (
    <div class="alert-list">
      {props.items.map((item) => (
        <AlertRow key={item.id} {...props} item={item} />
      ))}
    </div>
  );
}
function AlertRow({
  item,
  busy,
  acknowledge,
  resolve,
  navigate,
  openOperation,
  openHistory,
}: AlertListProps & { item: Alert }) {
  const [editing, setEditing] = useState(false);
  const [note, setNote] = useState("");
  return (
    <article
      class="alert-card"
      aria-label={`${item.stackName}: ${item.summary}`}
    >
      <div class="page-heading">
        <h2>{item.summary}</h2>
        <Badge tone={item.resolvedAt ? "neutral" : "danger"}>
          {item.resolvedAt ? "Resolved" : "Open"}
        </Badge>
      </div>
      <p>
        <a
          href={`#/stacks/${encodeURIComponent(item.key.stackId)}`}
          onClick={(event) => {
            event.preventDefault();
            navigate(`/stacks/${encodeURIComponent(item.key.stackId)}`);
          }}
        >
          {item.stackName || item.key.stackId}
        </a>{" "}
        · {item.key.problem} · {item.key.target}
      </p>
      <p class="muted">
        Episode {item.episode} · {item.count} occurrence
        {item.count === 1 ? "" : "s"} · Latest{" "}
        {new Date(item.latestAt).toLocaleString()}
      </p>
      <p>
        {item.acknowledgedAt
          ? `Acknowledged by ${item.acknowledgedBy} · ${new Date(item.acknowledgedAt).toLocaleString()}`
          : "Needs acknowledgment"}
      </p>
      {item.resolvedAt && (
        <p>
          Resolved by {item.resolvedBy || "verified recovery"} ·{" "}
          {new Date(item.resolvedAt).toLocaleString()}
          {item.resolution && ` · ${item.resolution}`}
        </p>
      )}
      <div class="action-group">
        {!item.acknowledgedAt && (
          <button disabled={busy} onClick={() => acknowledge(item)}>
            Acknowledge
          </button>
        )}
        {!item.resolvedAt && item.canResolveManually && (
          <button disabled={busy} onClick={() => setEditing(!editing)}>
            Resolve manually
          </button>
        )}
        {item.operationId && (
          <button onClick={() => openOperation(item.operationId!)}>
            View operation
          </button>
        )}
        {openHistory && (
          <button onClick={() => openHistory(item.id)}>History</button>
        )}
      </div>
      {editing && !item.resolvedAt && (
        <form
          onSubmit={(event) => {
            event.preventDefault();
            resolve(item, note);
            setEditing(false);
          }}
        >
          <label>
            Resolution note (optional)
            <textarea
              maxLength={1024}
              value={note}
              onInput={(event) => setNote(event.currentTarget.value)}
            />
          </label>
          <p class="muted">
            Record manual recovery. This does not start containers or resume
            automatic updates.
          </p>
          <button disabled={busy} type="submit">
            Confirm resolution
          </button>
        </form>
      )}
    </article>
  );
}
