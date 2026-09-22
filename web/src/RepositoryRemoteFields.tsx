import type { RepositoryAuthType, RepositorySetupStatus } from "./api";

export function RemoteAuthenticationFields({
  ssh,
  authType,
  username,
  secret,
  disabled = false,
  onAuthType,
  onUsername,
  onSecret,
}: {
  ssh: RepositorySetupStatus["ssh"];
  authType: RepositoryAuthType;
  username: string;
  secret: string;
  disabled?: boolean;
  onAuthType: (value: RepositoryAuthType) => void;
  onUsername: (value: string) => void;
  onSecret: (value: string) => void;
}) {
  return (
    <>
      <fieldset class="choice-list" disabled={disabled}>
        <legend>Authentication</legend>
        <label>
          <input
            type="radio"
            name="authentication"
            checked={authType === "none"}
            onChange={() => onAuthType("none")}
          />
          No authentication
        </label>
        <label>
          <input
            type="radio"
            name="authentication"
            checked={authType === "https"}
            onChange={() => onAuthType("https")}
          />
          HTTPS username and secret
        </label>
        <label>
          <input
            type="radio"
            name="authentication"
            checked={authType === "ssh"}
            disabled={disabled || !ssh.usable}
            onChange={() => onAuthType("ssh")}
          />
          Mounted SSH files
        </label>
      </fieldset>
      {authType === "https" && (
        <div class="setup-fields two-columns">
          <label>
            HTTPS username
            <input
              value={username}
              autocomplete="username"
              disabled={disabled}
              onInput={(event) => onUsername(event.currentTarget.value)}
              required
            />
          </label>
          <label>
            HTTPS secret
            <input
              value={secret}
              type="password"
              autocomplete="new-password"
              disabled={disabled}
              onInput={(event) => onSecret(event.currentTarget.value)}
              required
            />
          </label>
        </div>
      )}
      {authType === "ssh" && (
        <p class="muted">
          {ssh.usable
            ? "SSH identity and known hosts are ready."
            : "The mounted SSH identity and known hosts are not usable."}
        </p>
      )}
      {!ssh.usable && authType !== "ssh" && (
        <p class="muted">
          Mounted SSH files are unavailable until both fixed files are safe.
        </p>
      )}
    </>
  );
}
