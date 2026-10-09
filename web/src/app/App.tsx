import { useEffect, useState } from "preact/hooks";
import { APIError, message, setCSRF } from "../lib/http";
import { getSession, getSetupStatus } from "../features/auth/api";
import type { Session } from "../features/auth/types";
import { getRepositorySetupStatus } from "../features/repository/api";
import type { RepositorySetupStatus } from "../features/repository/types";
import { Auth } from "../features/auth/Auth";
import { Notice } from "../components/Feedback";
import { Workspace } from "./Workspace";

export function App() {
  const [session, setSession] = useState<Session | null>(null);
  const [registered, setRegistered] = useState(true);
  const [loading, setLoading] = useState(true);
  const [repositoryStatus, setRepositoryStatus] =
    useState<RepositorySetupStatus | null>(null);
  const [repositoryLoading, setRepositoryLoading] = useState(false);
  const [repositoryError, setRepositoryError] = useState("");
  const [repositoryAttempt, setRepositoryAttempt] = useState(0);
  const [error, setError] = useState("");
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
  return session ? (
    <Workspace
      session={session}
      repositoryStatus={repositoryStatus}
      repositoryLoading={
        repositoryLoading || (!repositoryStatus && !repositoryError)
      }
      repositoryError={repositoryError}
      retryRepository={() => setRepositoryAttempt((value) => value + 1)}
      onRepositoryChange={setRepositoryStatus}
      logout={() => {
        setCSRF("");
        setRegistered(true);
        setRepositoryStatus(null);
        setSession(null);
      }}
    />
  ) : (
    <Auth registered={registered} onSession={setSession} />
  );
}
