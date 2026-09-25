import { useEffect, useState } from "preact/hooks";
import { Badge, Empty, Notice } from "../../components/Feedback";
import { Icon } from "../../components/Icon";
import { message } from "../../lib/http";
import type { Operation } from "../operations/types";
import { ServiceCells } from "./ServiceCells";
import { runContainerBatchAction } from "./api";
import { useOverviewContainers } from "./useOverviewContainers";
import type { OverviewContainer } from "./overviewContainers";
import type { ContainerAction, Stack } from "./types";

const rowKey = ({ stackId, container }: OverviewContainer) =>
  JSON.stringify([stackId, container.id]);

export function OverviewServices({
  stacks,
  operations,
  navigate,
  onOperationsAccepted,
}: {
  stacks: Stack[];
  operations: Operation[];
  navigate: (path: string) => void;
  onOperationsAccepted: (operations: Operation[]) => void;
}) {
  const [search, setSearch] = useState("");
  const [filter, setFilter] = useState("all");
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [busy, setBusy] = useState(false);
  const [feedback, setFeedback] = useState("");
  const stackById = new Map(stacks.map((stack) => [stack.id, stack]));
  const refreshKey = operations
    .filter(
      (operation) =>
        operation.kind.startsWith("container_") &&
        !["queued", "running"].includes(operation.status),
    )
    .map((operation) => operation.id + ":" + operation.status)
    .sort()
    .join("|");
  const { rows, errors, loading } = useOverviewContainers(
    stacks.map((stack) => stack.id),
    refreshKey,
  );
  const ordered = rows
    .filter((row) => stackById.has(row.stackId))
    .sort(
      (a, b) =>
        stackById
          .get(a.stackId)!
          .directoryName.localeCompare(
            stackById.get(b.stackId)!.directoryName,
          ) ||
        a.container.service.localeCompare(b.container.service) ||
        a.container.name.localeCompare(b.container.name) ||
        a.container.id.localeCompare(b.container.id),
    );
  const query = search.trim().toLowerCase();
  const visible = ordered.filter(
    ({ stackId, container }) =>
      [
        stackById.get(stackId)!.directoryName,
        container.service,
        container.name,
        container.image,
        ...container.networks,
      ].some((value) => value.toLowerCase().includes(query)) &&
      (filter === "all" ||
        (filter === "running" && container.state === "running") ||
        (filter === "stopped" &&
          ["created", "exited"].includes(container.state)) ||
        (filter === "unhealthy" && container.health === "unhealthy")),
  );
  const selectable = visible.filter(
    (row) => !stackById.get(row.stackId)!.archivedAt,
  );
  const visibleKeys = new Set(selectable.map(rowKey));
  useEffect(() => {
    setSelected((current) => {
      if ([...current].every((key) => visibleKeys.has(key))) return current;
      return new Set([...current].filter((key) => visibleKeys.has(key)));
    });
  }, [rows, stacks, search, filter]);
  const selectedRows = selectable.filter((row) => selected.has(rowKey(row)));
  const activeStackIds = new Set(
    operations
      .filter((operation) => ["queued", "running"].includes(operation.status))
      .map((operation) => operation.scopeId),
  );
  const available =
    !busy &&
    selectedRows.length > 0 &&
    selectedRows.length <= 20 &&
    selectedRows.every((row) => !activeStackIds.has(row.stackId));
  const canStart =
    available &&
    selectedRows.every(({ container }) =>
      ["created", "exited"].includes(container.state),
    );
  const canRun =
    available &&
    selectedRows.every(({ container }) => container.state === "running");
  const allVisibleSelected =
    selectable.length > 0 &&
    selectable.every((row) => selected.has(rowKey(row)));
  const runningCount = ordered.filter(
    ({ container }) => container.state === "running",
  ).length;

  async function run(action: ContainerAction) {
    if (action === "start" ? !canStart : !canRun) return;
    const groups = new Map<string, string[]>();
    for (const row of selectedRows) {
      const ids = groups.get(row.stackId) || [];
      ids.push(row.container.id);
      groups.set(row.stackId, ids);
    }
    if (
      action === "stop" &&
      !confirm(
        `Stop ${selectedRows.length} selected containers across ${groups.size} ${groups.size === 1 ? "stack" : "stacks"}?`,
      )
    )
      return;
    setBusy(true);
    setFeedback("");
    const entries = [...groups];
    const results = await Promise.allSettled(
      entries.map(([id, ids]) => runContainerBatchAction(id, ids, action)),
    );
    const accepted: Operation[] = [];
    const completed = new Set<string>();
    const outcomes = results.map((result, index) => {
      const [id] = entries[index];
      const name = stackById.get(id)?.directoryName || id;
      if (result.status === "rejected")
        return `${name}: ${message(result.reason)}`;
      accepted.push(result.value);
      completed.add(id);
      return `${name}: accepted`;
    });
    if (accepted.length) onOperationsAccepted(accepted);
    setSelected(
      (current) =>
        new Set(
          [...current].filter(
            (key) => !completed.has((JSON.parse(key) as [string, string])[0]),
          ),
        ),
    );
    setFeedback(outcomes.join(" · "));
    setBusy(false);
  }

  return (
    <section class="overview-services">
      <div class="services-heading">
        <div>
          <h2>Services</h2>
          <p class="muted">
            {ordered.length} {ordered.length === 1 ? "container" : "containers"}{" "}
            · {runningCount} running
          </p>
        </div>
        <div class="overview-service-filters">
          <label class="search service-search">
            <Icon name="Search" />
            <input
              aria-label="Search all services"
              placeholder="Search all services…"
              value={search}
              onInput={(event) => {
                setSearch(event.currentTarget.value);
                setSelected(new Set());
              }}
            />
          </label>
          <select
            aria-label="Filter services state"
            value={filter}
            onChange={(event) => {
              setFilter(event.currentTarget.value);
              setSelected(new Set());
            }}
          >
            <option value="all">All states</option>
            <option value="running">Running</option>
            <option value="stopped">Stopped</option>
            <option value="unhealthy">Unhealthy</option>
          </select>
        </div>
      </div>
      {errors.length > 0 && (
        <Notice>
          Partial inventory ·{" "}
          {errors
            .map(
              ({ stackId, message }) =>
                `${stackById.get(stackId)?.directoryName || stackId}: ${message}`,
            )
            .join(" · ")}
        </Notice>
      )}
      {feedback && (
        <div class="selection-feedback" role="status">
          {feedback}
        </div>
      )}
      {selectedRows.length > 0 && (
        <div class="selection-bar service-selection">
          <strong>
            {selectedRows.length}{" "}
            {selectedRows.length === 1 ? "container" : "containers"} selected
          </strong>
          <div class="service-selection-actions">
            <button
              type="button"
              class="small"
              aria-label="Start selected"
              disabled={!canStart}
              onClick={() => void run("start")}
            >
              Start
            </button>
            <button
              type="button"
              class="small"
              aria-label="Restart selected"
              disabled={!canRun}
              onClick={() => void run("restart")}
            >
              Restart
            </button>
            <button
              type="button"
              class="small"
              aria-label="Stop selected"
              disabled={!canRun}
              onClick={() => void run("stop")}
            >
              Stop
            </button>
          </div>
          <button
            type="button"
            class="text-button"
            onClick={() => setSelected(new Set())}
          >
            Clear
          </button>
        </div>
      )}
      <div
        class="table-scroll"
        role="region"
        aria-label="All services table"
        tabIndex={0}
      >
        <table class="services-table overview-services-table">
          <thead>
            <tr>
              <th class="select-column">
                <input
                  type="checkbox"
                  aria-label="Select all visible services"
                  checked={allVisibleSelected}
                  ref={(node) => {
                    if (node)
                      node.indeterminate =
                        selectedRows.length > 0 && !allVisibleSelected;
                  }}
                  disabled={
                    busy || selectable.length === 0 || selectable.length > 20
                  }
                  title={
                    selectable.length > 20
                      ? "Select at most 20 services"
                      : undefined
                  }
                  onChange={(event) =>
                    setSelected(
                      event.currentTarget.checked
                        ? new Set(selectable.map(rowKey))
                        : new Set(),
                    )
                  }
                />
              </th>
              <th>Stack</th>
              <th>Service</th>
              <th>State</th>
              <th>Image</th>
              <th>Networks</th>
              <th>Ports</th>
            </tr>
          </thead>
          <tbody>
            {visible.map((row) => {
              const stack = stackById.get(row.stackId)!;
              const key = rowKey(row);
              return (
                <tr
                  key={key}
                  class={selected.has(key) ? "selected-row" : undefined}
                >
                  <td class="select-column">
                    <input
                      type="checkbox"
                      aria-label={`Select ${stack.directoryName} ${row.container.name}`}
                      checked={selected.has(key)}
                      disabled={
                        busy ||
                        !!stack.archivedAt ||
                        (selected.size >= 20 && !selected.has(key))
                      }
                      onChange={(event) =>
                        setSelected((current) => {
                          const next = new Set(current);
                          if (event.currentTarget.checked) next.add(key);
                          else next.delete(key);
                          return next;
                        })
                      }
                    />
                  </td>
                  <td>
                    <div class="stack-name-cell">
                      <a
                        href={`#/stacks/${encodeURIComponent(stack.id)}`}
                        onClick={(event) => {
                          event.preventDefault();
                          navigate(`/stacks/${encodeURIComponent(stack.id)}`);
                        }}
                      >
                        {stack.directoryName}
                      </a>
                      {stack.archivedAt && <Badge>Archived</Badge>}
                    </div>
                  </td>
                  <ServiceCells container={row.container} />
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>
      {loading && ordered.length === 0 && (
        <p role="status">Loading services…</p>
      )}
      {!loading && ordered.length === 0 && errors.length === 0 && (
        <Empty>No containers exist yet.</Empty>
      )}
      {!loading && ordered.length > 0 && visible.length === 0 && (
        <Empty>No services match your filters.</Empty>
      )}
    </section>
  );
}
