import { useEffect, useState } from "preact/hooks";
import {
  api,
  message,
  stackPath,
  type Stack,
  type Repository,
  type Operation,
  type Deployment,
} from "./api";
import { Badge, Empty, Icon, Notice, isModified, remoteState } from "./ui";
import { Editor } from "./Editor";
import { StackSettings } from "./Settings";

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
  const [logOutput, setLogOutput] = useState("");
  const [logGap, setLogGap] = useState(false);
  const active = operations.find(
    (operation) =>
      operation.scopeId === stack.id &&
      ["running", "queued"].includes(operation.status),
  );
  const logs = operations.find(
    (operation) => operation.scopeId === stack.id && operation.kind === "logs",
  );
  const status = operations.find(
    (operation) =>
      operation.scopeId === stack.id && operation.kind === "status",
  );
  useEffect(() => {
    if (tab === "History" || tab === "Overview")
      api<Deployment[] | null>(`${stackPath(stack.id)}/deployments?limit=50`)
        .then((value) => setDeployments(value || []))
        .catch((error) => setError(message(error)));
  }, [stack.id, tab, operations]);
  useEffect(() => {
    if (tab !== "Logs") return;
    const socket = new WebSocket(
      `${location.protocol === "https:" ? "wss" : "ws"}://${location.host}/api/v1/stream`,
    );
    socket.onopen = () =>
      socket.send(
        JSON.stringify({
          type: "subscribe",
          subscriptionId: "logs",
          topic: `logs:${stack.id}`,
          since: 0,
        }),
      );
    socket.onmessage = (event) => {
      try {
        const value = JSON.parse(event.data);
        if (value.subscriptionId !== "logs") return;
        if (value.type === "gap") setLogGap(true);
        else if (
          value.type === "log" &&
          typeof value.payload?.output === "string"
        )
          setLogOutput(value.payload.output.slice(-65536));
      } catch {
        setLogGap(true);
      }
    };
    return () => socket.close();
  }, [stack.id, tab]);
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
      onAction(
        await api<Operation>(`${stackPath(stack.id)}/actions/${kind}`, "POST"),
      );
    } catch (error) {
      setError(message(error));
    } finally {
      setBusy(false);
    }
  }
  return (
    <section class="stack-detail">
      <div class="stack-top">
        <a
          href="#/"
          aria-label="All stacks"
          onClick={(event) => {
            event.preventDefault();
            navigate("/");
          }}
        >
          Stacks
        </a>
        <span class="muted"> / {stack.directoryName}</span>
        <div class="page-heading">
          <h1>{stack.directoryName}</h1>
          <div class="action-group">
            {["stop", "restart", "validate", "deploy"].map((kind) => (
              <button
                disabled={busy || !!active || !!stack.archivedAt}
                class={
                  kind === "deploy"
                    ? "primary"
                    : kind === "stop"
                      ? "danger"
                      : "accent"
                }
                onClick={() => action(kind)}
              >
                <Icon
                  name={
                    kind === "stop"
                      ? "Stop"
                      : kind === "restart"
                        ? "Refresh"
                        : kind === "validate"
                          ? "Check"
                          : "Play"
                  }
                />
                {kind[0].toUpperCase() + kind.slice(1)}
              </button>
            ))}
          </div>
        </div>
        <div class="state-strip">
          <div>
            <span class="runtime">
              <i />
              {stack.state?.runtime || "Unknown"}
            </span>
            <small>Refresh status to inspect runtime</small>
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
            <Badge tone="blue">{remoteState(repo)}</Badge>
            <small>Repository remote</small>
          </div>
          <div>
            <Badge>
              {stack.state?.freshness?.replaceAll("_", " ") ||
                (active ? `${active.kind}…` : "Unverified")}
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
          <h2>Runtime</h2>
          <p class="muted">
            Runtime is inspected on demand. Status output is a snapshot.
          </p>
          <div class="action-group">
            {["status", "start", "pull", "recreate"].map((kind) => (
              <button disabled={busy || !!active} onClick={() => action(kind)}>
                {kind === "status"
                  ? "Refresh status"
                  : kind === "pull"
                    ? "Pull images"
                    : kind === "recreate"
                      ? "Recreate containers"
                      : "Start stack"}
              </button>
            ))}
          </div>
          {status && <pre class="output">{status.output || status.status}</pre>}
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
          <div class="page-heading">
            <h2>Container logs</h2>
            <button disabled={busy || !!active} onClick={() => action("logs")}>
              <Icon name="Refresh" />
              Load logs
            </button>
          </div>
          <p class="muted">Latest 500 lines. Reload to refresh the snapshot.</p>
          <pre class="output">
            {logOutput ||
              (logs
                ? logs.status
                : "Load logs to inspect recent container output.")}
          </pre>
          {logGap && <Notice>Some log output was missed. Reload logs.</Notice>}
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
