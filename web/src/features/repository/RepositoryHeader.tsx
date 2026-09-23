import type { Commit, Repository } from "./types";
import { Icon } from "../../components/Icon";

export function RepositoryHeader({
  repo,
  commits,
  remoteEnabled,
  busy,
  onAction,
}: {
  repo: Repository | null;
  commits: Commit[];
  remoteEnabled: boolean;
  busy: boolean;
  onAction: (action: string) => void;
}) {
  const configuredRepo = repo?.configured ? repo : null;
  return (
    <header class="repository-header">
      <div>
        <strong>Repository</strong>
        <span>
          <Icon name="Repository" />
          {configuredRepo?.branch || "Not configured"}
        </span>
      </div>
      <div>
        <strong>Commit</strong>
        <code>{commits[0]?.sha.slice(0, 7) || "—"}</code>
      </div>
      <div class="last-fetch">
        <strong>Last fetched</strong>
        <span>Unavailable</span>
      </div>
      <div class="repository-actions">
        <button
          disabled={busy || !repo || !remoteEnabled}
          onClick={() => onAction("fetch")}
        >
          <Icon name="Refresh" />
          Fetch
        </button>
        <button
          disabled={busy || !configuredRepo || !remoteEnabled}
          onClick={() => onAction("pull")}
        >
          <Icon name="Pull" />
          Pull
        </button>
        <button
          disabled={
            busy ||
            !configuredRepo ||
            !remoteEnabled ||
            configuredRepo.ahead === 0
          }
          onClick={() => onAction("push")}
        >
          <Icon name="Push" />
          Push
        </button>
      </div>
    </header>
  );
}
