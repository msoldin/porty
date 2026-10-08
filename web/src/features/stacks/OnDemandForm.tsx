import { useState } from "preact/hooks";
import { Notice } from "../../components/Feedback";
import { defaultOnDemandPolicy, groupPolicy } from "./onDemandTypes";
import type {
  OnDemandGroup,
  OnDemandPolicy,
  OnDemandUpdate,
} from "./onDemandTypes";
import type { Container } from "./types";
import { formatContainerPort } from "./serviceDisplay";

const advancedFields = [
  {
    key: "wakeWindowMs",
    label: "Wake window (milliseconds)",
    min: 10,
    max: 60000,
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
] as const;

export function OnDemandForm({
  group,
  groups,
  containers,
  servicesError,
  busy,
  save,
  cancel,
}: {
  group?: OnDemandGroup;
  groups: OnDemandGroup[];
  containers: Container[] | undefined;
  servicesError: string;
  busy: boolean;
  save: (value: OnDemandUpdate) => Promise<void>;
  cancel: () => void;
}) {
  const [policy, setPolicy] = useState<OnDemandPolicy>(() =>
    group ? groupPolicy(group) : { ...defaultOnDemandPolicy, members: [] },
  );
  const byService = new Map<string, Container[]>();
  for (const container of containers || []) {
    if (container.service)
      byService.set(container.service, [
        ...(byService.get(container.service) || []),
        container,
      ]);
  }
  const names = [
    ...new Set([...byService.keys(), ...(group?.members || [])]),
  ].sort();
  const choices = names.map((name) => {
    const replicas = byService.get(name) || [];
    const assigned = groups.find(
      (other) => other.id !== group?.id && other.members.includes(name),
    );
    const currentMember = group?.members.includes(name);
    const container = replicas[0];
    const reason = assigned
      ? `Already in ${assigned.name}`
      : replicas.length === 0
        ? "Not currently deployed"
        : replicas.length > 1
          ? "Requires a single container"
          : !currentMember && container.state !== "running"
            ? "Start this service before adding it"
            : container.state === "running" &&
                container.health &&
                container.health !== "healthy"
              ? "Wait until this service is healthy"
              : "";
    return { name, container, reason };
  });
  const membershipLocked =
    group?.phase === "sleeping" &&
    !(
      containers &&
      group.members.every((name) => {
        const replicas = byService.get(name);
        return replicas?.length === 1 && replicas[0].state === "running";
      })
    );
  const canSave = Boolean(
    containers &&
    !servicesError &&
    policy.members.length > 0 &&
    policy.members.length <= 8 &&
    policy.members.every((name) =>
      choices.some((choice) => choice.name === name && !choice.reason),
    ),
  );
  return (
    <form
      class="on-demand-form"
      onSubmit={(event) => {
        event.preventDefault();
        if (canSave && !busy)
          void save({ ...policy, expectedRevision: group?.revision || 0 });
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
              placeholder="Minecraft"
              onInput={(event) =>
                setPolicy({ ...policy, name: event.currentTarget.value })
              }
            />
          </label>
          <label>
            Sleep after inactivity (minutes)
            <input
              type="number"
              required
              min={1}
              max={1440}
              step="any"
              value={policy.idleSeconds / 60}
              onInput={(event) =>
                setPolicy({
                  ...policy,
                  idleSeconds: Math.round(
                    event.currentTarget.valueAsNumber * 60,
                  ),
                })
              }
            />
          </label>
        </div>
        <fieldset class="on-demand-services">
          <legend>Services</legend>
          <p class="muted">
            Choose up to 8 services to wake and sleep together.
          </p>
          {servicesError ? (
            <Notice>{servicesError}</Notice>
          ) : !containers ? (
            <p role="status">Loading services…</p>
          ) : !names.length ? (
            <p>No deployed services found. Deploy this stack first.</p>
          ) : null}
          {membershipLocked && (
            <p class="muted">
              Start the group before changing its services. You can still change
              its timing settings.
            </p>
          )}
          {choices.map(({ name, container, reason }) => {
            const checked = policy.members.includes(name);
            return (
              <label class="on-demand-service" key={name}>
                <input
                  type="checkbox"
                  checked={checked}
                  disabled={
                    membershipLocked ||
                    (!checked && Boolean(reason)) ||
                    (!checked && policy.members.length >= 8)
                  }
                  onChange={(event) =>
                    setPolicy({
                      ...policy,
                      members: event.currentTarget.checked
                        ? [...policy.members, name]
                        : policy.members.filter((member) => member !== name),
                    })
                  }
                />
                <span>
                  <strong>{name}</strong>
                  {container && <small>{container.image}</small>}
                  {container && (
                    <small>
                      {container.ports
                        .filter((port) => port.publishedPort > 0)
                        .map(formatContainerPort)
                        .join(" · ") ||
                        "No published ports; include a service that has one."}
                    </small>
                  )}
                  {reason && (
                    <small class="on-demand-service-reason">{reason}</small>
                  )}
                </span>
              </label>
            );
          })}
          <p class="muted">{policy.members.length} of 8 services selected</p>
        </fieldset>
        <div class="on-demand-fields">
          <label>
            Wake attempts
            <input
              type="number"
              required
              min={1}
              max={1000}
              step={1}
              value={policy.wakeThreshold}
              onInput={(event) =>
                setPolicy({
                  ...policy,
                  wakeThreshold: event.currentTarget.valueAsNumber,
                })
              }
            />
          </label>
        </div>
        <p class="muted">
          One attempt wakes the group immediately. The server still needs time
          to start.
        </p>
        <details class="on-demand-advanced">
          <summary>Advanced timing</summary>
          <div class="on-demand-fields">
            {advancedFields.map((field) => (
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
        </details>
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
          <button class="primary" disabled={!canSave}>
            Save group
          </button>
          <button type="button" onClick={cancel}>
            Cancel
          </button>
        </div>
      </fieldset>
    </form>
  );
}
