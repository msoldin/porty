import { useState } from "preact/hooks";
import { Notice } from "../../components/Feedback";
import { useOnDemand } from "./useOnDemand";
import { groupPolicy } from "./onDemandTypes";
import { OnDemandForm } from "./OnDemandForm";
import { useStackContainers } from "./useStackContainers";
import "./onDemandSettings.css";

export function OnDemandSettings({
  stackId,
  archived = false,
}: {
  stackId: string;
  archived?: boolean;
}) {
  const state = useOnDemand(stackId);
  const [editing, setEditing] = useState<string>();
  const services = useStackContainers(stackId, Boolean(editing), "");
  return (
    <section class="on-demand-settings" aria-label="On-demand containers">
      <h2>On-demand containers</h2>
      <p class="muted">
        Stop idle services completely and wake them when someone connects.
        Running traffic goes directly through Docker.
      </p>
      <p class="muted">
        A wake attempt is one UDP datagram or one completed TCP connection.
        Clients may need to retry while the server starts.
      </p>
      {state.error && (
        <Notice>
          {state.error}
          <button onClick={() => void state.reload()}>Refresh groups</button>
        </Notice>
      )}
      {!state.groups && !state.error && (
        <p role="status">Loading on-demand groups…</p>
      )}
      {state.groups?.map((group) => (
        <article class="on-demand-group" key={group.id}>
          <header>
            <h3>{group.name}</h3>
            <span class="on-demand-state">
              {!group.enabled
                ? "Disabled"
                : group.holdReason
                  ? "Held"
                  : group.pausedReason
                    ? "Paused"
                    : group.phase}
            </span>
          </header>
          <p class="muted">
            {group.members.join(", ")} · Idle timeout: {group.idleSeconds / 60}{" "}
            min · Wake after {group.wakeThreshold} attempt
            {group.wakeThreshold === 1 ? "" : "s"}
          </p>
          {group.holdReason && <p>{group.holdReason}</p>}
          {group.pausedReason && <p>{group.pausedReason}</p>}
          {group.observationReason && (
            <Notice>{group.observationReason}</Notice>
          )}
          {editing === group.id ? (
            <OnDemandForm
              key={`${group.id}:${group.revision}`}
              group={group}
              groups={state.groups || []}
              containers={services.containers}
              servicesError={services.error}
              busy={state.busy || archived}
              save={async (value) => {
                if (await state.save(group.id, value)) setEditing(undefined);
              }}
              cancel={() => setEditing(undefined)}
            />
          ) : (
            <div class="on-demand-actions">
              <button
                disabled={state.busy || archived}
                onClick={() => setEditing(group.id)}
              >
                Edit {group.name}
              </button>
              <button
                disabled={state.busy || archived}
                onClick={() =>
                  void state.save(group.id, {
                    ...groupPolicy(group),
                    enabled: !group.enabled,
                    expectedRevision: group.revision,
                  })
                }
              >
                {group.enabled ? "Disable" : "Enable"} {group.name}
              </button>
              {group.holdReason || group.pausedReason ? (
                <button
                  disabled={state.busy || archived}
                  onClick={() => void state.change(group, "resume")}
                >
                  Resume {group.name}
                </button>
              ) : (
                <button
                  disabled={state.busy || archived || !group.enabled}
                  onClick={() => void state.change(group, "hold")}
                >
                  Hold {group.name}
                </button>
              )}
              <button
                disabled={state.busy || archived}
                onClick={() => void state.change(group, "delete")}
              >
                Remove {group.name}
              </button>
            </div>
          )}
        </article>
      ))}
      {editing === "new" ? (
        <OnDemandForm
          key={stackId}
          groups={state.groups || []}
          containers={services.containers}
          servicesError={services.error}
          busy={state.busy || archived}
          save={async (value) => {
            if (await state.save("", value)) setEditing(undefined);
          }}
          cancel={() => setEditing(undefined)}
        />
      ) : (
        state.groups && (
          <button
            disabled={state.busy || archived}
            onClick={() => setEditing("new")}
          >
            Add on-demand group
          </button>
        )
      )}
      <p class="muted">
        Create groups from deployed, running services with fixed published
        ports. UDP host and container ports must match. Containerized Porty
        requires host networking.
      </p>
      <p class="muted">
        Any container network activity resets the idle timer. Manual stop holds
        the group until you choose Resume. Hold, Disable, and Remove leave
        running containers up.
      </p>
    </section>
  );
}
