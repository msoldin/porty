import { render, screen, fireEvent, waitFor } from "@testing-library/preact";
import { it, expect, vi } from "vitest";
import { useStackBatchActions } from "./useStackBatchActions";
import { StackBatchDialog } from "./StackBatchDialog";
import { getDeploymentReview } from "./deploymentReviewApi";
import type { Stack } from "./types";
vi.mock("./deploymentReviewApi", () => ({
  getDeploymentReview: vi.fn(),
  deployReviewedStack: vi.fn(),
}));
it("lets the user cancel while deployment review is still loading", async () => {
  vi.mocked(getDeploymentReview).mockReturnValue(new Promise(() => {}));
  function Fixture() {
    const model = useStackBatchActions({
      stacks: [{ id: "s", directoryName: "monitoring" } as Stack],
      states: {},
      operations: [],
      onAccepted: () => {},
    });
    return (
      <>
        <button onClick={() => model.setSelectedIds(new Set(["s"]))}>
          Select
        </button>
        <button onClick={() => void model.request("deploy")}>Review</button>
        <StackBatchDialog model={model} />
      </>
    );
  }
  render(<Fixture />);
  fireEvent.click(screen.getByRole("button", { name: "Select" }));
  fireEvent.click(screen.getByRole("button", { name: "Review" }));
  await waitFor(() => expect(getDeploymentReview).toHaveBeenCalledWith("s"));
  expect(screen.getByRole("button", { name: "Cancel" })).toBeEnabled();
  fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
});
