import { fireEvent, render, screen, waitFor } from "@testing-library/preact";
import { expect, it, vi } from "vitest";
import { RepositoryPage } from "./RepositoryPage";
import { runRepositoryAction } from "./api";
vi.mock("./api", () => ({
  runRepositoryAction: vi
    .fn()
    .mockResolvedValue({ id: "op", status: "queued" }),
}));
it("explains fast-forward pull and confirms repository-wide publication", async () => {
  render(
    <RepositoryPage
      repo={{
        configured: true,
        branch: "main",
        dirty: false,
        ahead: 1,
        behind: 0,
        paths: [],
      }}
      commits={[]}
      remoteEnabled
      dirty={false}
      onAccepted={vi.fn()}
    />,
  );
  expect(screen.getByText(/fast-forward/i)).toBeInTheDocument();
  expect(screen.getByText(/does not deploy/i)).toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "Push", exact: true }));
  expect(runRepositoryAction).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole("button", { name: "Push commits" }));
  await waitFor(() => expect(runRepositoryAction).toHaveBeenCalledWith("push"));
});
