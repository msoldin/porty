import { render, screen, fireEvent, waitFor } from "@testing-library/preact";
import { it, expect, vi, afterEach } from "vitest";
import { AutoUpdateSettings } from "./AutoUpdateSettings";
import {
  getAutoUpdate,
  saveAutoUpdate,
  resumeAutoUpdate,
} from "./autoUpdateApi";
import { APIError } from "../../lib/http";
vi.mock("./autoUpdateApi", () => ({
  getAutoUpdate: vi.fn(),
  saveAutoUpdate: vi.fn(),
  resumeAutoUpdate: vi.fn(),
}));
afterEach(() => vi.clearAllMocks());
const status = {
  policy: {
    stackId: "s",
    enabled: false,
    expression: "0 0 * * *",
    revision: 0,
    nextRunAt: "2026-09-28T00:00:00Z",
  },
  available: true,
  eligible: false,
  eligibilityReason: "Stack is stopped",
  excluded: {},
};
it("defaults to midnight UTC", async () => {
  vi.mocked(getAutoUpdate).mockResolvedValue(status);
  render(<AutoUpdateSettings stackId="s" />);
  expect(await screen.findByLabelText("Cron schedule (UTC)")).toHaveValue(
    "0 0 * * *",
  );
  await waitFor(() =>
    expect(
      screen.getByRole("button", { name: "Save schedule" }),
    ).not.toBeDisabled(),
  );
  expect(screen.getByLabelText("Enable automatic updates")).not.toBeChecked();
  expect(screen.getByText(/midnight UTC/)).toBeInTheDocument();
});
it("shows next UTC run and skipped reason", async () => {
  vi.mocked(getAutoUpdate).mockResolvedValue({
    ...status,
    policy: { ...status.policy, enabled: true },
    lastRun: {
      id: "r",
      outcome: "skipped",
      reason: "stack_not_eligible",
      scheduledAt: "2026-09-27T00:00:00Z",
    },
  });
  render(<AutoUpdateSettings stackId="s" />);
  expect(
    await screen.findByText(/28 Sept? 2026, 00:00 UTC/),
  ).toBeInTheDocument();
  expect(screen.getByText(/stack not eligible/)).toBeInTheDocument();
});
it("keeps pause separate from acknowledgment", async () => {
  const paused = {
    ...status,
    policy: {
      ...status.policy,
      revision: 3,
      pausedReason: "recovery_required",
    },
  };
  vi.mocked(getAutoUpdate).mockResolvedValue(paused);
  vi.mocked(resumeAutoUpdate).mockResolvedValue(status);
  render(<AutoUpdateSettings stackId="s" />);
  fireEvent.click(
    await screen.findByRole("button", { name: /Verify recovery/ }),
  );
  await waitFor(() => expect(resumeAutoUpdate).toHaveBeenCalledWith("s", 3));
  expect(saveAutoUpdate).not.toHaveBeenCalled();
});
it("does not enable after stale policy response", async () => {
  vi.mocked(getAutoUpdate).mockResolvedValue(status);
  vi.mocked(saveAutoUpdate).mockRejectedValue(
    new APIError(409, "PolicyConflict", "Changed"),
  );
  render(<AutoUpdateSettings stackId="s" />);
  await waitFor(() =>
    expect(
      screen.getByRole("button", { name: "Save schedule" }),
    ).not.toBeDisabled(),
  );
  fireEvent.click(screen.getByLabelText("Enable automatic updates"));
  fireEvent.click(screen.getByRole("button", { name: "Save schedule" }));
  expect(await screen.findByText(/policy changed/i)).toBeInTheDocument();
  expect(screen.getByLabelText("Enable automatic updates")).not.toBeChecked();
});
