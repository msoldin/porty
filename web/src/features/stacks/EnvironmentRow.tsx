import { useState } from "preact/hooks";
import { message } from "../../lib/http";
import { Notice } from "../../components/Feedback";
import { setEnvironmentValue, deleteEnvironmentValue } from "./api";

export function EnvironmentRow({
  name,
  stackId,
  reload,
}: {
  name: string;
  stackId: string;
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
            await setEnvironmentValue(stackId, name, value);
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
              await deleteEnvironmentValue(stackId, name);
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
