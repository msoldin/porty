import { useState } from "preact/hooks";
import { message } from "../../lib/http";
import { changePassword } from "./api";
import type { RepositorySetupStatus } from "../repository/types";
import { Notice } from "../../components/Feedback";
import { RepositorySettings } from "../repository/RepositorySettings";
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
  repositoryStatus: RepositorySetupStatus;
  onRepositoryChange: (status: RepositorySetupStatus) => void;
}) {
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [theme, setTheme] = useState<ThemePreference>(getThemePreference);
  return (
    <div class="settings-content">
      <h1>Settings</h1>
      <section class="appearance-settings">
        <h2>Appearance</h2>
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
      <RepositorySettings
        status={repositoryStatus}
        onChange={onRepositoryChange}
      />
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
