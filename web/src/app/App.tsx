import { useEffect, useState } from "preact/hooks";
import { APIError, message, setCSRF } from "../lib/http";
import { getSession, getSetupStatus } from "../features/auth/api";
import type { Session } from "../features/auth/types";
import { getRepositorySetupStatus } from "../features/repository/api";
import type { RepositorySetupStatus } from "../features/repository/types";
import { Auth } from "../features/auth/Auth";
import { Notice } from "../components/Feedback";
import { Workspace } from "./Workspace";
import { RepositorySetup } from "../features/repository/RepositorySetup";
import { useHashRoute } from "./useHashRoute";

export function App() {
  const navigation = useHashRoute();
  const [session, setSession] = useState<Session | null>(null);
  const [registered, setRegistered] = useState(true);
  const [loading, setLoading] = useState(true);
  const [repositoryStatus, setRepositoryStatus] =
    useState<RepositorySetupStatus | null>(null);
  const [repositoryLoading, setRepositoryLoading] = useState(false);
  const [repositoryError, setRepositoryError] = useState("");
  const [repositoryAttempt, setRepositoryAttempt] = useState(0);
  const [error, setError] = useState("");
  let requiredRoute: string | undefined;
  if (!loading && !error) {
    if (!session) requiredRoute = registered ? "/login" : "/register";
    else if (repositoryStatus && repositoryStatus.state !== "ready")
      requiredRoute = "/setup";
    else if (
      repositoryStatus?.state === "ready" &&
      ["/register", "/login", "/setup"].includes(navigation.route)
    )
      requiredRoute = "/stacks";
  }
  useEffect(() => {
    if (requiredRoute && navigation.route !== requiredRoute)
      navigation.navigate(requiredRoute, true);
  }, [requiredRoute, navigation.route]);
  useEffect(() => {
    let active = true;
    async function load() {
      try {
        const value = await getSession();
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
          const setup = await getSetupStatus();
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
  useEffect(() => {
    if (!session) {
      setRepositoryStatus(null);
      setRepositoryLoading(false);
      return;
    }
    let active = true;
    setRepositoryLoading(true);
    setRepositoryError("");
    getRepositorySetupStatus()
      .then((status) => {
        if (active) setRepositoryStatus(status);
      })
      .catch((failure) => {
        if (!active) return;
        if (failure instanceof APIError && failure.status === 401) {
          setCSRF("");
          setSession(null);
        } else setRepositoryError(message(failure));
      })
      .finally(() => {
        if (active) setRepositoryLoading(false);
      });
    return () => {
      active = false;
    };
  }, [session, repositoryAttempt]);
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
  if (!session)
    return (
      <Auth
        registered={registered}
        onSession={(value) => {
          setRegistered(true);
          setSession(value);
        }}
      />
    );
  if (repositoryError)
    return (
      <main class="auth">
        <Notice>{repositoryError}</Notice>
        <button onClick={() => setRepositoryAttempt((value) => value + 1)}>
          Retry
        </button>
      </main>
    );
  if (repositoryLoading || !repositoryStatus)
    return (
      <main class="auth" role="status">
        Loading repository setup…
      </main>
    );
  if (repositoryStatus.state !== "ready")
    return (
      <main>
        <RepositorySetup
          status={repositoryStatus}
          onReady={setRepositoryStatus}
        />
      </main>
    );
  return (
    <Workspace
      session={session}
      navigation={navigation}
      repositoryStatus={repositoryStatus}
      onRepositoryChange={setRepositoryStatus}
      logout={() => {
        setCSRF("");
        setRegistered(true);
        setRepositoryStatus(null);
        setSession(null);
      }}
    />
  );
}
