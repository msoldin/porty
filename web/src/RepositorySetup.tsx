import { useState } from "preact/hooks";
import {
  inspectRepositoryRemote,
  message,
  setupRepository,
  type GitIdentity,
  type RemoteAuthenticationInput,
  type RemoteInspection,
  type RepositoryRemoteInput,
  type RepositorySetupMode,
  type RepositorySetupStatus,
} from "./api";
import { Icon, Notice } from "./ui";

const choices: Array<{
  mode: RepositorySetupMode;
  title: string;
  description: string;
}> = [
  {
    mode: "init",
    title: "Create local repository",
    description: "Start a new repository without a remote.",
  },
  {
    mode: "remote",
    title: "Use remote repository",
    description: "Inspect and import an HTTPS or SSH remote.",
  },
  {
    mode: "adopt",
    title: "Use mounted repository",
    description: "Adopt the worktree mounted for Porty.",
  },
];

export function RepositorySetup({
  status,
  onReady,
}: {
  status: RepositorySetupStatus;
  onReady: (status: RepositorySetupStatus) => void;
}) {
  const [mode, setMode] = useState<RepositorySetupMode | null>(null);
  const [branch, setBranch] = useState("main");
  const [author, setAuthor] = useState<GitIdentity>(status.defaultAuthor);
  const [remoteURL, setRemoteURL] = useState("");
  const [authType, setAuthType] = useState<"none" | "https" | "ssh">("none");
  const [username, setUsername] = useState("");
  const [secret, setSecret] = useState("");
  const [inspection, setInspection] = useState<RemoteInspection | null>(null);
  const [manageExistingRemote, setManageExistingRemote] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const remoteReady =
    remoteURL.length > 0 &&
    (authType !== "https" || (username.length > 0 && secret.length > 0)) &&
    (authType !== "ssh" || status.ssh.usable);

  function chooseMode(next: RepositorySetupMode) {
    setMode(next);
    setError("");
    setInspection(null);
    if (next === "adopt") {
      setBranch(status.branch || "");
      setAuthor(
        status.author.name && status.author.email
          ? status.author
          : status.defaultAuthor,
      );
    } else {
      setBranch("main");
      setAuthor(status.defaultAuthor);
    }
  }

  function authentication(): RemoteAuthenticationInput {
    if (authType === "https") return { type: "https", username, secret };
    return { type: authType };
  }

  function remote(): RepositoryRemoteInput {
    return { url: remoteURL, authentication: authentication() };
  }

  function invalidateInspection() {
    setInspection(null);
    setError("");
  }

  async function inspect() {
    setBusy(true);
    setError("");
    try {
      const result = await inspectRepositoryRemote({ remote: remote() });
      setInspection(result);
      setBranch(result.suggestedBranch);
    } catch (failure) {
      setInspection(null);
      setSecret("");
      setError(message(failure));
    } finally {
      setBusy(false);
    }
  }

  async function submit() {
    if (!mode) return;
    setBusy(true);
    setError("");
    try {
      const request = {
        mode,
        branch,
        author,
        manageExistingRemote: mode === "adopt" && manageExistingRemote,
        ...(mode === "remote" ? { remote: remote() } : {}),
      };
      const result = await setupRepository(request);
      onReady(result);
    } catch (failure) {
      setError(message(failure));
    } finally {
      setSecret("");
      setBusy(false);
    }
  }

  if (!mode) {
    return (
      <main class="setup-page">
        <div class="setup-heading">
          <div class="auth-brand">
            <Icon name="Stacks" /> Porty
          </div>
          <h1>Configure the stack repository</h1>
          <p>Choose how Porty should prepare its fixed repository location.</p>
        </div>
        <div class="setup-choices">
          {choices.map((choice) => {
            const availability = status.modes.find(
              (value) => value.mode === choice.mode,
            );
            return (
              <article class="setup-choice" key={choice.mode}>
                <h2>{choice.title}</h2>
                <p>{choice.description}</p>
                {availability?.reason && (
                  <p class="muted">{availability.reason}</p>
                )}
                <button
                  class="primary"
                  disabled={!availability?.available}
                  onClick={() => chooseMode(choice.mode)}
                >
                  {choice.title}
                </button>
              </article>
            );
          })}
        </div>
      </main>
    );
  }

  return (
    <main class="setup-page setup-form-page">
      <button class="text-button" onClick={() => setMode(null)}>
        ← Back to choices
      </button>
      <h1>{choices.find((choice) => choice.mode === mode)?.title}</h1>
      {error && <Notice>{error}</Notice>}

      {mode === "remote" && (
        <section class="setup-section">
          <h2>Remote access</h2>
          <label>
            Remote URL
            <input
              value={remoteURL}
              placeholder="https://example.com/team/repo.git"
              onInput={(event) => {
                setRemoteURL(event.currentTarget.value);
                invalidateInspection();
              }}
              required
            />
          </label>
          <fieldset class="choice-list">
            <legend>Authentication</legend>
            <label>
              <input
                type="radio"
                name="authentication"
                checked={authType === "none"}
                onChange={() => {
                  setAuthType("none");
                  setSecret("");
                  invalidateInspection();
                }}
              />
              No authentication
            </label>
            <label>
              <input
                type="radio"
                name="authentication"
                checked={authType === "https"}
                onChange={() => {
                  setAuthType("https");
                  invalidateInspection();
                }}
              />
              HTTPS username and secret
            </label>
            <label>
              <input
                type="radio"
                name="authentication"
                checked={authType === "ssh"}
                disabled={!status.ssh.usable}
                onChange={() => {
                  setAuthType("ssh");
                  setSecret("");
                  invalidateInspection();
                }}
              />
              Mounted SSH files
            </label>
          </fieldset>
          {authType === "https" && (
            <div class="setup-fields two-columns">
              <label>
                HTTPS username
                <input
                  value={username}
                  autocomplete="username"
                  onInput={(event) => {
                    setUsername(event.currentTarget.value);
                    invalidateInspection();
                  }}
                  required
                />
              </label>
              <label>
                HTTPS secret
                <input
                  value={secret}
                  type="password"
                  autocomplete="new-password"
                  onInput={(event) => {
                    setSecret(event.currentTarget.value);
                    invalidateInspection();
                  }}
                  required
                />
              </label>
            </div>
          )}
          {authType === "ssh" && (
            <p class="muted">
              {status.ssh.usable
                ? "SSH identity and known hosts are ready."
                : "The mounted SSH identity and known hosts are not usable."}
            </p>
          )}
          {!status.ssh.usable && authType !== "ssh" && (
            <p class="muted">
              Mounted SSH files are unavailable until both fixed files are safe.
            </p>
          )}
          <button
            type="button"
            disabled={busy || !remoteReady}
            onClick={inspect}
          >
            {busy ? "Inspecting…" : "Inspect remote"}
          </button>
        </section>
      )}

      <form
        class="setup-section setup-fields"
        onSubmit={(event) => {
          event.preventDefault();
          submit();
        }}
      >
        {mode === "adopt" ? (
          <div class="detected-facts">
            <span class="muted">Detected branch</span>
            <strong>{branch}</strong>
          </div>
        ) : mode === "remote" && inspection ? (
          <label>
            Branch
            {inspection.empty ? (
              <input
                type="text"
                value={branch}
                onInput={(event) => setBranch(event.currentTarget.value)}
                required
              />
            ) : (
              <select
                value={branch}
                onChange={(event) => setBranch(event.currentTarget.value)}
                required
              >
                {inspection.branches.map((value) => (
                  <option value={value}>{value}</option>
                ))}
              </select>
            )}
          </label>
        ) : mode === "init" ? (
          <label>
            Initial branch
            <input
              value={branch}
              onInput={(event) => setBranch(event.currentTarget.value)}
              required
            />
          </label>
        ) : null}

        {(mode !== "remote" || inspection) && (
          <div class="setup-fields two-columns">
            <label>
              Git author name
              <input
                value={author.name}
                onInput={(event) =>
                  setAuthor({ ...author, name: event.currentTarget.value })
                }
                required
              />
            </label>
            <label>
              Git author email
              <input
                type="email"
                value={author.email}
                onInput={(event) =>
                  setAuthor({ ...author, email: event.currentTarget.value })
                }
                required
              />
            </label>
          </div>
        )}

        {mode === "adopt" && status.existingRemote && (
          <fieldset class="choice-list">
            <legend>Existing origin ({status.existingRemote.url})</legend>
            <label>
              <input
                type="radio"
                name="existing-remote"
                checked={!manageExistingRemote}
                onChange={() => setManageExistingRemote(false)}
              />
              Keep local-only
            </label>
            <label>
              <input
                type="radio"
                name="existing-remote"
                checked={manageExistingRemote}
                onChange={() => setManageExistingRemote(true)}
              />
              Manage origin with Porty
            </label>
          </fieldset>
        )}

        <button
          class="primary"
          disabled={
            busy ||
            !branch ||
            !author.name ||
            !author.email ||
            (mode === "remote" && (!inspection || !remoteReady))
          }
        >
          {busy
            ? "Configuring…"
            : mode === "init"
              ? "Create repository"
              : mode === "remote"
                ? "Import repository"
                : "Use mounted repository"}
        </button>
      </form>
    </main>
  );
}
