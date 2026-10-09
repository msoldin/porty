import type { Repository, Commit } from "./types";
import type { Operation } from "../operations/types";
import { useRef, useState } from "preact/hooks";
import { runRepositoryAction } from "./api";
import { message } from "../../lib/http";
import { RepositoryHeader } from "./RepositoryHeader";
import { RepositoryHistory } from "./RepositoryHistory";
import { ConfirmDialog } from "../../components/ConfirmDialog";
import { Notice } from "../../components/Feedback";
import "./repository.css";
type Props = {
  repo: Repository | null;
  commits: Commit[];
  remoteEnabled: boolean;
  dirty: boolean;
  onAccepted: (operation: Operation) => void;
};
export function RepositoryPage({
  repo,
  commits,
  remoteEnabled,
  dirty,
  onAccepted,
}: Props) {
  const [busy, setBusy] = useState(false);
  const submitting = useRef(false);
  const [error, setError] = useState("");
  const [push, setPush] = useState(false);
  async function run(action: string) {
    if (submitting.current || dirty || !remoteEnabled || !repo) return;
    submitting.current = true;
    setBusy(true);
    setError("");
    try {
      const operation = await runRepositoryAction(action);
      setPush(false);
      onAccepted(operation);
    } catch (cause) {
      setError(message(cause));
    } finally {
      submitting.current = false;
      setBusy(false);
    }
  }
  return (
    <section class="repository-page">
      <RepositoryHeader
        repo={repo}
        commits={commits}
        remoteEnabled={remoteEnabled}
        busy={busy || dirty}
        onAction={(action) => {
          if (action === "push") {
            setError("");
            setPush(true);
          } else void run(action);
        }}
      />
      <div class="repository-explanation">
        <p>
          <strong>Fetch</strong> updates remote information.{" "}
          <strong>Pull</strong> fast-forwards local files to the remote branch.{" "}
          <strong>Push</strong> publishes commits from every stack in this
          repository.
        </p>
        <p>
          Repository synchronization does not deploy your stacks. Deploy saved
          configuration from each stack when ready.
        </p>
      </div>
      {dirty && (
        <Notice>
          Save or discard editor changes before changing the repository.
        </Notice>
      )}
      {error && !push && <Notice>{error}</Notice>}
      <ConfirmDialog
        open={push}
        title={`Push ${repo?.branch || "repository"} to origin?`}
        description="This publishes committed changes from the entire repository, including other stacks. Uncommitted files are not published."
        confirmLabel="Push commits"
        busy={busy}
        error={error}
        onCancel={() => setPush(false)}
        onConfirm={() => void run("push")}
      />
      <RepositoryHistory repo={repo} commits={commits} />
    </section>
  );
}
