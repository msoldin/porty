import { fireEvent, render, screen, waitFor } from "@testing-library/preact";
import { expect, it, vi } from "vitest";
import { OperationDrawer } from "./OperationDrawer";
import { operationPresentation } from "./operationPresentation";
import type { Operation } from "./types";
const operation: Operation = {
  id: "one",
  kind: "deploy",
  scopeType: "stack",
  scopeId: "monitoring",
  status: "queued",
  outputTruncated: false,
};
it("shows accepted work as queued rather than succeeded", () => {
  expect(operationPresentation(operation)).toMatchObject({
    label: "Queued",
    terminal: false,
  });
  expect(
    operationPresentation({ ...operation, status: "cancelled" }),
  ).toMatchObject({ label: "Cancelled", terminal: true });
  expect(
    operationPresentation({ ...operation, status: "unexpected" }),
  ).toMatchObject({ label: "Unknown", terminal: false });
});
it("keeps failed output available and explains unavailable cancellation", async () => {
  const trigger = document.createElement("button");
  document.body.append(trigger);
  trigger.focus();
  const view = render(
    <OperationDrawer
      operation={{
        ...operation,
        status: "failed",
        errorCode: "server_interrupted",
        output: "Pull failed",
        outputTruncated: true,
      }}
      close={vi.fn()}
    />,
  );
  expect(
    screen.getByRole("region", { name: "Operation details" }),
  ).toBeInTheDocument();
  expect(screen.getByText("Pull failed")).toBeInTheDocument();
  expect(screen.getByText("Output was truncated.")).toBeInTheDocument();
  expect(
    screen.getByText(/cancellation is not available/i),
  ).toBeInTheDocument();
  await waitFor(() =>
    expect(
      screen.getByRole("button", { name: "Close operation" }),
    ).toHaveFocus(),
  );
  view.unmount();
  expect(trigger).toHaveFocus();
  trigger.remove();
  render(
    <OperationDrawer
      operation={{ ...operation, status: "failed", output: "Pull failed" }}
      close={vi.fn()}
    />,
  );
  expect(screen.getByText("Pull failed")).toBeInTheDocument();
});
it("distinguishes terminal empty output from waiting for running work", () => {
  const view = render(
    <OperationDrawer
      operation={{ ...operation, status: "succeeded" }}
      close={vi.fn()}
    />,
  );
  expect(screen.getByText("No output was recorded.")).toBeInTheDocument();
  view.rerender(
    <OperationDrawer
      operation={{ ...operation, status: "running" }}
      close={vi.fn()}
    />,
  );
  expect(screen.getByText("Waiting for operation output…")).toBeInTheDocument();
});
