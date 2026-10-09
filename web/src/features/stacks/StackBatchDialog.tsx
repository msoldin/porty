import { Dialog } from "../../components/Dialog";
import { Notice } from "../../components/Feedback";
import type { useStackBatchActions } from "./useStackBatchActions";
export function StackBatchDialog({
  model,
}: {
  model: ReturnType<typeof useStackBatchActions>;
}) {
  const action =
    model.review?.kind === "stop"
      ? "Stop"
      : model.review?.kind === "restart"
        ? "Restart"
        : "Deploy";
  return (
    <Dialog
      open={!!model.review}
      title={`${action} ${model.review?.targets.length || 0} stacks?`}
      onClose={model.cancel}
    >
      <p>
        Every service in these stacks will be affected. Services may be
        temporarily unavailable. Data volumes are retained.
      </p>
      <ul>
        {model.review?.targets.map((target) => (
          <li key={target.id}>{target.name}</li>
        ))}
      </ul>
      {model.review?.kind === "deploy" && (
        <p>
          Deployment applies saved files, including uncommitted changes. It does
          not create a Git commit.
        </p>
      )}
      {model.pending && (
        <p role="status">Preparing or submitting the selected stacks…</p>
      )}
      {model.error && <Notice>{model.error}</Notice>}
      <div class="dialog-actions">
        <button disabled={model.submitting} onClick={model.cancel}>
          Cancel
        </button>
        <button
          class={action === "Stop" ? "danger" : "primary"}
          disabled={model.pending || !!model.error}
          onClick={() => void model.confirm()}
        >
          {action} stacks
        </button>
      </div>
    </Dialog>
  );
}
