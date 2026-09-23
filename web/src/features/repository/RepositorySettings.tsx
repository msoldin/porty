import { useState } from "preact/hooks";
import { message } from "../../lib/http";
import {
  configureRepositoryRemote,
  inspectRepositoryRemote,
  removeRepositoryRemote,
} from "./api";
import {
  type RemoteAuthenticationInput,
  type RemoteInspection,
  type RepositoryAuthType,
  type RepositoryRemoteInput,
  type RepositorySetupStatus,
} from "./types";
import { RemoteAuthenticationFields } from "./RepositoryRemoteFields";
import { Notice } from "../../components/Feedback";

type Action = "configure" | "authentication" | null;

export function RepositorySettings({
  status,
  onChange,
}: {
  status: RepositorySetupStatus;
  onChange: (status: RepositorySetupStatus) => void;
}) {
  const [action, setAction] = useState<Action>(null);
  const [remoteURL, setRemoteURL] = useState("");
  const [authType, setAuthType] = useState<RepositoryAuthType>("none");
  const [username, setUsername] = useState("");
  const [secret, setSecret] = useState("");
  const [inspection, setInspection] = useState<RemoteInspection | null>(null);
  const [branch, setBranch] = useState(status.branch || "");
  const [replaceUnmanaged, setReplaceUnmanaged] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  const unmanagedRemote =
    !status.managedRemote && status.existingRemote?.managed === false;
  const remoteReady =
    remoteURL.length > 0 &&
    (authType !== "https" || (username.length > 0 && secret.length > 0)) &&
    (authType !== "ssh" || status.ssh.usable);

  function authentication(): RemoteAuthenticationInput {
    if (authType === "https") return { type: "https", username, secret };
    return { type: authType };
  }

  function remote(): RepositoryRemoteInput {
    return { url: remoteURL, authentication: authentication() };
  }

  function reset(next: Action) {
    setAction(next);
    setError("");
    setInspection(null);
    setBranch(status.branch || "");
    setReplaceUnmanaged(false);
    setUsername("");
    setSecret("");
    if (next === "authentication" && status.managedRemote) {
      setRemoteURL(status.managedRemote.url);
      setAuthType(status.managedRemote.authType);
    } else {
      setRemoteURL("");
      setAuthType("none");
    }
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
      setSecret("");
      setInspection(null);
      setError(message(failure));
    } finally {
      setBusy(false);
    }
  }

  async function save() {
    if (!action) return;
    setBusy(true);
    setError("");
    try {
      const next = await configureRepositoryRemote({
        remote: remote(),
        branch,
        replaceExisting: Boolean(status.managedRemote || unmanagedRemote),
      });
      onChange(next);
      setAction(null);
    } catch (failure) {
      setError(message(failure));
    } finally {
      setSecret("");
      setBusy(false);
    }
  }

  if (action) {
    const configuring = action === "configure";
    const saveLabel = configuring
      ? status.managedRemote
        ? "Replace remote"
        : unmanagedRemote
          ? "Replace existing origin"
          : "Add remote"
      : "Update authentication";
    return (
      <section class="repository-settings setup-section">
        <button class="text-button" onClick={() => setAction(null)}>
          ← Back to repository settings
        </button>
        <h2>{saveLabel}</h2>
        {error && <Notice>{error}</Notice>}
        {configuring ? (
          <label>
            Remote URL
            <input
              value={remoteURL}
              onInput={(event) => {
                setRemoteURL(event.currentTarget.value);
                invalidateInspection();
              }}
              required
            />
          </label>
        ) : (
          <p>
            Remote <strong>{status.managedRemote?.url}</strong>
          </p>
        )}
        <RemoteAuthenticationFields
          ssh={status.ssh}
          authType={authType}
          username={username}
          secret={secret}
          disabled={busy}
          onAuthType={(value) => {
            setAuthType(value);
            if (value !== "https") setSecret("");
            invalidateInspection();
          }}
          onUsername={(value) => {
            setUsername(value);
            invalidateInspection();
          }}
          onSecret={(value) => {
            setSecret(value);
            invalidateInspection();
          }}
        />
        {configuring && unmanagedRemote && (
          <label class="confirmation">
            <input
              type="checkbox"
              checked={replaceUnmanaged}
              onChange={(event) =>
                setReplaceUnmanaged(event.currentTarget.checked)
              }
            />
            Replace the existing unmanaged origin
          </label>
        )}
        {configuring && (
          <button
            type="button"
            disabled={
              busy || !remoteReady || (unmanagedRemote && !replaceUnmanaged)
            }
            onClick={inspect}
          >
            {busy ? "Inspecting…" : "Inspect remote"}
          </button>
        )}
        {configuring && inspection && (
          <label>
            Branch
            {inspection.empty ? (
              <input
                value={branch}
                onInput={(event) => setBranch(event.currentTarget.value)}
                required
              />
            ) : (
              <select
                value={branch}
                onChange={(event) => setBranch(event.currentTarget.value)}
              >
                {inspection.branches.map((value) => (
                  <option value={value}>{value}</option>
                ))}
              </select>
            )}
          </label>
        )}
        <button
          class="primary"
          disabled={
            busy ||
            !remoteReady ||
            !branch ||
            (configuring && !inspection) ||
            (configuring && unmanagedRemote && !replaceUnmanaged)
          }
          onClick={save}
        >
          {busy ? "Saving…" : saveLabel}
        </button>
      </section>
    );
  }

  return (
    <section class="repository-settings">
      <h2>Repository remote</h2>
      {error && <Notice>{error}</Notice>}
      {status.managedRemote ? (
        <>
          <p>
            Managed origin <strong>{status.managedRemote.url}</strong>
          </p>
          <div class="settings-actions">
            <button onClick={() => reset("configure")}>Replace remote</button>
            <button onClick={() => reset("authentication")}>
              Update authentication
            </button>
            <button
              class="danger"
              disabled={busy}
              onClick={async () => {
                if (
                  !confirm(
                    "Remove Porty's managed remote? Local commits, stacks, and history will remain.",
                  )
                )
                  return;
                setBusy(true);
                setError("");
                try {
                  onChange(await removeRepositoryRemote());
                } catch (failure) {
                  setError(message(failure));
                } finally {
                  setBusy(false);
                }
              }}
            >
              Remove remote
            </button>
          </div>
          <p class="muted">
            Removing the remote keeps all stacks, local commits, files, and
            history.
          </p>
        </>
      ) : (
        <>
          {unmanagedRemote ? (
            <p>
              Existing unmanaged origin{" "}
              <strong>{status.existingRemote?.url}</strong>
            </p>
          ) : (
            <p class="muted">
              Local commits and stack files remain usable without a remote.
            </p>
          )}
          <button onClick={() => reset("configure")}>
            {unmanagedRemote ? "Replace existing origin" : "Add remote"}
          </button>
        </>
      )}
    </section>
  );
}
