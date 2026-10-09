import { Alerts } from "../features/alerts/Alerts";
import { useAlerts } from "../features/alerts/useAlerts";
import { getOperation } from "../features/operations/api";
import { useRef, useState } from "preact/hooks";
import { useHashRoute } from "./useHashRoute";
import { parseStackRoute, requiresRepository } from "./routes";
import { useWorkspaceData } from "./useWorkspaceData";
import { WorkspaceShell } from "./WorkspaceShell";
import { RepositoryPage } from "../features/repository/RepositoryPage";
import { ConfirmDialog } from "../components/ConfirmDialog";
import { Audit } from "../features/audit/Audit";
import { message } from "../lib/http";
import type { Session } from "../features/auth/types";
import type { RepositorySetupStatus } from "../features/repository/types";
import type { Operation } from "../features/operations/types";
import { signOut } from "../features/auth/api";
import { StackInventory } from "../features/stacks/StackInventory";
import { Dashboard } from "../features/dashboard/Dashboard";
import { RepositorySetup } from "../features/repository/RepositorySetup";
import { StackDetail } from "../features/stacks/StackDetail";
import { ContainerDetail } from "../features/stacks/ContainerDetail";
import { AccountSettings } from "../features/auth/AccountSettings";
import { Operations } from "../features/operations/Operations";
import { OperationDrawer } from "../features/operations/OperationDrawer";
import { Empty, Notice } from "../components/Feedback";

export function Workspace({
  session,
  repositoryStatus,
  repositoryLoading,
  repositoryError,
  retryRepository,
  onRepositoryChange,
  logout,
}: {
  session: Session;
  repositoryStatus: RepositorySetupStatus | null;
  repositoryLoading: boolean;
  repositoryError: string;
  retryRepository: () => void;
  onRepositoryChange: (status: RepositorySetupStatus) => void;
  logout: () => void;
}) {
  const {
    route,
    navigate,
    dirty,
    setDirty,
    pendingRoute,
    confirmNavigation,
    cancelNavigation,
  } = useHashRoute();
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
  } = useWorkspaceData(logout, repositoryStatus?.state === "ready");
  const alerts = useAlerts();
  const operationRequest = useRef(0);
  const [linkedOperation, setLinkedOperation] = useState<Operation>();
  const [selectedOperation, setSelectedOperation] = useState<string>();
  const [operationLoading, setOperationLoading] = useState(false);
  const [signOutPending, setSignOutPending] = useState(false);
  const [signOutBusy, setSignOutBusy] = useState(false);
  const [signOutError, setSignOutError] = useState("");
  const repositoryRoute = requiresRepository(route);
  const remoteEnabled = Boolean(repositoryStatus?.managedRemote);
  async function openOperation(id: string) {
    const request = ++operationRequest.current;
    setOperationLoading(true);
    setSelectedOperation(undefined);
    try {
      const item =
        operations.find((item) => item.id === id) || (await getOperation(id));
      if (request !== operationRequest.current) return;
      setLinkedOperation(item);
      addOperation(item);
      setSelectedOperation(item.id);
    } catch (cause) {
      if (request === operationRequest.current) setError(message(cause));
    } finally {
      if (request === operationRequest.current) setOperationLoading(false);
    }
  }
  function onAction(operation: Operation) {
    operationRequest.current++;
    setOperationLoading(false);
    addOperation(operation);
    setSelectedOperation(operation.id);
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
    setSignOutBusy(true);
    setSignOutError("");
    try {
      await signOut();
      logout();
    } catch (error) {
      setSignOutError(message(error));
    } finally {
      setSignOutBusy(false);
    }
  }
  return (
    <WorkspaceShell
      unacknowledgedAlerts={alerts.page?.unacknowledgedCount || 0}
      username={session.username}
      route={route}
      stackSelected={Boolean(selectedStack)}
      operationOpen={Boolean(operation) || operationLoading}
      connection={stream.connection}
      navigate={navigate}
      onSignOut={() => {
        if (dirty) setSignOutPending(true);
        else void signOutAction();
      }}
      drawer={
        (operation || operationLoading) && (
          <OperationDrawer
            operation={operation}
            close={() => {
              operationRequest.current++;
              setOperationLoading(false);
              setSelectedOperation(undefined);
            }}
          />
        )
      }
    >
      <ConfirmDialog
        open={!!pendingRoute}
        title="Discard unsaved edits?"
        description="Your unsaved editor changes will be lost when you leave this stack. Saved files are retained."
        confirmLabel="Discard and leave"
        destructive
        onCancel={cancelNavigation}
        onConfirm={confirmNavigation}
      />
      <ConfirmDialog
        open={signOutPending}
        title="Discard edits and sign out?"
        description="Unsaved editor changes will be lost. Saved files are retained."
        confirmLabel="Discard and sign out"
        destructive
        busy={signOutBusy}
        error={signOutError}
        onCancel={() => setSignOutPending(false)}
        onConfirm={() => void signOutAction()}
      />
      {signOutError && <Notice>{signOutError}</Notice>}
      {repositoryRoute && error && (
        <Notice>
          {error} <button onClick={refresh}>Retry</button>
        </Notice>
      )}
      {repositoryRoute &&
        Object.values(resources).some((resource) => resource.stale) && (
          <Notice role="status">
            Some information could not be refreshed. Previously loaded records
            may be out of date.
          </Notice>
        )}
      {repositoryRoute && stream.gap && (
        <Notice>
          Stream interrupted; some output may be missing. Operation records have
          been refreshed.
        </Notice>
      )}
      <main>
        {route === "/" ? (
          <Dashboard onUnauthorized={logout} />
        ) : route === "/settings" ? (
          <AccountSettings
            onLogout={logout}
            repositoryStatus={repositoryStatus}
            onRepositoryChange={onRepositoryChange}
          />
        ) : route === "/alerts" ? (
          <Alerts navigate={navigate} openOperation={openOperation} />
        ) : repositoryError ? (
          <Notice>
            {repositoryError}{" "}
            <button onClick={retryRepository}>Retry repository setup</button>
          </Notice>
        ) : repositoryLoading || !repositoryStatus ? (
          <Empty>Loading repository setup…</Empty>
        ) : repositoryStatus.state !== "ready" ? (
          <RepositorySetup
            status={repositoryStatus}
            onReady={onRepositoryChange}
          />
        ) : loading ? (
          <Empty>Loading stacks…</Empty>
        ) : route === "/audit" &&
          !resources.audit.loaded &&
          resources.audit.error ? (
          <Notice>
            Audit log could not be loaded.{" "}
            <button onClick={refresh}>Retry audit log</button>
          </Notice>
        ) : route === "/stacks" &&
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
            key={`${selectedStack.id}:${stackRoute.containerId}:${stackRoute.containerSection || "overview"}`}
            initialTab={
              stackRoute.containerSection === "logs" ? "Logs" : "Overview"
            }
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
        ) : route === "/stacks" ? (
          <StackInventory
            remoteEnabled={remoteEnabled}
            stacks={stacks}
            repo={configuredRepo}
            operations={operations}
            navigate={navigate}
            refresh={refresh}
            onOperationsAccepted={addOperations}
          />
        ) : route === "/repository" ? (
          <RepositoryPage
            repo={configuredRepo}
            commits={commits}
            remoteEnabled={remoteEnabled}
            dirty={dirty}
            onAccepted={onAction}
          />
        ) : route === "/operations" ? (
          <Operations
            operations={operations}
            open={(operation) => setSelectedOperation(operation.id)}
          />
        ) : route === "/audit" ? (
          <Audit events={audit} />
        ) : (
          <div class="detail-content">
            <h1>Stack not found</h1>
            <button onClick={() => navigate("/stacks")}>Back to stacks</button>
          </div>
        )}
      </main>
    </WorkspaceShell>
  );
}
