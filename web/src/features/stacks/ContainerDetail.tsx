import { useState } from "preact/hooks";
import { Badge, Notice } from "../../components/Feedback";
import { message } from "../../lib/http";
import type { Operation } from "../operations/types";
import { runContainerAction } from "./api";
import { formatContainerPort } from "./serviceDisplay";
import { containerStatePresentation } from "./statusPresentation";
import type { ContainerAction, Stack } from "./types";
import { useStackContainers } from "./useStackContainers";

export function ContainerDetail({
  stack,
  containerId,
  operations,
  dirty,
  navigate,
  onAction,
}: {
  stack: Stack;
  containerId: string;
  operations: Operation[];
  dirty: boolean;
  navigate: (path: string) => void;
  onAction: (operation: Operation) => void;
}) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const activeOperation = operations.some(
    (operation) =>
      operation.scopeId === stack.id &&
      ["running", "queued"].includes(operation.status),
  );
  const refreshKey = operations
    .filter(
      (operation) =>
        operation.scopeId === stack.id &&
        operation.kind.startsWith("container_") &&
        ["succeeded", "failed"].includes(operation.status),
    )
    .map((operation) => `${operation.id}:${operation.status}`)
    .join("|");
  const { containers, error: readError } = useStackContainers(
    stack.id,
    true,
    refreshKey,
  );
  const container = containers?.find((item) => item.id === containerId);
  const state = containerStatePresentation(container?.state, container?.health);
  const available =
    !!container &&
    !busy &&
    !activeOperation &&
    !stack.archivedAt &&
    !dirty &&
    !readError;
  const canStart = available && ["created", "exited"].includes(container.state);
  const canRun = available && container.state === "running";

  async function run(action: ContainerAction) {
    if (!container || (action === "start" ? !canStart : !canRun)) return;
    if (action === "stop" && !confirm(`Stop ${container.name}?`)) return;
    setBusy(true);
    setError("");
    try {
      onAction(await runContainerAction(stack.id, container.id, action));
    } catch (cause) {
      setError(message(cause));
    } finally {
      setBusy(false);
    }
  }

  const stackRoute = `/stacks/${encodeURIComponent(stack.id)}`;
  return (
    <section class="container-detail">
      <nav class="container-breadcrumbs" aria-label="Breadcrumb">
        <a
          href="#/"
          onClick={(event) => {
            event.preventDefault();
            navigate("/");
          }}
        >
          Overview
        </a>
        <span aria-hidden="true">/</span>
        <a
          href={`#${stackRoute}`}
          onClick={(event) => {
            event.preventDefault();
            navigate(stackRoute);
          }}
        >
          {stack.directoryName}
        </a>
        <span aria-hidden="true">/</span>
        <span>{container?.service || "Container"}</span>
      </nav>
      {readError && <Notice>{readError}</Notice>}
      {!containers && !readError && <p role="status">Loading container…</p>}
      {containers && !container && (
        <div class="detail-content">
          <h1>Container not found</h1>
          <p>Container not found in this stack.</p>
          <a
            href={`#${stackRoute}`}
            onClick={(event) => {
              event.preventDefault();
              navigate(stackRoute);
            }}
          >
            Back to {stack.directoryName}
          </a>
        </div>
      )}
      {container && !readError && (
        <>
          <div class="page-heading container-heading">
            <div>
              <p class="muted">{container.service || "Container"}</p>
              <h1>{container.name}</h1>
            </div>
            <div class="action-group">
              <button disabled={!canStart} onClick={() => void run("start")}>
                Start
              </button>
              <button disabled={!canRun} onClick={() => void run("restart")}>
                Restart
              </button>
              <button
                class="danger"
                disabled={!canRun}
                onClick={() => void run("stop")}
              >
                Stop
              </button>
            </div>
          </div>
          {error && <Notice>{error}</Notice>}
          <div class="detail-content">
            <h2>Overview</h2>
            <dl class="container-facts">
              <div>
                <dt>Container ID</dt>
                <dd>
                  <code>{container.id}</code>
                </dd>
              </div>
              <div>
                <dt>Name</dt>
                <dd>{container.name || "—"}</dd>
              </div>
              <div>
                <dt>Service</dt>
                <dd>{container.service || "—"}</dd>
              </div>
              <div>
                <dt>State</dt>
                <dd>
                  <Badge tone={state.tone} dot>
                    {state.label}
                  </Badge>{" "}
                  {container.state}
                </dd>
              </div>
              <div>
                <dt>Health</dt>
                <dd>{state.health || "—"}</dd>
              </div>
              <div>
                <dt>Image</dt>
                <dd>
                  <code>{container.image || "—"}</code>
                </dd>
              </div>
              <div>
                <dt>Networks</dt>
                <dd>
                  {container.networks.length
                    ? container.networks.join(", ")
                    : "—"}
                </dd>
              </div>
              <div>
                <dt>Ports</dt>
                <dd>
                  {container.ports.length
                    ? container.ports.map(formatContainerPort).join(", ")
                    : "—"}
                </dd>
              </div>
            </dl>
          </div>
        </>
      )}
    </section>
  );
}
