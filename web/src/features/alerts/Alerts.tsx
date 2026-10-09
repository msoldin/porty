import { useEffect, useState } from "preact/hooks";
import { Notice } from "../../components/Feedback";
import { message } from "../../lib/http";
import { getAlert } from "./api";
import { AlertList } from "./AlertList";
import { useAlerts } from "./useAlerts";
import type { AlertDetail, AlertView } from "./types";
export function Alerts({
  stackId,
  streamEnabled = true,
  navigate,
  openOperation,
}: {
  stackId?: string;
  streamEnabled?: boolean;
  navigate: (path: string) => void;
  openOperation: (id: string) => void;
}) {
  const state = useAlerts(stackId, streamEnabled);
  const [history, setHistory] = useState<string>();
  return (
    <section class="detail-content">
      <div class="page-heading">
        <h1>{stackId ? "Stack alerts" : "Alerts"}</h1>
        <button onClick={() => void state.reload()} disabled={state.loading}>
          Refresh
        </button>
      </div>
      <p class="muted">
        Acknowledge incidents after reviewing them. Recovery is tracked
        separately.
      </p>
      <label>
        Show{" "}
        <select
          value={state.view}
          onChange={(event) =>
            state.setView(event.currentTarget.value as AlertView)
          }
        >
          <option value="attention">Needs attention</option>
          <option value="open">Open</option>
          <option value="unacknowledged">Unacknowledged</option>
          <option value="all">All history</option>
        </select>
      </label>
      {state.error && <Notice>{state.error}</Notice>}
      {state.loading && <p role="status">Loading alerts…</p>}
      {state.page && (
        <>
          <AlertList
            items={state.page.items}
            busy={state.busy || state.loading}
            acknowledge={state.acknowledge}
            resolve={state.resolve}
            navigate={navigate}
            openOperation={openOperation}
            openHistory={setHistory}
          />
          <div class="action-group">
            <button
              disabled={state.offset === 0 || state.loading}
              onClick={() => state.setOffset(Math.max(0, state.offset - 25))}
            >
              Previous alerts
            </button>
            <span>{state.page.total} alerts</span>
            <button
              disabled={state.offset + 25 >= state.page.total || state.loading}
              onClick={() => state.setOffset(state.offset + 25)}
            >
              Next alerts
            </button>
          </div>
        </>
      )}
      {history && (
        <AlertHistory
          key={history}
          id={history}
          close={() => setHistory(undefined)}
          openOperation={openOperation}
        />
      )}
    </section>
  );
}
function AlertHistory({
  id,
  close,
  openOperation,
}: {
  id: string;
  close: () => void;
  openOperation: (id: string) => void;
}) {
  const [offset, setOffset] = useState(0);
  const [detail, setDetail] = useState<AlertDetail>();
  const [error, setError] = useState("");
  useEffect(() => {
    let active = true;
    setDetail(undefined);
    getAlert(id, offset)
      .then((value) => {
        if (active) {
          setDetail(value);
          setError("");
        }
      })
      .catch((cause) => {
        if (active) setError(message(cause));
      });
    return () => {
      active = false;
    };
  }, [id, offset]);
  return (
    <section aria-label="Alert history" class="alert-card">
      <div class="page-heading">
        <h2>Alert history</h2>
        <button onClick={close}>Close history</button>
      </div>
      {error && <Notice>{error}</Notice>}
      {detail?.history.map((event) => (
        <p key={event.id}>
          {new Date(event.at).toLocaleString()} · {event.kind} · Episode{" "}
          {event.episode} · {event.actorId || "System"}
          {event.note && ` · ${event.note}`}{" "}
          {event.operationId && (
            <button onClick={() => openOperation(event.operationId!)}>
              View operation
            </button>
          )}
        </p>
      ))}
      <div class="action-group">
        <button
          disabled={!offset || !detail}
          onClick={() => setOffset(Math.max(0, offset - 25))}
        >
          Previous events
        </button>
        <button
          disabled={!detail || detail.history.length < 25}
          onClick={() => setOffset(offset + 25)}
        >
          Next events
        </button>
      </div>
    </section>
  );
}
