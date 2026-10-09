import type { ComponentChildren } from "preact";
import { useLayoutEffect, useRef } from "preact/hooks";
import { Dialog } from "./Dialog";
import { Notice } from "./Feedback";

export type ConfirmDialogProps = {
  open: boolean;
  title: string;
  description: ComponentChildren;
  confirmLabel: string;
  destructive?: boolean;
  busy?: boolean;
  error?: string;
  onConfirm: () => void;
  onCancel: () => void;
};
export function ConfirmDialog({
  open,
  title,
  description,
  confirmLabel,
  destructive = false,
  busy = false,
  error,
  onConfirm,
  onCancel,
}: ConfirmDialogProps) {
  const cancelRef = useRef<HTMLButtonElement>(null);
  const submitted = useRef(false);
  useLayoutEffect(() => {
    if (!busy) submitted.current = false;
  }, [busy, open]);
  return (
    <Dialog
      open={open}
      title={title}
      onClose={() => {
        if (!busy) onCancel();
      }}
      initialFocusRef={cancelRef}
    >
      <div class="dialog-description">{description}</div>
      {error && <Notice tone="danger">{error}</Notice>}
      <div class="dialog-actions">
        <button
          type="button"
          ref={cancelRef}
          disabled={busy}
          onClick={onCancel}
        >
          Cancel
        </button>
        <button
          type="button"
          class={destructive ? "danger" : "primary"}
          disabled={busy}
          aria-busy={busy}
          onClick={() => {
            if (busy || submitted.current) return;
            submitted.current = true;
            onConfirm();
          }}
        >
          {confirmLabel}
        </button>
      </div>
    </Dialog>
  );
}
