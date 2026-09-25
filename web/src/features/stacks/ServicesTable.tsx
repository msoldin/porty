import { useEffect, useState } from "preact/hooks";
import { Empty, Notice } from "../../components/Feedback";
import { Icon } from "../../components/Icon";
import { ServiceCells } from "./ServiceCells";
import type { Container, ContainerAction } from "./types";

export function ServicesTable({
  containers,
  error,
  busy,
  archived,
  dirty,
  onBatchAction,
}: {
  containers: Container[] | undefined;
  error: string;
  busy: boolean;
  archived: boolean;
  dirty: boolean;
  onBatchAction: (ids: string[], action: ContainerAction) => void;
}) {
  const [search, setSearch] = useState("");
  const [selected, setSelected] = useState<Set<string>>(new Set());
  useEffect(() => {
    if (!containers) return;
    const known = new Set(containers.map((container) => container.id));
    setSelected((current) => {
      if ([...current].every((id) => known.has(id))) return current;
      return new Set([...current].filter((id) => known.has(id)));
    });
  }, [containers]);
  if (error) return <Notice>{error}</Notice>;
  if (!containers) return <p role="status">Loading containers…</p>;
  if (!containers.length)
    return <Empty>No containers yet. Deploy this stack to create them.</Empty>;
  const query = search.toLowerCase();
  const visible = containers.filter((container) =>
    [
      container.service,
      container.name,
      container.image,
      ...container.networks,
    ].some((value) => value.toLowerCase().includes(query)),
  );
  const selectedRows = visible.filter((container) =>
    selected.has(container.id),
  );
  const allVisibleSelected =
    visible.length > 0 &&
    visible.every((container) => selected.has(container.id));
  const available =
    selectedRows.length > 0 &&
    selectedRows.length <= 20 &&
    !busy &&
    !archived &&
    !dirty;
  const canStart =
    available &&
    selectedRows.every((container) =>
      ["created", "exited"].includes(container.state),
    );
  const canRun =
    available &&
    selectedRows.every((container) => container.state === "running");
  function run(action: ContainerAction) {
    if (action === "start" ? !canStart : !canRun) return;
    if (
      action === "stop" &&
      !confirm("Stop " + selectedRows.length + " selected containers?")
    )
      return;
    onBatchAction(
      selectedRows.map((container) => container.id),
      action,
    );
  }

  return (
    <div class="services">
      <div class="services-heading">
        <div>
          <h2>Services</h2>
          <p class="muted">Docker container state updates automatically.</p>
        </div>
        <label class="search service-search">
          <Icon name="Search" />
          <input
            aria-label="Search services"
            placeholder="Search services…"
            value={search}
            onInput={(event) => {
              setSearch(event.currentTarget.value);
              setSelected(new Set());
            }}
          />
        </label>
      </div>
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
              onClick={() => run("start")}
            >
              Start
            </button>
            <button
              type="button"
              class="small"
              aria-label="Restart selected"
              disabled={!canRun}
              onClick={() => run("restart")}
            >
              Restart
            </button>
            <button
              type="button"
              class="small"
              aria-label="Stop selected"
              disabled={!canRun}
              onClick={() => run("stop")}
            >
              Stop
            </button>
            <button
              type="button"
              class="text-button"
              onClick={() => setSelected(new Set())}
            >
              Clear
            </button>
          </div>
        </div>
      )}
      <div
        class="table-scroll"
        role="region"
        aria-label="Services table"
        tabIndex={0}
      >
        <table class="services-table">
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
                  disabled={visible.length === 0 || visible.length > 20 || busy}
                  title={
                    visible.length > 20
                      ? "Select at most 20 services"
                      : undefined
                  }
                  onChange={(event) =>
                    setSelected(
                      event.currentTarget.checked
                        ? new Set(visible.map((container) => container.id))
                        : new Set(),
                    )
                  }
                />
              </th>
              <th>Service</th>
              <th>State</th>
              <th>Image</th>
              <th>Networks</th>
              <th>Ports</th>
            </tr>
          </thead>
          <tbody>
            {visible.map((container) => (
              <tr
                key={container.id}
                class={selected.has(container.id) ? "selected-row" : undefined}
              >
                <td class="select-column">
                  <input
                    type="checkbox"
                    aria-label={"Select " + container.name}
                    checked={selected.has(container.id)}
                    disabled={
                      busy ||
                      (selected.size >= 20 && !selected.has(container.id))
                    }
                    onChange={(event) =>
                      setSelected((current) => {
                        const next = new Set(current);
                        if (event.currentTarget.checked) next.add(container.id);
                        else next.delete(container.id);
                        return next;
                      })
                    }
                  />
                </td>
                <ServiceCells container={container} />
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      {visible.length === 0 && <Empty>No services match your search.</Empty>}
    </div>
  );
}
