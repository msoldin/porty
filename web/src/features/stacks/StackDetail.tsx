import { Alerts } from "../alerts/Alerts";
import { useEffect, useState } from "preact/hooks";
import { message } from "../../lib/http";
import { listDeployments, runStackAction } from "./api";
import { type Stack, type Deployment } from "./types";
import { type Repository } from "../repository/types";
import { type Operation } from "../operations/types";
import { Icon } from "../../components/Icon";
import { ActionMenu } from "../../components/ActionMenu";
import { Badge, Empty, Notice } from "../../components/Feedback";
import { isModified } from "./stackStatus";
import {
  deploymentLabel,
  deploymentTone,
  stackRuntimePresentation,
} from "./statusPresentation";
import { Editor } from "./Editor";
import { useStackEditor } from "./useStackEditor";
import { useDeploymentReview } from "./useDeploymentReview";
import { DeploymentReviewDialog } from "./DeploymentReviewDialog";
import { ConfirmDialog } from "../../components/ConfirmDialog";
import "./stackDetail.css";
import { StackSettings } from "./StackSettings";
import { useStackLogs } from "./useStackLogs";
import { useStackState } from "./useStackState";
import { useStackContainers } from "./useStackContainers";
import { ServicesTable } from "./ServicesTable";

export function StackDetail({
  openOperation,
  stack,
  repo,
  operations,
  dirty,
  setDirty,
  navigate,
  refresh,
  onAction,
}: {
  openOperation?: (id: string) => void;
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
  const editor = useStackEditor({
    stackId: stack.id,
    enabled: tab === "Editor",
    onDirtyChange: setDirty,
    onSaved: refresh,
  });
  const [error, setError] = useState("");
  const [pendingAction, setPendingAction] = useState<string>();
  const review = useDeploymentReview({
    stack,
    operations,
    editor,
    onAccepted: onAction,
  });
  const [busy, setBusy] = useState(false);
  const [deployments, setDeployments] = useState<Deployment[]>([]);
  const [deploymentStatus, setDeploymentStatus] = useState<
    "loading" | "ready" | "error"
  >("loading");
  const [deploymentError, setDeploymentError] = useState("");
  const [deploymentRetry, setDeploymentRetry] = useState(0);
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
    let current = true;
    if (tab === "History" || tab === "Overview") {
      setDeploymentStatus("loading");
      setDeploymentError("");
      listDeployments(stack.id)
        .then((value) => {
          if (current) {
            setDeployments(value || []);
            setDeploymentStatus("ready");
          }
        })
        .catch((error) => {
          if (current) {
            setDeploymentError(message(error));
            setDeploymentStatus("error");
          }
        });
    }
    return () => {
      current = false;
    };
  }, [stack.id, tab, operations, deploymentRetry]);
  async function action(kind: string) {
    if (
      busy ||
      active ||
      stack.archivedAt ||
      (["stop", "restart"].includes(kind) && !showRuntimeActions)
    ) {
      setError(
        "Stack availability changed. Check its current state before trying again.",
      );
      return;
    }
    if (dirty) {
      setError(
        "Save or discard your editor changes before running a stack action.",
      );
      return;
    }
    setBusy(true);
    setError("");
    try {
      onAction(await runStackAction(stack.id, kind));
      setPendingAction(undefined);
    } catch (error) {
      setError(message(error));
    } finally {
      setBusy(false);
    }
  }

  const actionReason = stack.archivedAt
    ? "Archived stacks cannot run actions."
    : busy || active
      ? "Wait for the current operation to finish."
      : dirty
        ? "Save or discard your editor changes before validating, stopping, or restarting this stack."
        : "";
  const runtimeActionReason =
    actionReason || "Requires a running, previously deployed stack.";
  const deployReason = stack.archivedAt
    ? "Archived stacks cannot be deployed."
    : busy || active
      ? "Wait for the current operation to finish."
      : editor.busy
        ? "Wait for the file operation to finish."
        : review.phase !== "idle"
          ? "Finish or cancel the deployment review."
          : "";
  const deploymentFeedback =
    deploymentStatus === "loading" ? (
      <p role="status">Loading deployment history…</p>
    ) : deploymentStatus === "error" ? (
      <Notice>
        Deployment history could not be loaded: {deploymentError}
        {deployments.length > 0 &&
          " Previously loaded deployments may be out of date."}
        <button onClick={() => setDeploymentRetry((value) => value + 1)}>
          Retry deployment history
        </button>
      </Notice>
    ) : null;

  return (
    <section class="stack-detail">
      <div class="stack-top">
        <nav class="detail-breadcrumbs" aria-label="Breadcrumb">
          <a
            href="#/stacks"
            aria-label="Stacks"
            onClick={(event) => {
              event.preventDefault();
              navigate("/stacks");
            }}
          >
            Stacks
          </a>
          <span class="muted"> / {stack.directoryName}</span>
        </nav>
        <div class="page-heading">
          <div>
            <h1>{stack.directoryName}</h1>
            <p class="stack-project muted">
              Compose project: <code>{stack.composeProjectName}</code>
            </p>
          </div>
          <div class="action-group">
            <button
              disabled={!!actionReason}
              aria-describedby={
                actionReason ? "stack-action-reason" : undefined
              }
              onClick={() => action("validate")}
            >
              <Icon name="Check" /> Validate config
            </button>
            <button
              aria-describedby={
                deployReason ? "stack-deploy-reason" : undefined
              }
              class="primary"
              disabled={
                busy ||
                !!active ||
                !!stack.archivedAt ||
                editor.busy ||
                review.phase !== "idle"
              }
              onClick={() => void review.request()}
            >
              {freshness === "changes_pending"
                ? "Deploy changes…"
                : "Deploy stack…"}
            </button>
            <ActionMenu
              label="Actions"
              disabled={busy || !!active || !!stack.archivedAt}
              disabledReason={
                stack.archivedAt
                  ? "Archived stacks cannot run actions."
                  : "Wait for the current operation to finish."
              }
              items={[
                {
                  id: "restart",
                  label: "Restart",
                  disabled: dirty || !showRuntimeActions,
                  reason: runtimeActionReason,
                },
                {
                  id: "stop",
                  label: "Stop",
                  disabled: dirty || !showRuntimeActions,
                  reason: runtimeActionReason,
                },
              ]}
              onSelect={(kind) => {
                setError("");
                setPendingAction(kind);
              }}
            />
          </div>
        </div>
        {actionReason && (
          <p id="stack-action-reason" class="action-reason">
            {actionReason}
          </p>
        )}
        {deployReason && deployReason !== actionReason && (
          <p id="stack-deploy-reason" class="action-reason">
            {deployReason}
          </p>
        )}
        {deployReason && deployReason === actionReason && (
          <span id="stack-deploy-reason" class="visually-hidden">
            {deployReason}
          </span>
        )}
        <div class="state-strip" aria-label="Stack status">
          <div>
            <span class="state-label">Runtime</span>
            <Badge tone={stackRuntimePresentation(state?.runtime).tone} dot>
              {stackRuntimePresentation(state?.runtime).label}
            </Badge>
            <small>Current Docker state</small>
          </div>
          <div>
            <span class="state-label">Deployment</span>
            <Badge tone={deploymentTone(freshness)}>
              {deploymentLabel(freshness)}
            </Badge>
            <small>Saved configuration versus deployment</small>
          </div>
          <div>
            <span class="state-label">Git · This stack</span>
            <Badge tone={isModified(stack, repo) ? "warning" : "neutral"}>
              {repo
                ? isModified(stack, repo)
                  ? "Uncommitted files"
                  : "Committed files"
                : "Unavailable"}
            </Badge>
            <small>Saved locally · saving does not commit</small>
          </div>
        </div>
      </div>
      <div class="tabs" role="tablist" aria-label="Stack sections">
        {["Overview", "Editor", "Logs", "History", "Alerts", "Settings"].map(
          (name) => (
            <button
              id={`stack-tab-${name}`}
              role="tab"
              tabIndex={tab === name ? 0 : -1}
              aria-controls={`stack-panel-${name}`}
              aria-selected={tab === name}
              onKeyDown={(event) => {
                const names = [
                  "Overview",
                  "Editor",
                  "Logs",
                  "History",
                  "Alerts",
                  "Settings",
                ];
                const index = names.indexOf(name);
                const next =
                  event.key === "ArrowRight"
                    ? (index + 1) % names.length
                    : event.key === "ArrowLeft"
                      ? (index + names.length - 1) % names.length
                      : event.key === "Home"
                        ? 0
                        : event.key === "End"
                          ? names.length - 1
                          : -1;
                if (next < 0) return;
                event.preventDefault();
                setTab(names[next]);
                document.getElementById(`stack-tab-${names[next]}`)?.focus();
              }}
              onClick={() => {
                setTab(name);
                setError("");
              }}
            >
              {name === "Editor"
                ? "Compose & files"
                : name === "History"
                  ? "Deployments"
                  : name}
            </button>
          ),
        )}
      </div>
      {freshness === "changes_pending" && tab !== "Editor" && (
        <aside class="pending-configuration" aria-label="Undeployed changes">
          <div>
            <strong>Your saved changes are not running yet.</strong>
            <p>Review the configuration, then deploy this stack to apply it.</p>
          </div>
          <button class="text-button" onClick={() => setTab("Editor")}>
            Review files <span aria-hidden="true">→</span>
          </button>
        </aside>
      )}
      {error && <Notice>{error}</Notice>}
      <DeploymentReviewDialog name={stack.directoryName} model={review} />
      <ConfirmDialog
        open={!!pendingAction}
        title={`${pendingAction === "stop" ? "Stop" : "Restart"} ${stack.directoryName}?`}
        description={`This affects every service in ${stack.directoryName}. ${pendingAction === "stop" ? "Services will be unavailable until started again. Data volumes are retained." : "Services will briefly be unavailable while they restart."}`}
        confirmLabel={pendingAction === "stop" ? "Stop stack" : "Restart stack"}
        destructive={pendingAction === "stop"}
        busy={busy}
        error={error}
        onCancel={() => {
          setPendingAction(undefined);
          setError("");
        }}
        onConfirm={() => {
          if (pendingAction) void action(pendingAction);
        }}
      />
      <div
        role="tabpanel"
        class={tab === "Editor" ? "editor-tabpanel" : undefined}
        id={`stack-panel-${tab}`}
        aria-labelledby={`stack-tab-${tab}`}
        tabIndex={0}
      >
        {tab === "Alerts" && openOperation && (
          <Alerts
            stackId={stack.id}
            navigate={navigate}
            openOperation={openOperation}
          />
        )}
        {tab === "Editor" && <Editor stack={stack} model={editor} />}
        {tab === "Settings" && (
          <StackSettings
            stack={stack}
            onChanged={refresh}
            onAlerts={() => setTab("Alerts")}
            openOperation={openOperation}
          />
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
              stackName={stack.directoryName}
              onOperationsAccepted={(operations) => {
                operations.forEach((operation) => onAction(operation));
              }}
            />
            <section class="last-deployment" aria-label="Last deployment">
              <h2>Last deployment</h2>
              {deploymentFeedback}
              {deployments.length ? (
                <p>
                  <Badge
                    tone={
                      deployments[0].status === "succeeded"
                        ? "success"
                        : "danger"
                    }
                  >
                    {deployments[0].status}
                  </Badge>{" "}
                  {new Date(deployments[0].startedAt).toLocaleString()} ·{" "}
                  {deployments[0].gitCommit?.slice(0, 7) || "No commit"}
                  {deployments[0].operationId && openOperation && (
                    <button
                      class="text-button"
                      onClick={() => openOperation(deployments[0].operationId!)}
                    >
                      View operation
                    </button>
                  )}
                </p>
              ) : deploymentStatus === "ready" ? (
                <Empty>No deployments recorded.</Empty>
              ) : null}
            </section>
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
            {deploymentFeedback}
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
                        {deployment.operationId && openOperation && (
                          <button
                            class="text-button"
                            onClick={() =>
                              openOperation(deployment.operationId!)
                            }
                          >
                            View operation
                          </button>
                        )}
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
            {deploymentStatus === "ready" && !deployments.length && (
              <Empty>No deployments recorded.</Empty>
            )}
          </div>
        )}
      </div>
    </section>
  );
}
