import type { Operation } from "./types";
import { useEffect, useLayoutEffect, useRef, useState } from "preact/hooks";
import { Dialog } from "../../components/Dialog";
import { operationPresentation } from "./operationPresentation";
import "./operations.css";
import { Icon } from "../../components/Icon";
import { Badge, Notice } from "../../components/Feedback";

export function OperationDrawer({
  operation,
  close,
}: {
  operation?: Operation;
  close: () => void;
}) {
  const [mobile, setMobile] = useState(
    () => window.matchMedia?.("(max-width: 767px)").matches ?? false,
  );
  const closeButton = useRef<HTMLButtonElement>(null);
  useEffect(() => {
    const media = window.matchMedia?.("(max-width: 767px)");
    if (!media) return;
    const change = () => setMobile(media.matches);
    media.addEventListener("change", change);
    change();
    return () => media.removeEventListener("change", change);
  }, []);
  useLayoutEffect(() => {
    if (mobile) return;
    const trigger = document.activeElement as HTMLElement | null;
    closeButton.current?.focus();
    return () => {
      if (trigger?.isConnected) trigger.focus();
    };
  }, [mobile]);
  const state = operation ? operationPresentation(operation) : undefined;
  const content = (
    <>
      <div class="drawer-title">
        <h2>
          <Icon name="Operations" />
          Operations
        </h2>
        <button
          class="icon-button"
          aria-label="Close operation"
          ref={closeButton}
          onClick={close}
        >
          <Icon name="Close" />
        </button>
      </div>
      <div class="drawer-body">
        {!operation && <p role="status">Loading operation…</p>}
        {operation && state && (
          <>
            <h3>
              {operation.kind} {operation.scopeId}
            </h3>
            <Badge tone={state.tone}>{state.label}</Badge>
            {operation.errorCode && <Notice>{operation.errorCode}</Notice>}
            {operation.errorCode?.includes("interrupted") && (
              <p>
                Work was interrupted. Verify the stack before retrying or
                resuming automation.
              </p>
            )}
            <p>Trigger: {operation.trigger || "manual"}</p>
            {operation.startedAt &&
              !operation.startedAt.startsWith("0001-") && (
                <p>Started: {new Date(operation.startedAt).toLocaleString()}</p>
              )}
            {operation.completedAt &&
              !operation.completedAt.startsWith("0001-") && (
                <p>
                  Completed: {new Date(operation.completedAt).toLocaleString()}
                </p>
              )}
            {operation.serviceUpdates?.map((service) => (
              <section key={service.service} class="update-result">
                <h4>
                  {service.service}: {service.outcome}
                </h4>
                <p>
                  Before: <code>{service.beforeImageId || "Unknown"}</code>
                </p>
                <p>
                  Target: <code>{service.targetImageId || "Unknown"}</code>
                </p>
                <p>
                  Actual: <code>{service.actualImageId || "Unknown"}</code>
                </p>
              </section>
            ))}
            <h4>Output</h4>
            <pre class="output">
              {operation.output ||
                (state.terminal
                  ? "No output was recorded."
                  : operation.status === "queued" ||
                      operation.status === "running"
                    ? "Waiting for operation output…"
                    : "Output is unavailable for this status.")}
            </pre>
            {operation.outputTruncated && (
              <Notice>Output was truncated.</Notice>
            )}
          </>
        )}
      </div>
      <div class="drawer-footer">
        <button disabled aria-describedby="cancellation-unavailable">
          Cancel operation
        </button>
        <p id="cancellation-unavailable" class="muted">
          Operation cancellation is not available on this server.
        </p>
      </div>
    </>
  );
  return mobile ? (
    <Dialog
      open
      title="Operation details"
      onClose={close}
      initialFocusRef={closeButton}
    >
      <div class="mobile-operation">{content}</div>
    </Dialog>
  ) : (
    <aside
      role="region"
      class="operation-drawer"
      aria-label="Operation details"
      onKeyDown={(event) => {
        if (event.key === "Escape") close();
      }}
    >
      {content}
    </aside>
  );
}
