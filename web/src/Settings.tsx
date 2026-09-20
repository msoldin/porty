import { useEffect, useState } from "preact/hooks";
import { api, message, stackPath, type Stack } from "./api";
import { Notice } from "./ui";

function EnvironmentRow({
  name,
  root,
  reload,
}: {
  name: string;
  root: string;
  reload: () => void;
}) {
  const [value, setValue] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  return (
    <div class="environment-row">
      <form
        onSubmit={async (event) => {
          event.preventDefault();
          setBusy(true);
          setError("");
          try {
            await api(
              `${root}/environment/${encodeURIComponent(name)}`,
              "PUT",
              { value },
            );
            setValue("");
          } catch (error) {
            setError(message(error));
          } finally {
            setBusy(false);
          }
        }}
      >
        <label>
          {name}
          <input
            type="password"
            aria-label={`New value for ${name}`}
            placeholder="Value stored · enter replacement"
            autoComplete="new-password"
            value={value}
            onInput={(event) => setValue(event.currentTarget.value)}
          />
        </label>
        <button disabled={busy} aria-label={`Update ${name}`}>
          Update
        </button>
        <button
          type="button"
          disabled={busy}
          aria-label={`Delete ${name}`}
          onClick={async () => {
            if (!confirm(`Delete environment value ${name}?`)) return;
            setBusy(true);
            setError("");
            try {
              await api(
                `${root}/environment/${encodeURIComponent(name)}`,
                "DELETE",
              );
              reload();
            } catch (error) {
              setError(message(error));
            } finally {
              setBusy(false);
            }
          }}
        >
          Delete
        </button>
      </form>
      {error && <Notice>{error}</Notice>}
    </div>
  );
}
export function StackSettings({
  stack,
  onChanged,
}: {
  stack: Stack;
  onChanged: () => void;
}) {
  const root = stackPath(stack.id);
  const [keys, setKeys] = useState<string[]>([]);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [name, setName] = useState(stack.directoryName);
  const reload = () =>
    api<{ keys: string[] | null }>(`${root}/environment`)
      .then((value) => setKeys(value.keys || []))
      .catch((error) => setError(message(error)));
  useEffect(() => {
    reload();
  }, [root]);
  return (
    <div class="settings-content">
      <h2>Environment</h2>
      <p class="muted">
        Values are write-only. Existing values are never sent to your browser.
      </p>
      {error && <Notice>{error}</Notice>}
      {keys.map((key) => (
        <EnvironmentRow key={key} name={key} root={root} reload={reload} />
      ))}
      <form
        class="inline-form"
        onSubmit={async (event) => {
          event.preventDefault();
          const form = event.currentTarget;
          const data = new FormData(form);
          setBusy(true);
          setError("");
          try {
            await api(
              `${root}/environment/${encodeURIComponent(String(data.get("key")))}`,
              "PUT",
              { value: data.get("value") },
            );
            form.reset();
            await reload();
          } catch (error) {
            setError(message(error));
          } finally {
            setBusy(false);
          }
        }}
      >
        <label>
          New key
          <input
            name="key"
            pattern="[A-Za-z_][A-Za-z0-9_]*"
            required
            placeholder="API_TOKEN"
          />
        </label>
        <label>
          New value
          <input
            name="value"
            type="password"
            autoComplete="new-password"
            required
          />
        </label>
        <button disabled={busy}>Add value</button>
      </form>
      <h2>Stack settings</h2>
      <form
        class="inline-form"
        onSubmit={async (event) => {
          event.preventDefault();
          setBusy(true);
          setError("");
          try {
            await api(root, "PATCH", { name });
            onChanged();
          } catch (error) {
            setError(message(error));
          } finally {
            setBusy(false);
          }
        }}
      >
        <label>
          Stack name
          <input
            value={name}
            onInput={(event) => setName(event.currentTarget.value)}
            required
          />
        </label>
        <button disabled={busy}>Rename stack</button>
      </form>
      <section class="danger-zone">
        <h3>Delete stack</h3>
        <p>
          This stops the stack, removes its files, and archives its metadata.
          Docker volumes are retained.
        </p>
        <button
          class="danger"
          disabled={busy}
          onClick={async () => {
            if (
              !confirm(
                `Stop and delete ${stack.directoryName}? Files will be removed. Volumes will be retained.`,
              )
            )
              return;
            setBusy(true);
            setError("");
            try {
              await api(root, "DELETE");
              onChanged();
            } catch (error) {
              setError(message(error));
            } finally {
              setBusy(false);
            }
          }}
        >
          Delete stack
        </button>
      </section>
    </div>
  );
}
export function AccountSettings({ onLogout }: { onLogout: () => void }) {
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  return (
    <div class="settings-content">
      <h1>Settings</h1>
      <h2>Change password</h2>
      <p class="muted">Changing your password signs out every session.</p>
      {error && <Notice>{error}</Notice>}
      <form
        class="password-form"
        onSubmit={async (event) => {
          event.preventDefault();
          const data = new FormData(event.currentTarget);
          setBusy(true);
          setError("");
          try {
            await api("/session/password", "PUT", {
              CurrentPassword: data.get("current"),
              NewPassword: data.get("next"),
            });
            onLogout();
          } catch (error) {
            setError(message(error));
          } finally {
            setBusy(false);
          }
        }}
      >
        <label>
          Current password
          <input
            name="current"
            type="password"
            autoComplete="current-password"
            required
          />
        </label>
        <label>
          New password
          <input
            name="next"
            type="password"
            autoComplete="new-password"
            minLength={12}
            required
          />
        </label>
        <button disabled={busy} class="primary">
          Change password
        </button>
      </form>
    </div>
  );
}
