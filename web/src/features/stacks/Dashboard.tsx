import { useState } from "preact/hooks";
import { message } from "../../lib/http";
import { createStack } from "./api";
import { type Stack } from "./types";
import { type Repository } from "../repository/types";
import { type Operation } from "../operations/types";
import { Icon } from "../../components/Icon";
import { Badge, Empty, Notice } from "../../components/Feedback";
import { isModified, remoteState } from "./stackStatus";
import { useStackState } from "./useStackState";

export function Dashboard({
  stacks,
  repo,
  operations,
  navigate,
  refresh,
}: {
  stacks: Stack[];
  repo: Repository | null;
  operations: Operation[];
  navigate: (path: string) => void;
  refresh: () => void;
}) {
  const [search, setSearch] = useState("");
  const [filter, setFilter] = useState("all");
  const [sort, setSort] = useState("asc");
  const [creating, setCreating] = useState(false);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
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
  return (
    <section class="dashboard">
      <div class="page-heading">
        <h1>Stacks</h1>
        <button class="primary" onClick={() => setCreating(!creating)}>
          <Icon name="Plus" />
          New stack
        </button>
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
              navigate(`/stacks/${encodeURIComponent(stack.id)}`);
            } catch (error) {
              setError(message(error));
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
      <div class="filters">
        <label class="search">
          <Icon name="Search" />
          <input
            aria-label="Search stacks"
            placeholder="Search stacks..."
            value={search}
            onInput={(event) => setSearch(event.currentTarget.value)}
          />
        </label>
        <select
          aria-label="Filter stacks"
          value={filter}
          onChange={(event) => setFilter(event.currentTarget.value)}
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
      <div class="table-scroll">
        <table class="stack-table">
          <thead>
            <tr>
              <th>Name ↑</th>
              <th>Runtime</th>
              <th>Working tree</th>
              <th>Remote</th>
              <th>Deployment</th>
              <th>Containers</th>
              <th>Last deployment</th>
              <th />
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
}: {
  stack: Stack;
  repo: Repository | null;
  operations: Operation[];
  navigate: (path: string) => void;
}) {
  const state = useStackState(stack.id);
  const active = operations.find(
    (operation) =>
      operation.scopeId === stack.id &&
      ["queued", "running"].includes(operation.status),
  );
  return (
    <tr>
      <td>
        <a
          href={`#/stacks/${encodeURIComponent(stack.id)}`}
          onClick={(event) => {
            event.preventDefault();
            navigate(`/stacks/${encodeURIComponent(stack.id)}`);
          }}
        >
          {stack.directoryName}
        </a>
      </td>
      <td>
        <span class="runtime">
          <i />
          {state?.runtime || "Unknown"}
        </span>
      </td>
      <td>
        <Badge tone={isModified(stack, repo) ? "warning" : "neutral"}>
          {repo
            ? isModified(stack, repo)
              ? "Modified"
              : "Clean"
            : "Unavailable"}
        </Badge>
      </td>
      <td>
        <Badge tone={repo?.behind ? "danger" : "success"}>
          {remoteState(repo)}
        </Badge>
      </td>
      <td>
        <Badge>
          {state?.freshness?.replaceAll("_", " ") ||
            (active ? `${active.kind}…` : "Unverified")}
        </Badge>
      </td>
      <td>
        {active ? (
          <Badge tone="blue">{active.kind}…</Badge>
        ) : (
          <span class="muted">Not checked</span>
        )}
      </td>
      <td class="muted">—</td>
      <td>
        <Icon name="Chevron" />
      </td>
    </tr>
  );
}
