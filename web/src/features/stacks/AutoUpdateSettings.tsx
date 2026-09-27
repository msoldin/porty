import { useEffect, useState } from "preact/hooks";
import { Notice } from "../../components/Feedback";
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
  return (
    <section class="auto-update-settings">
      <h2>Automatic updates</h2>
      <button disabled={state.busy} onClick={() => void state.reload()}>
        Refresh update status
      </button>
      <p class="muted">
        Opt in to update public image tags while the entire stack is running and
        healthy. The default runs daily at midnight UTC. Stopped stacks are
        skipped.
      </p>
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
        <label class="confirmation">
          <input
            type="checkbox"
            checked={enabled}
            disabled={
              state.busy ||
              !policy ||
              archived ||
              (!state.status?.available && !policy.enabled)
            }
            onChange={(event) => setEnabled(event.currentTarget.checked)}
          />
          Enable automatic updates
        </label>
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
        <p class="muted">
          Five fields: minute, hour, day of month, month, day of week. Missed
          runs are skipped.
        </p>
        <p>
          Recreation discards container writable-layer data. Volumes are
          retained; application migrations are not rolled back.
        </p>
        <button
          disabled={
            state.busy ||
            !policy ||
            archived ||
            (enabled && !state.status?.available)
          }
        >
          Save schedule
        </button>
      </form>
      {policy?.enabled && !policy.pausedReason && (
        <p>Next run (UTC): {new Date(policy.nextRunAt).toISOString()}</p>
      )}
      {state.status?.eligibilityReason && (
        <p>{state.status.eligibilityReason}</p>
      )}
      {Object.entries(state.status?.excluded || {}).map(([service, reason]) => (
        <p key={service}>
          <strong>{service}</strong>: {reason}
        </p>
      ))}
      {state.status?.lastRun && (
        <p>
          Last check: {state.status.lastRun.outcome || "in progress"}
          {state.status.lastRun.reason &&
            ` — ${state.status.lastRun.reason.replaceAll("_", " ")}`}{" "}
          · {new Date(state.status.lastRun.scheduledAt).toISOString()}
        </p>
      )}
      {state.status?.lastRun?.operationId && openOperation && (
        <button
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
      {onAlerts ? (
        <button onClick={onAlerts}>Review stack alerts</button>
      ) : (
        <a href="#/alerts">Review alerts</a>
      )}
    </section>
  );
}
