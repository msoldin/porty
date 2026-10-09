import { act, renderHook } from "@testing-library/preact";
import { beforeEach, expect, it, vi } from "vitest";
import { APIError } from "../../lib/http";
import {
  getDeploymentReview,
  deployReviewedStack,
} from "./deploymentReviewApi";
import { useDeploymentReview, type ReviewOptions } from "./useDeploymentReview";
vi.mock("./deploymentReviewApi", () => ({
  getDeploymentReview: vi.fn(),
  deployReviewedStack: vi.fn(),
}));
const review = {
  stackId: "one",
  sourceRevision: "first",
  uncommittedChanges: true,
};
function options(dirty = false): ReviewOptions {
  return {
    stack: { id: "one", directoryName: "monitoring" } as never,
    operations: [],
    onAccepted: vi.fn(),
    editor: {
      dirty,
      save: vi.fn().mockResolvedValue(true),
      discard: vi.fn(),
    } as never,
  };
}
beforeEach(() => {
  vi.resetAllMocks();
  vi.mocked(getDeploymentReview).mockResolvedValue(review);
});
it("saves before review without deploying and discards only on explicit choice", async () => {
  const props = options(true);
  const view = renderHook(() => useDeploymentReview(props));
  await act(async () => view.result.current.request());
  expect(view.result.current.phase).toBe("unsaved");
  expect(props.editor.discard).not.toHaveBeenCalled();
  act(() => view.result.current.cancel());
  expect(props.editor.save).not.toHaveBeenCalled();
  expect(props.editor.discard).not.toHaveBeenCalled();
  await act(async () => view.result.current.request());
  await act(async () => view.result.current.saveAndContinue());
  expect(props.editor.save).toHaveBeenCalledTimes(1);
  expect(getDeploymentReview).toHaveBeenCalledWith("one");
  expect(deployReviewedStack).not.toHaveBeenCalled();
  expect(view.result.current.phase).toBe("reviewing");
  act(() => view.result.current.cancel());
  await act(async () => view.result.current.request());
  await act(async () => view.result.current.discardAndContinue());
  expect(props.editor.discard).toHaveBeenCalledTimes(1);
});
it("requires fresh confirmation after a 412 response and guards duplicate submission", async () => {
  vi.mocked(getDeploymentReview)
    .mockResolvedValueOnce(review)
    .mockResolvedValueOnce({ ...review, sourceRevision: "second" });
  vi.mocked(deployReviewedStack).mockRejectedValueOnce(
    new APIError(412, "DeploymentReviewChanged", "Changed"),
  );
  const view = renderHook(() => useDeploymentReview(options()));
  await act(async () => view.result.current.request());
  await act(async () => {
    await Promise.all([
      view.result.current.submit(),
      view.result.current.submit(),
    ]);
  });
  expect(deployReviewedStack).toHaveBeenCalledTimes(1);
  expect(view.result.current.phase).toBe("reviewing");
  expect(view.result.current.error).toContain("changed");
  await act(async () => view.result.current.submit());
  expect(deployReviewedStack).toHaveBeenLastCalledWith("one", "second");
});
it("ignores a review response after cancellation or changing stacks", async () => {
  let finish!: (value: typeof review) => void;
  vi.mocked(getDeploymentReview).mockImplementation(
    () =>
      new Promise((resolve) => {
        finish = resolve;
      }),
  );
  const props = options();
  const view = renderHook((p) => useDeploymentReview(p), {
    initialProps: props,
  });
  let pending!: Promise<void>;
  act(() => {
    pending = view.result.current.request();
  });
  view.rerender({ ...props, stack: { ...props.stack, id: "two" } });
  await act(async () => {
    finish(review);
    await pending;
  });
  expect(view.result.current.phase).toBe("idle");
  expect(deployReviewedStack).not.toHaveBeenCalled();
});
