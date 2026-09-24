import { useLayoutEffect, useRef, useState } from "preact/hooks";
import { message } from "../../lib/http";
import { Notice } from "../../components/Feedback";
import {
  getEnvironmentValue,
  setEnvironmentValue,
  deleteEnvironmentValue,
} from "./api";

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
  const [revealed, setRevealed] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);
  const generation = useRef(0);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const conceal = () => {
    generation.current++;
    setRevealed(null);
    setLoading(false);
  };
  useLayoutEffect(() => {
    conceal();
    setError("");
    return () => {
      generation.current++;
    };
  }, [name, stackId]);
  const show = async () => {
    const request = ++generation.current;
    setError("");
    setLoading(true);
    try {
      const current = await getEnvironmentValue(stackId, name);
      if (request === generation.current) setRevealed(current);
    } catch (error) {
      if (request === generation.current) setError(message(error));
    } finally {
      if (request === generation.current) setLoading(false);
    }
  };
  return (
    <div class="environment-row">
      <div class="environment-current">
        <label>
          Saved Stack value · {name}
          <input
            aria-label={`Saved value for ${name}`}
            type={revealed === null ? "password" : "text"}
            readOnly
            value={revealed ?? ""}
          />
        </label>
        <button
          type="button"
          disabled={busy}
          aria-label={`${revealed !== null || loading ? "Hide" : "Show"} ${name}`}
          onClick={() => {
            if (revealed !== null || loading) conceal();
            else void show();
          }}
        >
          {revealed !== null || loading ? "Hide" : "Show"}
        </button>
        {loading && <span class="muted">Loading…</span>}
        {revealed === "" && <span class="muted">Empty value</span>}
      </div>
      <form
        onSubmit={async (event) => {
          event.preventDefault();
          conceal();
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
            conceal();
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
