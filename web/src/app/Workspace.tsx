import { Alerts } from "../features/alerts/Alerts";
import { useAlerts } from "../features/alerts/useAlerts";
import { getOperation } from "../features/operations/api";
import { useRef, useState } from "preact/hooks";
import { useHashRoute } from "./useHashRoute";
import { parseStackRoute } from "./routes";
import { useWorkspaceData } from "./useWorkspaceData";
import { WorkspaceShell } from "./WorkspaceShell";
import { RepositoryHeader } from "../features/repository/RepositoryHeader";
import { RepositoryHistory } from "../features/repository/RepositoryHistory";
import { Audit } from "../features/audit/Audit";
import { message } from "../lib/http";
import type { Session } from "../features/auth/types";
import type { RepositorySetupStatus } from "../features/repository/types";
import type { Operation } from "../features/operations/types";
import { runRepositoryAction } from "../features/repository/api";
import { signOut } from "../features/auth/api";
import { Dashboard } from "../features/stacks/Dashboard";
import { StackDetail } from "../features/stacks/StackDetail";
import { ContainerDetail } from "../features/stacks/ContainerDetail";
import { AccountSettings } from "../features/auth/AccountSettings";
import { Operations } from "../features/operations/Operations";
import { OperationDrawer } from "../features/operations/OperationDrawer";
import { Empty, Notice } from "../components/Feedback";

export function Workspace({
  session,
  repositoryStatus,
  onRepositoryChange,
  logout,
}: {
  session: Session;
  repositoryStatus: RepositorySetupStatus;
  onRepositoryChange: (status: RepositorySetupStatus) => void;
  logout: () => void;
}) {
  const { route, navigate, dirty, setDirty } = useHashRoute();
  const {
    stacks,
    repo,
    commits,
    operations,
    audit,
    error,
    setError,
    loading,
    resources,
    refresh,
    stream,
    addOperation,
    addOperations,
  } = useWorkspaceData(logout);
  const alerts = useAlerts();
  const operationRequest = useRef(0);
  const [linkedOperation, setLinkedOperation] = useState<Operation>();
  const [selectedOperation, setSelectedOperation] = useState<string>();
  const [busy, setBusy] = useState(false);
  const remoteEnabled = Boolean(repositoryStatus.managedRemote);
  async function openOperation(id: string) {
    const request = ++operationRequest.current;
    try {
      const item =
        operations.find((item) => item.id === id) || (await getOperation(id));
      if (request !== operationRequest.current) return;
      setLinkedOperation(item);
      addOperation(item);
      setSelectedOperation(item.id);
    } catch (cause) {
      if (request === operationRequest.current) setError(message(cause));
    }
  }
  function onAction(operation: Operation) {
    operationRequest.current++;
    addOperation(operation);
    setSelectedOperation(operation.id);
  }
  async function repoAction(action: string) {
    if (dirty) {
      setError(
        "Save or discard editor changes before changing the repository.",
      );
      return;
    }
    if (action === "push" && !confirm("Push committed changes to origin?"))
      return;
    setBusy(true);
    setError("");
    try {
      onAction(await runRepositoryAction(action));
    } catch (error) {
      setError(message(error));
    } finally {
      setBusy(false);
    }
  }
  const stackRoute = parseStackRoute(route);
  const selectedStack = stacks.find(
    (stack) => stack.id === stackRoute?.stackId,
  );
  const operation =
    operations.find((operation) => operation.id === selectedOperation) ||
    (linkedOperation?.id === selectedOperation ? linkedOperation : undefined);
  const configuredRepo = repo?.configured ? repo : null;
  async function signOutAction(): Promise<void> {
    if (dirty && !confirm("Discard unsaved changes and sign out?")) return;
    try {
      await signOut();
      logout();
    } catch (error) {
      setError(message(error));
    }
  }
  return (
    <WorkspaceShell
      unacknowledgedAlerts={alerts.page?.unacknowledgedCount || 0}
      username={session.username}
      route={route}
      stackSelected={Boolean(selectedStack)}
      operationOpen={Boolean(operation)}
      connection={stream.connection}
      navigate={navigate}
      onSignOut={signOutAction}
      drawer={
        operation && (
          <OperationDrawer
            operation={operation}
            close={() => {
              operationRequest.current++;
              setSelectedOperation(undefined);
            }}
          />
        )
      }
    >
      <RepositoryHeader
        repo={repo}
        commits={commits}
        remoteEnabled={remoteEnabled}
        busy={busy}
        onAction={repoAction}
      />
      {error && (
        <Notice>
          {error} <button onClick={refresh}>Retry</button>
        </Notice>
      )}
      {Object.values(resources).some((resource) => resource.stale) && (
        <Notice role="status">
          Some information could not be refreshed. Previously loaded records may
          be out of date.
        </Notice>
      )}
      {stream.gap && (
        <Notice>
          Stream interrupted; some output may be missing. Operation records have
          been refreshed.
        </Notice>
      )}
      <main>
        {route === "/alerts" ? (
          <Alerts navigate={navigate} openOperation={openOperation} />
        ) : loading ? (
          <Empty>Loading stacks…</Empty>
        ) : route === "/audit" &&
          !resources.audit.loaded &&
          resources.audit.error ? (
          <Notice>
            Audit log could not be loaded.{" "}
            <button onClick={refresh}>Retry audit log</button>
          </Notice>
        ) : route === "/" &&
          !resources.stacks.loaded &&
          resources.stacks.error ? (
          <Notice>
            Stacks could not be loaded.{" "}
            <button onClick={refresh}>Retry stacks</button>
          </Notice>
        ) : route === "/operations" &&
          !resources.operations.loaded &&
          resources.operations.error ? (
          <Notice>
            Operations could not be loaded.{" "}
            <button onClick={refresh}>Retry operations</button>
          </Notice>
        ) : selectedStack && stackRoute?.containerId ? (
          <ContainerDetail
            key={`${selectedStack.id}:${stackRoute.containerId}`}
            stack={selectedStack}
            containerId={stackRoute.containerId}
            operations={operations}
            dirty={dirty}
            navigate={navigate}
            onAction={onAction}
          />
        ) : selectedStack ? (
          <StackDetail
            openOperation={openOperation}
            key={selectedStack.id}
            stack={selectedStack}
            repo={configuredRepo}
            operations={operations}
            dirty={dirty}
            setDirty={setDirty}
            navigate={navigate}
            refresh={refresh}
            onAction={onAction}
          />
        ) : route === "/" ? (
          <Dashboard
            stacks={stacks}
            repo={configuredRepo}
            operations={operations}
            navigate={navigate}
            refresh={refresh}
            onOperationsAccepted={addOperations}
          />
        ) : route === "/repository" ? (
          <RepositoryHistory repo={configuredRepo} commits={commits} />
        ) : route === "/operations" ? (
          <Operations
            operations={operations}
            open={(operation) => setSelectedOperation(operation.id)}
          />
        ) : route === "/settings" ? (
          <AccountSettings
            onLogout={logout}
            repositoryStatus={repositoryStatus}
            onRepositoryChange={onRepositoryChange}
          />
        ) : route === "/audit" ? (
          <Audit events={audit} />
        ) : (
          <div class="detail-content">
            <h1>Stack not found</h1>
            <button onClick={() => navigate("/")}>Back to stacks</button>
          </div>
        )}
      </main>
    </WorkspaceShell>
  );
}
