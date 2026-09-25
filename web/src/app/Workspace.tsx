import { useState } from "preact/hooks";
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
    refresh,
    stream,
    addOperation,
    addOperations,
  } = useWorkspaceData(logout);
  const [selectedOperation, setSelectedOperation] = useState<string>();
  const [busy, setBusy] = useState(false);
  const remoteEnabled = Boolean(repositoryStatus.managedRemote);
  function onAction(operation: Operation) {
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
  const operation = operations.find(
    (operation) => operation.id === selectedOperation,
  );
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
            close={() => setSelectedOperation(undefined)}
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
      {stream.gap && (
        <Notice>
          Stream interrupted; some output may be missing. Operation records have
          been refreshed.
        </Notice>
      )}
      <main>
        {loading ? (
          <Empty>Loading stacks…</Empty>
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
