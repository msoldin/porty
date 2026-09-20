import { useEffect, useRef, useState } from "preact/hooks";
import {
  api,
  APIError,
  message,
  setCSRF,
  type Session,
  type Stack,
  type Repository,
  type Operation,
  type Commit,
  type AuditEvent,
  type StackState,
} from "./api";
import { Auth } from "./Auth";
import { Dashboard } from "./Dashboard";
import { StackDetail } from "./StackDetail";
import { AccountSettings } from "./Settings";
import { OperationDrawer, Operations } from "./Operations";
import { Empty, Icon, Notice } from "./ui";
import { useOperationStream } from "./stream";

export function App() {
  const [session, setSession] = useState<Session | null>(null);
  const [registered, setRegistered] = useState(true);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  useEffect(() => {
    let active = true;
    async function load() {
      try {
        const value = await api<Session>("/session");
        if (active) {
          setCSRF(value.csrfToken);
          setSession(value);
        }
      } catch (error) {
        if (!(error instanceof APIError) || error.status !== 401) {
          if (active) setError(message(error));
          return;
        }
        try {
          const setup = await api<{ registered: boolean; csrfToken: string }>(
            "/setup/status",
          );
          if (active) {
            setRegistered(setup.registered);
            setCSRF(setup.csrfToken);
          }
        } catch (error) {
          if (active) setError(message(error));
        }
      } finally {
        if (active) setLoading(false);
      }
    }
    load();
    return () => {
      active = false;
    };
  }, []);
  if (loading)
    return (
      <main class="auth" role="status">
        Loading Porty…
      </main>
    );
  if (error)
    return (
      <main class="auth">
        <h1>Porty</h1>
        <Notice>{error}</Notice>
        <button onClick={() => location.reload()}>Retry</button>
      </main>
    );
  return session ? (
    <Workspace
      session={session}
      logout={() => {
        setCSRF("");
        setRegistered(true);
        setSession(null);
      }}
    />
  ) : (
    <Auth registered={registered} onSession={setSession} />
  );
}

function Workspace({
  session,
  logout,
}: {
  session: Session;
  logout: () => void;
}) {
  const [stacks, setStacks] = useState<Stack[]>([]);
  const [repo, setRepo] = useState<Repository | null>(null);
  const [commits, setCommits] = useState<Commit[]>([]);
  const [operations, setOperations] = useState<Operation[]>([]);
  const [audit, setAudit] = useState<AuditEvent[]>([]);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);
  const [route, setRoute] = useState(location.hash.slice(1) || "/");
  const [dirty, setDirty] = useState(false);
  const dirtyRef = useRef(dirty);
  dirtyRef.current = dirty;
  const routeRef = useRef(route);
  routeRef.current = route;
  const [selectedOperation, setSelectedOperation] = useState<string>();
  const [busy, setBusy] = useState(false);
  async function refresh() {
    const result = await Promise.allSettled([
      api<Stack[] | null>("/stacks").then(async (items) =>
        Promise.all(
          (items || []).map(async (stack) => {
            try {
              return {
                ...stack,
                state: await api<StackState>(
                  `/stacks/${encodeURIComponent(stack.id)}/state`,
                ),
              };
            } catch {
              return stack;
            }
          }),
        ),
      ),
      api<Repository>("/repository/status"),
      api<Commit[] | null>("/repository/history?limit=50"),
      api<Operation[] | null>("/operations?limit=50"),
      api<AuditEvent[] | null>("/audit?limit=50"),
    ]);
    if (
      result.some(
        (value) =>
          value.status === "rejected" &&
          value.reason instanceof APIError &&
          value.reason.status === 401,
      )
    ) {
      logout();
      return;
    }
    if (result[0].status === "fulfilled") setStacks(result[0].value || []);
    if (result[1].status === "fulfilled") setRepo(result[1].value);
    if (result[2].status === "fulfilled") setCommits(result[2].value || []);
    if (result[3].status === "fulfilled") setOperations(result[3].value || []);
    if (result[4].status === "fulfilled") setAudit(result[4].value || []);
    const failed = result
      .slice(0, 4)
      .find((value) => value.status === "rejected");
    setError(failed?.status === "rejected" ? message(failed.reason) : "");
    setLoading(false);
  }
  useEffect(() => {
    refresh();
  }, []);
  const stream = useOperationStream((operation) => {
    setOperations((values) =>
      [operation, ...values.filter((value) => value.id !== operation.id)].slice(
        0,
        50,
      ),
    );
    if (["succeeded", "failed", "cancelled"].includes(operation.status))
      refresh();
  }, refresh);
  useEffect(() => {
    function hashChanged() {
      const next = location.hash.slice(1) || "/";
      if (next === routeRef.current) return;
      if (dirtyRef.current && !confirm("Discard unsaved changes?")) {
        history.replaceState(null, "", `#${routeRef.current}`);
        return;
      }
      setDirty(false);
      setRoute(next);
    }
    function beforeUnload(event: BeforeUnloadEvent) {
      if (dirtyRef.current) {
        event.preventDefault();
        event.returnValue = "";
      }
    }
    window.addEventListener("hashchange", hashChanged);
    window.addEventListener("beforeunload", beforeUnload);
    return () => {
      window.removeEventListener("hashchange", hashChanged);
      window.removeEventListener("beforeunload", beforeUnload);
    };
  }, []);
  function navigate(path: string) {
    if (dirty && !confirm("Discard unsaved changes?")) return;
    setDirty(false);
    setRoute(path);
    routeRef.current = path;
    location.hash = path;
  }
  function onAction(operation: Operation) {
    setOperations((values) => [
      operation,
      ...values.filter((value) => value.id !== operation.id),
    ]);
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
      onAction(await api<Operation>(`/repository/actions/${action}`, "POST"));
    } catch (error) {
      setError(message(error));
    } finally {
      setBusy(false);
    }
  }
  let id = "";
  try {
    id = route.startsWith("/stacks/") ? decodeURIComponent(route.slice(8)) : "";
  } catch {
    /* unknown route */
  }
  const selectedStack = stacks.find((stack) => stack.id === id);
  const operation = operations.find(
    (operation) => operation.id === selectedOperation,
  );
  const nav = [
    { name: "Stacks", path: "/" },
    { name: "Repository", path: "/repository" },
    { name: "Operations", path: "/operations" },
    { name: "Audit", path: "/audit" },
    { name: "Settings", path: "/settings" },
  ];
  return (
    <div class={`app-shell ${operation ? "with-drawer" : ""}`}>
      <aside class="sidebar">
        <a
          class="brand"
          href="#/"
          onClick={(event) => {
            event.preventDefault();
            navigate("/");
          }}
        >
          <Icon name="Stacks" />
          <span>Porty</span>
        </a>
        <p class="tagline">
          Docker Compose
          <br />
          made simple
        </p>
        <nav aria-label="Main navigation">
          {nav.map((item) => (
            <a
              href={`#${item.path}`}
              class={
                route === item.path || (item.path === "/" && !!id)
                  ? "active"
                  : ""
              }
              onClick={(event) => {
                event.preventDefault();
                navigate(item.path);
              }}
            >
              <Icon name={item.name} />
              {item.name}
            </a>
          ))}
        </nav>
        <div class="server-info">
          <span class="muted">Server</span>
          <strong>{location.hostname}</strong>
          <span class="connection">{stream.connection}</span>
          <hr />
          <span>{session.username}</span>
          <button
            class="text-button"
            onClick={async () => {
              if (dirty && !confirm("Discard unsaved changes and sign out?"))
                return;
              try {
                await api("/session", "DELETE");
                logout();
              } catch (error) {
                setError(message(error));
              }
            }}
          >
            Sign out
          </button>
        </div>
      </aside>
      <div class={`workspace ${selectedStack ? "stack-workspace" : ""}`}>
        <header class="repository-header">
          <div>
            <strong>Repository</strong>
            <span>
              <Icon name="Repository" />
              {repo?.branch || "Not configured"}
            </span>
          </div>
          <div>
            <strong>Commit</strong>
            <code>{commits[0]?.sha.slice(0, 7) || "—"}</code>
          </div>
          <div class="last-fetch">
            <strong>Last fetched</strong>
            <span>Unavailable</span>
          </div>
          <div class="repository-actions">
            <button disabled title="Fetch is not exposed by this server">
              <Icon name="Refresh" />
              Fetch
            </button>
            <button disabled={busy || !repo} onClick={() => repoAction("pull")}>
              <Icon name="Pull" />
              Pull
            </button>
            <button
              disabled={busy || !repo || repo.ahead === 0}
              onClick={() => repoAction("push")}
            >
              <Icon name="Push" />
              Push
            </button>
          </div>
        </header>
        {error && (
          <Notice>
            {error} <button onClick={refresh}>Retry</button>
          </Notice>
        )}
        {stream.gap && (
          <Notice>
            Stream interrupted; some output may be missing. Operation records
            have been refreshed.
          </Notice>
        )}
        <main>
          {loading ? (
            <Empty>Loading stacks…</Empty>
          ) : selectedStack ? (
            <StackDetail
              key={selectedStack.id}
              stack={selectedStack}
              repo={repo}
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
              repo={repo}
              operations={operations}
              navigate={navigate}
              refresh={refresh}
            />
          ) : route === "/repository" ? (
            <div class="detail-content">
              <h1>Repository</h1>
              <p class="muted">
                Branch {repo?.branch || "not configured"} · remote origin
              </p>
              <h2>History</h2>
              {commits.map((commit) => (
                <article class="commit-row" key={commit.sha}>
                  <code>{commit.sha.slice(0, 7)}</code>
                  <div>
                    <strong>{commit.subject}</strong>
                    <p class="muted">
                      {commit.author} · {commit.time}
                    </p>
                  </div>
                </article>
              ))}
              {!commits.length && <Empty>No commits available.</Empty>}
              {!repo && <RepositorySetup refresh={refresh} />}
            </div>
          ) : route === "/operations" ? (
            <Operations
              operations={operations}
              open={(operation) => setSelectedOperation(operation.id)}
            />
          ) : route === "/settings" ? (
            <AccountSettings onLogout={logout} />
          ) : route === "/audit" ? (
            <div class="detail-content">
              <h1>Audit</h1>
              {audit.map((event) => (
                <article class="commit-row" key={event.id}>
                  <code>{event.outcome}</code>
                  <div>
                    <strong>{event.action}</strong>
                    <p class="muted">
                      {event.targetId || event.targetType} ·{" "}
                      {new Date(event.occurredAt).toLocaleString()}
                    </p>
                  </div>
                </article>
              ))}
              {!audit.length && <Empty>No audit events recorded.</Empty>}
            </div>
          ) : (
            <div class="detail-content">
              <h1>Stack not found</h1>
              <button onClick={() => navigate("/")}>Back to stacks</button>
            </div>
          )}
        </main>
      </div>
      {operation && (
        <OperationDrawer
          operation={operation}
          close={() => setSelectedOperation(undefined)}
        />
      )}
    </div>
  );
}

function RepositorySetup({ refresh }: { refresh: () => void }) {
  const [error, setError] = useState("");
  return (
    <form
      class="inline-form"
      onSubmit={async (event) => {
        event.preventDefault();
        const data = new FormData(event.currentTarget);
        setError("");
        try {
          await api("/repository/setup", "POST", {
            mode: data.get("mode"),
            remote: data.get("remote"),
            branch: data.get("branch"),
            username: data.get("username"),
            secret: data.get("secret"),
          });
          refresh();
        } catch (failure) {
          setError(message(failure));
        }
      }}
    >
      <h2>Configure repository</h2>
      {error && <Notice>{error}</Notice>}
      <label>
        Mode
        <select name="mode">
          <option value="init">Initialize</option>
          <option value="clone">Clone</option>
          <option value="adopt">Adopt existing</option>
        </select>
      </label>
      <label>
        Branch
        <input name="branch" defaultValue="main" required />
      </label>
      <label>
        Remote URL
        <input name="remote" placeholder="https://example.com/team/repo.git" />
      </label>
      <label>
        HTTPS username
        <input name="username" autocomplete="username" />
      </label>
      <label>
        HTTPS secret
        <input name="secret" type="password" autocomplete="new-password" />
      </label>
      <button class="primary">Save repository</button>
    </form>
  );
}
