import { useState } from "preact/hooks";
import { api, message, setCSRF, type Session } from "./api";
import { Notice, Icon } from "./ui";
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
          : "Create the administrator account, then configure the fixed stack repository."}
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
            const session = await api<Session>(
              registered ? "/session" : "/setup/register",
              "POST",
              {
                username: data.get("username"),
                password: data.get("password"),
              },
            );
            form.reset();
            setCSRF(session.csrfToken);
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
            required
          />
        </label>
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
