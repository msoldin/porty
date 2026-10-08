import { useState } from "preact/hooks";
import { Notice } from "../../components/Feedback";
import { useOnDemand } from "./useOnDemand";
import { defaultOnDemandPolicy, groupPolicy } from "./onDemandTypes";
import type {
  OnDemandGroup,
  OnDemandPolicy,
  OnDemandUpdate,
} from "./onDemandTypes";
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

type NumberField =
  | "wakeThreshold"
  | "wakeWindowMs"
  | "idleSeconds"
  | "minRuntimeSeconds"
  | "startupSeconds"
  | "stopGraceSeconds";
const fields: { key: NumberField; label: string; min: number; max: number }[] =
  [
    { key: "wakeThreshold", label: "Wake attempts", min: 1, max: 1000 },
    {
      key: "wakeWindowMs",
      label: "Wake window (milliseconds)",
      min: 10,
      max: 60000,
    },
    {
      key: "idleSeconds",
      label: "Idle timeout (seconds)",
      min: 60,
      max: 86400,
    },
    {
      key: "minRuntimeSeconds",
      label: "Minimum runtime (seconds)",
      min: 0,
      max: 3600,
    },
    {
      key: "startupSeconds",
      label: "Startup timeout (seconds)",
      min: 30,
      max: 900,
    },
    {
      key: "stopGraceSeconds",
      label: "Stop grace per service (seconds)",
      min: 10,
      max: 120,
    },
  ];
function OnDemandForm({
  group,
  busy,
  save,
  cancel,
}: {
  group?: OnDemandGroup;
  busy: boolean;
  save: (value: OnDemandUpdate) => Promise<void>;
  cancel: () => void;
}) {
  const [policy, setPolicy] = useState<OnDemandPolicy>(() =>
    group ? groupPolicy(group) : { ...defaultOnDemandPolicy },
  );
  const [members, setMembers] = useState(group?.members.join(", ") || "");
  return (
    <form
      class="on-demand-form"
      onSubmit={(event) => {
        event.preventDefault();
        void save({
          ...policy,
          members: members
            .split(",")
            .map((value) => value.trim())
            .filter(Boolean),
          expectedRevision: group?.revision || 0,
        });
      }}
    >
      <fieldset disabled={busy}>
        <legend>{group ? `Edit ${group.name}` : "New on-demand group"}</legend>
        <div class="on-demand-fields">
          <label>
            Group name
            <input
              required
              maxLength={64}
              value={policy.name}
              onInput={(event) =>
                setPolicy({ ...policy, name: event.currentTarget.value })
              }
            />
          </label>
          <label>
            Services
            <input
              required
              placeholder="game, companion"
              value={members}
              onInput={(event) => setMembers(event.currentTarget.value)}
            />
          </label>
          {fields.map((field) => (
            <label key={field.key}>
              {field.label}
              <input
                type="number"
                required
                min={field.min}
                max={field.max}
                step={1}
                value={policy[field.key]}
                onInput={(event) =>
                  setPolicy({
                    ...policy,
                    [field.key]: event.currentTarget.valueAsNumber,
                  })
                }
              />
            </label>
          ))}
        </div>
        <p class="muted">
          Use Compose service names separated by commas. Attempts must arrive
          within the wake window.
        </p>
        <label class="confirmation">
          <input
            type="checkbox"
            checked={policy.enabled}
            onChange={(event) =>
              setPolicy({ ...policy, enabled: event.currentTarget.checked })
            }
          />
          Enable automatic wake and sleep
        </label>
        <div class="on-demand-actions">
          <button class="primary">Save group</button>
          <button type="button" onClick={cancel}>
            Cancel
          </button>
        </div>
      </fieldset>
    </form>
  );
}
