import { useEffect, useState } from "preact/hooks";
import { message } from "../../lib/http";
import {
  stackPath,
  listEnvironmentKeys,
  setEnvironmentValue,
  renameStack,
  deleteStack,
} from "./api";
import { EnvironmentRow } from "./EnvironmentRow";
import type { Stack } from "./types";
import { Notice } from "../../components/Feedback";

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
  const [newSecret, setNewSecret] = useState(false);
  const [name, setName] = useState(stack.directoryName);
  const reload = () =>
    listEnvironmentKeys(stack.id)
      .then(setKeys)
      .catch((error) => setError(message(error)));
  useEffect(() => {
    reload();
  }, [root]);
  return (
    <div class="settings-content">
      <h2>Environment</h2>
      <p class="muted">
        Saved values can be edited in place. Mark a value as secret to mask it
        by default.
      </p>
      {error && <Notice>{error}</Notice>}
      {keys.map((key) => (
        <EnvironmentRow
          key={key}
          name={key}
          stackId={stack.id}
          reload={reload}
        />
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
            await setEnvironmentValue(
              stack.id,
              String(data.get("key")),
              data.get("value"),
              data.has("secret"),
            );
            form.reset();
            setNewSecret(false);
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
          <input name="value" type={newSecret ? "password" : "text"} />
        </label>
        <label class="confirmation environment-secret-choice">
          <input
            name="secret"
            type="checkbox"
            checked={newSecret}
            onChange={(event) => setNewSecret(event.currentTarget.checked)}
          />
          Secret value for new key
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
            await renameStack(stack.id, name);
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
              await deleteStack(stack.id);
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
