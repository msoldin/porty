import { useState } from "preact/hooks";
import { message } from "../../lib/http";
import { signIn } from "./api";
import { type Session } from "./types";
import { Icon } from "../../components/Icon";
import { Notice } from "../../components/Feedback";
import "./auth.css";
export function Auth({
  registered,
  onSession,
}: {
  registered: boolean;
  onSession: (session: Session) => void;
}) {
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  return (
    <main class="auth">
      <div class="auth-brand">
        <Icon name="Stacks" />
        Porty
      </div>
      <h1>{registered ? "Welcome back" : "Set up Porty"}</h1>
      <p>
        {registered
          ? "Sign in to manage your stacks."
          : "Create your administrator account, then choose where your stack configuration comes from."}
      </p>
      {error && <Notice>{error}</Notice>}
      <form
        onSubmit={async (event) => {
          event.preventDefault();
          setBusy(true);
          setError("");
          const form = event.currentTarget;
          const data = new FormData(form);
          try {
            const session = await signIn(
              registered,
              data.get("username"),
              data.get("password"),
            );
            form.reset();
            onSession(session);
          } catch (error) {
            setError(message(error));
          } finally {
            setBusy(false);
          }
        }}
      >
        <label>
          Username
          <input name="username" autoComplete="username" required autoFocus />
        </label>
        <label>
          Password
          <input
            name="password"
            type="password"
            autoComplete={registered ? "current-password" : "new-password"}
            minLength={registered ? undefined : 12}
            aria-describedby={!registered ? "password-requirement" : undefined}
            required
          />
        </label>
        {!registered && (
          <p id="password-requirement" class="form-hint">
            Use at least 12 characters.
          </p>
        )}
        <button class="primary" disabled={busy}>
          {busy
            ? "Please wait…"
            : registered
              ? "Sign in"
              : "Create administrator"}
        </button>
      </form>
    </main>
  );
}
