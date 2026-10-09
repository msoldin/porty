import { ConfirmDialog } from "../../components/ConfirmDialog";
import type { useContainerBatchActions } from "./useContainerBatchActions";
export function ContainerBatchDialog({
  model,
}: {
  model: ReturnType<typeof useContainerBatchActions>;
}) {
  const review = model.review;
  const action = review
    ? review.kind[0].toUpperCase() + review.kind.slice(1)
    : "Run";
  return (
    <ConfirmDialog
      open={!!review}
      title={`${action} ${review?.targets.length || 0} selected containers?`}
      confirmLabel={`${action} containers`}
      destructive={review?.kind === "stop"}
      busy={model.pending}
      error={model.error}
      onCancel={model.cancel}
      onConfirm={() => void model.confirm()}
      description={
        <>
          <p>
            Only these containers will be affected.{" "}
            {review?.kind !== "start" &&
              "Services may be temporarily unavailable. Data volumes are retained."}
          </p>
          <ul>
            {review?.targets.map((target) => (
              <li key={`${target.stackId}:${target.container.id}`}>
                {target.stackName} / {target.container.name}
              </li>
            ))}
          </ul>
        </>
      }
    />
  );
}
