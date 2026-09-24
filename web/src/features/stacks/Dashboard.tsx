import { useCallback, useEffect, useState } from "preact/hooks";
import { message } from "../../lib/http";
import { createStack, runStackAction } from "./api";
import type { Stack, StackState } from "./types";
import type { Repository } from "../repository/types";
import type { Operation } from "../operations/types";
import { Icon } from "../../components/Icon";
import { Badge, Empty, Notice } from "../../components/Feedback";
import { ActionMenu } from "../../components/ActionMenu";
import { isModified, remoteState } from "./stackStatus";
import {
  deploymentTone,
  remoteTone,
  stackRuntimePresentation,
} from "./statusPresentation";
import { useStackState } from "./useStackState";

function deploymentTime(
  value?: string,
): { absolute: string; relative: string } | undefined {
  if (!value) return;
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return;
  const absolute = new Intl.DateTimeFormat(undefined, {
    dateStyle: "medium",
    timeStyle: "short",
  }).format(date);
  const minutes = Math.round((date.getTime() - Date.now()) / 60000);
  const unit =
    Math.abs(minutes) >= 1440
      ? "day"
      : Math.abs(minutes) >= 60
        ? "hour"
        : "minute";
  const amount =
    unit === "day"
      ? Math.round(minutes / 1440)
      : unit === "hour"
        ? Math.round(minutes / 60)
        : minutes;
  return {
    absolute,
    relative: new Intl.RelativeTimeFormat(undefined, {
      numeric: "auto",
    }).format(amount, unit),
  };
}

export function Dashboard({
  stacks,
  repo,
  operations,
  navigate,
  refresh,
  onOperationsAccepted,
}: {
  stacks: Stack[];
  repo: Repository | null;
  operations: Operation[];
  navigate: (path: string) => void;
  refresh: () => void;
  onOperationsAccepted: (operations: Operation[]) => void;
}) {
  const [search, setSearch] = useState("");
  const [filter, setFilter] = useState("all");
  const [sort, setSort] = useState("asc");
  const [creating, setCreating] = useState(false);
  const [error, setError] = useState("");
  const [feedback, setFeedback] = useState("");
  const [busy, setBusy] = useState(false);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [states, setStates] = useState<Record<string, StackState | undefined>>(
    {},
  );
  const onState = useCallback((id: string, state: StackState | undefined) => {
    setStates((current) => {
      const old = current[id];
      if (
        old === state ||
        (old &&
          state &&
          old.runtime === state.runtime &&
          old.freshness === state.freshness &&
          old.hasDeployed === state.hasDeployed)
      )
        return current;
      return { ...current, [id]: state };
    });
  }, []);
  useEffect(() => {
    const known = new Set(stacks.map((stack) => stack.id));
    setSelected((current) => {
      if ([...current].every((id) => known.has(id))) return current;
      return new Set([...current].filter((id) => known.has(id)));
    });
  }, [stacks]);
  const visible = stacks
    .filter(
      (stack) =>
        !stack.archivedAt &&
        stack.directoryName.toLowerCase().includes(search.toLowerCase()) &&
        (filter !== "modified" || isModified(stack, repo)),
    )
    .sort(
      (a, b) =>
        a.directoryName.localeCompare(b.directoryName) *
        (sort === "asc" ? 1 : -1),
    );
  const selectedStacks = visible.filter((stack) => selected.has(stack.id));
  const allVisibleSelected =
    visible.length > 0 && visible.every((stack) => selected.has(stack.id));
  const activeIDs = new Set(
    operations
      .filter((operation) => ["queued", "running"].includes(operation.status))
      .map((operation) => operation.scopeId),
  );
  const canDeploy =
    !busy &&
    selectedStacks.length > 0 &&
    selectedStacks.length <= 20 &&
    selectedStacks.every((stack) => !activeIDs.has(stack.id));
  const canRun =
    canDeploy &&
    selectedStacks.every((stack) => {
      const state = states[stack.id];
      return state?.runtime === "running" && state.hasDeployed;
    });

  async function runSelected(kind: string) {
    if (kind === "deploy" ? !canDeploy : !canRun) return;
    if (
      kind === "stop" &&
      !confirm("Stop " + selectedStacks.length + " selected stacks?")
    )
      return;
    setBusy(true);
    setError("");
    setFeedback("");
    const results = await Promise.allSettled(
      selectedStacks.map((stack) => runStackAction(stack.id, kind)),
    );
    const accepted: Operation[] = [];
    const outcomes = results.map((result, index) => {
      const name = selectedStacks[index].directoryName;
      if (result.status === "fulfilled") {
        accepted.push(result.value);
        return name + ": accepted";
      }
      return name + ": " + message(result.reason);
    });
    if (accepted.length) onOperationsAccepted(accepted);
    setFeedback(outcomes.join(" · "));
    setSelected(new Set());
    setBusy(false);
  }

  return (
    <section class="dashboard">
      <div class="page-heading">
        <h1>Stacks</h1>
        <div class="action-group">
          <button class="primary" onClick={() => setCreating(!creating)}>
            <Icon name="Plus" />
            New stack
          </button>
          <ActionMenu
            label="Actions"
            items={[
              {
                id: "deploy",
                label: "Deploy",
                disabled: !canDeploy,
                reason: "Select up to 20 available stacks",
              },
              {
                id: "restart",
                label: "Restart",
                disabled: !canRun,
                reason: "Select running, previously deployed stacks",
              },
              {
                id: "stop",
                label: "Stop",
                disabled: !canRun,
                reason: "Select running, previously deployed stacks",
              },
            ]}
            onSelect={(kind) => void runSelected(kind)}
          />
        </div>
      </div>
      {creating && (
        <form
          class="create-stack inline-form"
          onSubmit={async (event) => {
            event.preventDefault();
            const data = new FormData(event.currentTarget);
            setBusy(true);
            setError("");
            try {
              const stack = await createStack(data.get("name"));
              setCreating(false);
              refresh();
              navigate("/stacks/" + encodeURIComponent(stack.id));
            } catch (cause) {
              setError(message(cause));
            } finally {
              setBusy(false);
            }
          }}
        >
          <label>
            Stack name
            <input
              name="name"
              required
              pattern="[a-z0-9][a-z0-9_-]*"
              placeholder="my-stack"
              autoFocus
            />
          </label>
          <button class="primary" disabled={busy}>
            Create stack
          </button>
          <button type="button" onClick={() => setCreating(false)}>
            Cancel
          </button>
        </form>
      )}
      {error && <Notice>{error}</Notice>}
      {feedback && (
        <div class="selection-feedback" role="status">
          {feedback}
        </div>
      )}
      <div class="filters">
        <label class="search">
          <Icon name="Search" />
          <input
            aria-label="Search stacks"
            placeholder="Search stacks..."
            value={search}
            onInput={(event) => {
              setSearch(event.currentTarget.value);
              setSelected(new Set());
            }}
          />
        </label>
        <select
          aria-label="Filter stacks"
          value={filter}
          onChange={(event) => {
            setFilter(event.currentTarget.value);
            setSelected(new Set());
          }}
        >
          <option value="all">All status</option>
          <option value="modified">Modified</option>
        </select>
        <select
          aria-label="Sort stacks"
          value={sort}
          onChange={(event) => setSort(event.currentTarget.value)}
        >
          <option value="asc">Name ↑</option>
          <option value="desc">Name ↓</option>
        </select>
      </div>
      {selectedStacks.length > 0 && (
        <div class="selection-bar">
          <strong>
            {selectedStacks.length}{" "}
            {selectedStacks.length === 1 ? "stack" : "stacks"} selected
          </strong>
          <button
            type="button"
            class="text-button"
            onClick={() => setSelected(new Set())}
          >
            Clear selection
          </button>
        </div>
      )}
      <div
        class="table-scroll"
        role="region"
        aria-label="Stacks table"
        tabIndex={0}
      >
        <table class="stack-table">
          <thead>
            <tr>
              <th class="select-column">
                <input
                  type="checkbox"
                  aria-label="Select all visible stacks"
                  checked={allVisibleSelected}
                  ref={(node) => {
                    if (node)
                      node.indeterminate =
                        selectedStacks.length > 0 && !allVisibleSelected;
                  }}
                  disabled={visible.length === 0 || visible.length > 20 || busy}
                  title={
                    visible.length > 20 ? "Select at most 20 stacks" : undefined
                  }
                  onChange={(event) =>
                    setSelected(
                      event.currentTarget.checked
                        ? new Set(visible.map((stack) => stack.id))
                        : new Set(),
                    )
                  }
                />
              </th>
              <th>Name</th>
              <th>Runtime</th>
              <th>Remote</th>
              <th>Deployment</th>
              <th>Last deployment</th>
            </tr>
          </thead>
          <tbody>
            {visible.map((stack) => (
              <StackRow
                key={stack.id}
                stack={stack}
                repo={repo}
                operations={operations}
                navigate={navigate}
                selected={selected.has(stack.id)}
                selectionDisabled={
                  busy || (selected.size >= 20 && !selected.has(stack.id))
                }
                onSelect={(checked) =>
                  setSelected((current) => {
                    const next = new Set(current);
                    if (checked) next.add(stack.id);
                    else next.delete(stack.id);
                    return next;
                  })
                }
                onState={onState}
              />
            ))}
          </tbody>
        </table>
      </div>
      {visible.length === 0 && (
        <Empty>
          {stacks.length
            ? "No stacks match your filters."
            : "No stacks yet. Create your first stack to get started."}
        </Empty>
      )}
    </section>
  );
}

function StackRow({
  stack,
  repo,
  operations,
  navigate,
  selected,
  selectionDisabled,
  onSelect,
  onState,
}: {
  stack: Stack;
  repo: Repository | null;
  operations: Operation[];
  navigate: (path: string) => void;
  selected: boolean;
  selectionDisabled: boolean;
  onSelect: (checked: boolean) => void;
  onState: (id: string, state: StackState | undefined) => void;
}) {
  const state = useStackState(stack.id);
  useEffect(() => onState(stack.id, state), [stack.id, state, onState]);
  const active = operations.find(
    (operation) =>
      operation.scopeId === stack.id &&
      ["queued", "running"].includes(operation.status),
  );
  const runtime = stackRuntimePresentation(state?.runtime);
  const freshness =
    active?.kind === "deploy" || active?.kind === "recreate"
      ? "deploying"
      : state?.freshness;
  const time = deploymentTime(stack.lastDeploymentAt);
  return (
    <tr class={selected ? "selected-row" : undefined}>
      <td class="select-column">
        <input
          type="checkbox"
          aria-label={"Select " + stack.directoryName}
          checked={selected}
          disabled={selectionDisabled}
          onChange={(event) => onSelect(event.currentTarget.checked)}
        />
      </td>
      <td>
        <a
          class="stack-name-link"
          title={stack.directoryName}
          href={"#/stacks/" + encodeURIComponent(stack.id)}
          onClick={(event) => {
            event.preventDefault();
            navigate("/stacks/" + encodeURIComponent(stack.id));
          }}
        >
          <Icon name="Stacks" />
          {stack.directoryName}
        </a>
      </td>
      <td>
        <Badge tone={runtime.tone} dot>
          {runtime.label}
        </Badge>
      </td>
      <td>
        <Badge tone={remoteTone(repo)}>{remoteState(repo)}</Badge>
      </td>
      <td>
        <Badge tone={deploymentTone(freshness)}>
          {freshness ? freshness.replaceAll("_", " ") : "Unverified"}
        </Badge>
      </td>
      <td>
        {time ? (
          <div class="deployment-time">
            <time dateTime={stack.lastDeploymentAt}>{time.absolute}</time>
            <small>{time.relative}</small>
          </div>
        ) : (
          <span class="muted">Never</span>
        )}
      </td>
    </tr>
  );
}
