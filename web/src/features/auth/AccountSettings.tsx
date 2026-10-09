import { useState } from "preact/hooks";
import { message } from "../../lib/http";
import { changePassword } from "./api";
import type { RepositorySetupStatus } from "../repository/types";
import { Notice } from "../../components/Feedback";
import { RepositorySettings } from "../repository/RepositorySettings";
import "./auth.css";
import {
  getThemePreference,
  setThemePreference,
  type ThemePreference,
} from "../../app/theme";

export function AccountSettings({
  onLogout,
  repositoryStatus,
  onRepositoryChange,
}: {
  onLogout: () => void;
  repositoryStatus: RepositorySetupStatus | null;
  onRepositoryChange: (status: RepositorySetupStatus) => void;
}) {
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [theme, setTheme] = useState<ThemePreference>(getThemePreference);
  return (
    <div class="settings-content account-settings">
      <h1>Settings</h1>
      <section class="appearance-settings" aria-label="Appearance">
        <h2>Appearance</h2>
        <p class="muted">
          Choose a theme for this browser. System follows your device
          preference.
        </p>
        <label>
          Theme
          <select
            value={theme}
            onChange={(event) => {
              const preference = event.currentTarget.value as ThemePreference;
              setThemePreference(preference);
              setTheme(preference);
            }}
          >
            <option value="system">System</option>
            <option value="light">Light</option>
            <option value="dark">Dark</option>
          </select>
        </label>
      </section>
      {repositoryStatus?.state === "ready" && (
        <RepositorySettings
          status={repositoryStatus}
          onChange={onRepositoryChange}
        />
      )}
      <section class="account-card" aria-label="Account">
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
              await changePassword(data.get("current"), data.get("next"));
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
              aria-describedby="new-password-requirement"
              required
            />
          </label>
          <p id="new-password-requirement" class="form-hint">
            Use at least 12 characters.
          </p>
          <button disabled={busy} class="primary">
            Change password
          </button>
        </form>
      </section>
    </div>
  );
}
