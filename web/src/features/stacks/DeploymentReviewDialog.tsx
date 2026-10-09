import { Dialog } from "../../components/Dialog";
import { Notice } from "../../components/Feedback";
import type { useDeploymentReview } from "./useDeploymentReview";
export function DeploymentReviewDialog({
  name,
  model,
}: {
  name: string;
  model: ReturnType<typeof useDeploymentReview>;
}) {
  const busy = model.phase === "submitting";
  return (
    <Dialog
      open={model.phase !== "idle"}
      title={
        model.phase === "unsaved"
          ? "Save changes before deploying?"
          : `Deploy ${name}?`
      }
      onClose={model.cancel}
    >
      {model.error && <Notice tone="danger">{model.error}</Notice>}
      {model.phase === "unsaved" ? (
        <>
          <p>
            Your editor contains unsaved changes. Save them for review or
            discard them and review the files already on disk.
          </p>
          <div class="dialog-actions">
            <button onClick={model.cancel}>Cancel</button>
            <button onClick={() => void model.discardAndContinue()}>
              Discard and continue
            </button>
            <button
              class="primary"
              onClick={() => void model.saveAndContinue()}
            >
              Save and continue
            </button>
          </div>
        </>
      ) : (
        <>
          {model.phase === "loading" ? (
            <p role="status">Checking saved configuration…</p>
          ) : (
            model.review && (
              <>
                <p>
                  This applies the saved configuration to every service in{" "}
                  <strong>{name}</strong>. Services may restart and be
                  temporarily unavailable.
                </p>
                <p>
                  {model.review.uncommittedChanges
                    ? "This stack has uncommitted files. Deployment uses those saved files without creating a Git commit."
                    : "This stack has no uncommitted file changes."}
                </p>
                <p class="muted">
                  Configuration is checked again when you confirm. This review
                  does not predict every container change.
                </p>
              </>
            )
          )}
          <div class="dialog-actions">
            <button disabled={busy} onClick={model.cancel}>
              Cancel
            </button>
            {model.phase === "error" ? (
              <button onClick={() => void model.request()}>Review again</button>
            ) : (
              <button
                class="primary"
                disabled={model.phase !== "reviewing"}
                aria-busy={busy}
                onClick={() => void model.submit()}
              >
                Deploy stack
              </button>
            )}
          </div>
        </>
      )}
    </Dialog>
  );
}
