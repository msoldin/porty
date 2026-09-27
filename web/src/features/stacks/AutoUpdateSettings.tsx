import { useEffect, useState } from "preact/hooks";
import { Notice } from "../../components/Feedback";
import "./autoUpdateSettings.css";
import { useAutoUpdate } from "./useAutoUpdate";
export function AutoUpdateSettings({
  stackId,
  archived = false,
  onAlerts,
  openOperation,
}: {
  stackId: string;
  archived?: boolean;
  onAlerts?: () => void;
  openOperation?: (id: string) => void;
}) {
  const state = useAutoUpdate(stackId);
  const [enabled, setEnabled] = useState(false);
  const [expression, setExpression] = useState("0 0 * * *");
  useEffect(() => {
    if (state.status) {
      setEnabled(state.status.policy.enabled);
      setExpression(state.status.policy.expression);
    }
  }, [state.status]);
  const policy = state.status?.policy;
  const formatUTC = (value: string) =>
    new Intl.DateTimeFormat("en-GB", {
      day: "2-digit",
      month: "short",
      year: "numeric",
      hour: "2-digit",
      minute: "2-digit",
      hourCycle: "h23",
      timeZone: "UTC",
    }).format(new Date(value)) + " UTC";
  return (
    <section class="auto-update-settings">
      <header class="auto-update-heading">
        <div>
          <h2>Automatic updates</h2>
          <p class="muted">
            Update public images on a schedule. Stopped stacks are skipped.
          </p>
        </div>
        <label class="auto-update-toggle">
          <input
            type="checkbox"
            aria-label="Enable automatic updates"
            checked={enabled}
            disabled={
              state.busy ||
              !policy ||
              archived ||
              (!state.status?.available && !policy.enabled)
            }
            onChange={(event) => setEnabled(event.currentTarget.checked)}
          />
          <span>{enabled ? "Enabled" : "Disabled"}</span>
        </label>
      </header>
      {state.error && (
        <Notice>
          {state.error}
          <button onClick={() => void state.reload()}>
            Refresh update settings
          </button>
        </Notice>
      )}
      {!state.status && !state.error && (
        <p role="status">Loading update settings…</p>
      )}
      {state.status && !state.status.available && (
        <Notice>
          Automatic updates unavailable:{" "}
          {state.status.availabilityReason?.replaceAll("_", " ")}
        </Notice>
      )}
      <form
        onSubmit={(event) => {
          event.preventDefault();
          if (policy)
            void state.save({
              enabled,
              expression,
              expectedRevision: policy.revision,
            });
        }}
      >
        <div class="auto-update-schedule">
          <label>
            Cron schedule (UTC)
            <input
              value={expression}
              maxLength={256}
              required
              disabled={state.busy || !policy || archived}
              onInput={(event) => setExpression(event.currentTarget.value)}
            />
          </label>
          <button
            class="primary"
            disabled={
              state.busy ||
              !policy ||
              archived ||
              (enabled && !state.status?.available)
            }
          >
            Save schedule
          </button>
        </div>
        <p class="auto-update-schedule-hint muted">
          {expression.trim() === "0 0 * * *"
            ? "Daily at midnight UTC"
            : "Custom schedule · UTC"}
        </p>
      </form>
      {policy?.enabled && !policy.pausedReason && (
        <p class="auto-update-next muted">
          Next run:{" "}
          <time dateTime={policy.nextRunAt}>{formatUTC(policy.nextRunAt)}</time>
        </p>
      )}
      {state.status?.eligibilityReason && (
        <p>{state.status.eligibilityReason}</p>
      )}
      {Object.entries(state.status?.excluded || {}).map(([service, reason]) => (
        <p class="auto-update-excluded muted" key={service}>
          <strong>{service}</strong>: {reason}
        </p>
      ))}
      {state.status?.lastRun && (
        <p class="auto-update-last muted">
          Last check: {state.status.lastRun.outcome || "in progress"}
          {state.status.lastRun.reason &&
            ` — ${state.status.lastRun.reason.replaceAll("_", " ")}`}{" "}
          · {formatUTC(state.status.lastRun.scheduledAt)}
        </p>
      )}
      {state.status?.lastRun?.operationId && openOperation && (
        <button
          class="text-button"
          onClick={() => openOperation(state.status!.lastRun!.operationId!)}
        >
          View last update operation
        </button>
      )}
      {policy?.pausedReason && (
        <Notice>
          <p>
            Automatic updates paused: {policy.pausedReason.replaceAll("_", " ")}
            . Recover the stack manually, then verify recovery here.
            Acknowledging an alert does not resume updates.
          </p>
          <button
            disabled={state.busy || !state.status?.available || archived}
            onClick={() => void state.resume()}
          >
            {state.busy
              ? "Verifying recovery…"
              : policy.enabled
                ? "Verify recovery and resume"
                : "Verify recovery and clear pause"}
          </button>
        </Notice>
      )}
      <p class="auto-update-safety muted">
        Volumes are retained. Writable-layer data is lost; migrations cannot be
        rolled back.
      </p>
      <footer class="auto-update-footer">
        <details>
          <summary>How updates work</summary>
          <p>
            Only fully running, healthy stacks are updated. Use five cron
            fields: minute, hour, day of month, month, day of week. Missed runs
            are skipped. Recreation can cause downtime.
          </p>
        </details>
        <div class="auto-update-links">
          <button
            class="text-button"
            aria-label="Refresh update status"
            disabled={state.busy}
            onClick={() => void state.reload()}
          >
            Refresh
          </button>
          {onAlerts ? (
            <button
              class="text-button"
              aria-label="Review stack alerts"
              onClick={onAlerts}
            >
              Stack alerts
            </button>
          ) : (
            <a href="#/alerts">Alerts</a>
          )}
        </div>
      </footer>
    </section>
  );
}
