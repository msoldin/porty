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
  const [secret, setSecret] = useState(false);
  const [revealed, setRevealed] = useState(false);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const generation = useRef(0);

  useLayoutEffect(() => {
    const request = ++generation.current;
    setValue("");
    setSecret(false);
    setRevealed(false);
    setLoading(true);
    setError("");
    getEnvironmentValue(stackId, name)
      .then((current) => {
        if (request !== generation.current) return;
        setValue(current.value);
        setSecret(current.secret);
      })
      .catch((error) => {
        if (request === generation.current) setError(message(error));
      })
      .finally(() => {
        if (request === generation.current) setLoading(false);
      });
    return () => {
      generation.current++;
    };
  }, [name, stackId]);

  return (
    <div class="environment-row">
      <form
        onSubmit={async (event) => {
          event.preventDefault();
          const request = ++generation.current;
          setBusy(true);
          setError("");
          try {
            await setEnvironmentValue(stackId, name, value);
          } catch (error) {
            if (request === generation.current) setError(message(error));
          } finally {
            if (request === generation.current) setBusy(false);
          }
        }}
      >
        <label>
          {name}
          <input
            aria-label={`Value for ${name}`}
            type={secret && !revealed ? "password" : "text"}
            autoComplete={secret ? "off" : undefined}
            value={value}
            disabled={loading || busy}
            onInput={(event) => setValue(event.currentTarget.value)}
          />
        </label>
        {secret && !loading && (
          <button
            type="button"
            disabled={busy}
            aria-label={`${revealed ? "Hide" : "Show"} ${name}`}
            onClick={() => setRevealed((shown) => !shown)}
          >
            <svg
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              stroke-width="1.8"
              stroke-linecap="round"
              stroke-linejoin="round"
              aria-hidden="true"
            >
              <path d="M2 12s3.5-6 10-6 10 6 10 6-3.5 6-10 6S2 12 2 12Z" />
              <circle cx="12" cy="12" r="3" />
              {revealed && <path d="m4 20 16-16" />}
            </svg>{" "}
            {revealed ? "Hide" : "Show"}
          </button>
        )}
        <button disabled={loading || busy} aria-label={`Update ${name}`}>
          Update
        </button>
        <button
          type="button"
          disabled={loading || busy}
          aria-label={`Delete ${name}`}
          onClick={async () => {
            if (!confirm(`Delete environment value ${name}?`)) return;
            const request = ++generation.current;
            setBusy(true);
            setError("");
            try {
              await deleteEnvironmentValue(stackId, name);
              if (request === generation.current) reload();
            } catch (error) {
              if (request === generation.current) setError(message(error));
            } finally {
              if (request === generation.current) setBusy(false);
            }
          }}
        >
          Delete
        </button>
      </form>
      {loading && <span class="muted">Loading value…</span>}
      {error && <Notice>{error}</Notice>}
    </div>
  );
}
