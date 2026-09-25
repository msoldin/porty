import { useEffect, useState } from "preact/hooks";
import { message } from "../../lib/http";
import {
  listDeployments,
  runContainerBatchAction,
  runStackAction,
} from "./api";
import { type Stack, type Deployment, type ContainerAction } from "./types";
import { type Repository } from "../repository/types";
import { type Operation } from "../operations/types";
import { Icon } from "../../components/Icon";
import { ActionMenu } from "../../components/ActionMenu";
import { Badge, Empty, Notice } from "../../components/Feedback";
import { isModified, remoteState } from "./stackStatus";
import {
  deploymentLabel,
  deploymentTone,
  remoteTone,
  stackRuntimePresentation,
} from "./statusPresentation";
import { Editor } from "./Editor";
import { StackSettings } from "./StackSettings";
import { useStackLogs } from "./useStackLogs";
import { useStackState } from "./useStackState";
import { useStackContainers } from "./useStackContainers";
import { ServicesTable } from "./ServicesTable";

export function StackDetail({
  stack,
  repo,
  operations,
  dirty,
  setDirty,
  navigate,
  refresh,
  onAction,
}: {
  stack: Stack;
  repo: Repository | null;
  operations: Operation[];
  dirty: boolean;
  setDirty: (dirty: boolean) => void;
  navigate: (path: string) => void;
  refresh: () => void;
  onAction: (operation: Operation) => void;
}) {
  const [tab, setTab] = useState("Overview");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [deployments, setDeployments] = useState<Deployment[]>([]);
  const stackLogs = useStackLogs(stack.id, tab === "Logs");
  const state = useStackState(stack.id);
  const containerRefreshKey = operations
    .filter(
      (item) =>
        item.scopeId === stack.id &&
        item.kind.startsWith("container_") &&
        ["succeeded", "failed"].includes(item.status),
    )
    .map((item) => `${item.id}:${item.status}`)
    .join("|");
  const containerState = useStackContainers(
    stack.id,
    tab === "Overview",
    containerRefreshKey,
  );
  const showRuntimeActions =
    state?.hasDeployed && state.runtime === "running" && !stack.archivedAt;
  const active = operations.find(
    (operation) =>
      operation.scopeId === stack.id &&
      ["running", "queued"].includes(operation.status),
  );
  const freshness =
    active?.kind === "deploy" || active?.kind === "recreate"
      ? "deploying"
      : state?.freshness;
  useEffect(() => {
    if (tab === "History" || tab === "Overview")
      listDeployments(stack.id)
        .then((value) => setDeployments(value || []))
        .catch((error) => setError(message(error)));
  }, [stack.id, tab, operations]);
  async function action(kind: string) {
    if (dirty) {
      setError(
        "Save or discard your editor changes before running a stack action.",
      );
      return;
    }
    if (
      ["stop", "recreate"].includes(kind) &&
      !confirm(
        `${kind === "stop" ? "Stop" : "Recreate"} ${stack.directoryName}?`,
      )
    )
      return;
    setBusy(true);
    setError("");
    try {
      onAction(await runStackAction(stack.id, kind));
    } catch (error) {
      setError(message(error));
    } finally {
      setBusy(false);
    }
  }
  async function containerAction(
    containerIds: string[],
    kind: ContainerAction,
  ) {
    if (busy || active || stack.archivedAt) return;
    if (dirty) {
      setError(
        "Save or discard your editor changes before running a stack action.",
      );
      return;
    }
    setBusy(true);
    setError("");
    try {
      onAction(await runContainerBatchAction(stack.id, containerIds, kind));
    } catch (cause) {
      setError(message(cause));
    } finally {
      setBusy(false);
    }
  }
  return (
    <section class="stack-detail">
      <div class="stack-top">
        <a
          href="#/"
          aria-label="Overview"
          onClick={(event) => {
            event.preventDefault();
            navigate("/");
          }}
        >
          Overview
        </a>
        <span class="muted"> / {stack.directoryName}</span>
        <div class="page-heading">
          <h1>{stack.directoryName}</h1>
          <div class="action-group">
            <button
              class="accent"
              disabled={busy || !!active || !!stack.archivedAt || dirty}
              onClick={() => action("validate")}
            >
              <Icon name="Check" />
              Validate
            </button>
            <ActionMenu
              label="Actions"
              disabled={busy || !!active || !!stack.archivedAt}
              items={[
                {
                  id: "deploy",
                  label: "Deploy",
                  disabled: dirty,
                  reason: "Save or discard editor changes",
                },
                {
                  id: "restart",
                  label: "Restart",
                  disabled: dirty || !showRuntimeActions,
                  reason: "Requires a running, previously deployed stack",
                },
                {
                  id: "stop",
                  label: "Stop",
                  disabled: dirty || !showRuntimeActions,
                  reason: "Requires a running, previously deployed stack",
                },
              ]}
              onSelect={(kind) => void action(kind)}
            />
          </div>
        </div>
        <div class="state-strip">
          <div>
            <Badge tone={stackRuntimePresentation(state?.runtime).tone} dot>
              {stackRuntimePresentation(state?.runtime).label}
            </Badge>
            <small>Docker state updates automatically</small>
          </div>
          <div>
            <Badge tone={isModified(stack, repo) ? "warning" : "neutral"}>
              {repo
                ? isModified(stack, repo)
                  ? "Modified"
                  : "Clean"
                : "Unavailable"}
            </Badge>
            <small>Working tree</small>
          </div>
          <div>
            <Badge tone={remoteTone(repo)}>{remoteState(repo)}</Badge>
            <small>Repository remote</small>
          </div>
          <div>
            <Badge tone={deploymentTone(freshness)}>
              {deploymentLabel(freshness)}
            </Badge>
            <small>Deployment freshness</small>
          </div>
        </div>
      </div>
      <div class="tabs" role="tablist" aria-label="Stack sections">
        {["Overview", "Editor", "Logs", "History", "Settings"].map((name) => (
          <button
            role="tab"
            aria-selected={tab === name}
            onClick={() => {
              if (dirty && !confirm("Discard unsaved changes?")) return;
              setDirty(false);
              setTab(name);
              setError("");
            }}
          >
            {name}
          </button>
        ))}
      </div>
      {error && <Notice>{error}</Notice>}
      {tab === "Editor" && (
        <Editor
          stack={stack}
          dirty={dirty}
          setDirty={setDirty}
          refresh={refresh}
        />
      )}
      {tab === "Settings" && (
        <StackSettings stack={stack} onChanged={refresh} />
      )}
      {tab === "Overview" && (
        <div class="detail-content">
          <ServicesTable
            stackId={stack.id}
            navigate={navigate}
            containers={containerState.containers}
            error={containerState.error}
            busy={busy || !!active}
            archived={!!stack.archivedAt}
            dirty={dirty}
            onBatchAction={containerAction}
          />
          <h2>Last deployment</h2>
          {deployments.length ? (
            <p>
              <Badge
                tone={
                  deployments[0].status === "succeeded" ? "success" : "danger"
                }
              >
                {deployments[0].status}
              </Badge>{" "}
              {new Date(deployments[0].startedAt).toLocaleString()} ·{" "}
              {deployments[0].gitCommit?.slice(0, 7) || "No commit"}
            </p>
          ) : (
            <Empty>No deployments recorded.</Empty>
          )}
          <h2>Compose project</h2>
          <code>{stack.composeProjectName}</code>
        </div>
      )}
      {tab === "Logs" && (
        <div class="detail-content">
          <h2>Container logs</h2>
          <p class="muted">Latest 500 lines. Updates automatically.</p>
          {stackLogs.status === "loading" && !stackLogs.error && (
            <p role="status">Loading logs…</p>
          )}
          {stackLogs.status === "reconnecting" && (
            <p role="status">Reconnecting to logs…</p>
          )}
          {stackLogs.status === "disconnected" && (
            <p role="status">Log connection unavailable. Retrying…</p>
          )}
          {stackLogs.error && <Notice>{stackLogs.error}</Notice>}
          <pre class="output">
            {stackLogs.output ||
              (stackLogs.status === "connected"
                ? "Waiting for container output…"
                : "")}
          </pre>
          {stackLogs.gap && (
            <Notice>Some log output was missed. Refreshing logs…</Notice>
          )}
        </div>
      )}
      {tab === "History" && (
        <div class="detail-content">
          <h2>Deployment history</h2>
          <div class="table-scroll">
            <table>
              <thead>
                <tr>
                  <th>Started</th>
                  <th>Result</th>
                  <th>Commit</th>
                  <th>Working tree</th>
                </tr>
              </thead>
              <tbody>
                {deployments.map((deployment) => (
                  <tr key={deployment.id}>
                    <td>{new Date(deployment.startedAt).toLocaleString()}</td>
                    <td>
                      <Badge
                        tone={
                          deployment.status === "succeeded"
                            ? "success"
                            : "danger"
                        }
                      >
                        {deployment.status}
                      </Badge>
                    </td>
                    <td>
                      <code>{deployment.gitCommit?.slice(0, 7) || "—"}</code>
                    </td>
                    <td>{deployment.dirty ? "Modified" : "Clean"}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          {!deployments.length && <Empty>No deployments recorded.</Empty>}
        </div>
      )}
    </section>
  );
}
