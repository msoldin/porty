import { render, screen, fireEvent, waitFor } from "@testing-library/preact";
import { it, expect, vi, afterEach } from "vitest";
import { OnDemandSettings } from "./OnDemandSettings";
import { listOnDemand, saveOnDemand, changeOnDemand } from "./onDemandApi";
vi.mock("./onDemandApi", () => ({
  listOnDemand: vi.fn(),
  saveOnDemand: vi.fn(),
  changeOnDemand: vi.fn(),
}));
afterEach(() => vi.clearAllMocks());
const group = {
  id: "g",
  stackId: "s",
  name: "Minecraft",
  enabled: true,
  members: ["game"],
  revision: 3,
  phase: "sleeping" as const,
  wakeThreshold: 1,
  wakeWindowMs: 1000,
  idleSeconds: 600,
  minRuntimeSeconds: 120,
  startupSeconds: 300,
  stopGraceSeconds: 120,
};
it("explains one-attempt wake and saves the configurable threshold", async () => {
  vi.mocked(listOnDemand).mockResolvedValue([]);
  vi.mocked(saveOnDemand).mockResolvedValue(group);
  render(<OnDemandSettings stackId="s" />);
  fireEvent.click(
    await screen.findByRole("button", { name: "Add on-demand group" }),
  );
  expect(screen.getByLabelText("Wake attempts")).toHaveValue(1);
  expect(screen.getByText(/completed TCP connection/)).toBeInTheDocument();
  fireEvent.input(screen.getByLabelText("Group name"), {
    target: { value: "Minecraft" },
  });
  fireEvent.input(screen.getByLabelText("Services"), {
    target: { value: "game" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Save group" }));
  await waitFor(() =>
    expect(saveOnDemand).toHaveBeenCalledWith(
      "s",
      "",
      expect.objectContaining({
        wakeThreshold: 1,
        members: ["game"],
        expectedRevision: 0,
      }),
    ),
  );
});
it("requires explicit resume for a held group", async () => {
  vi.mocked(listOnDemand).mockResolvedValue([
    { ...group, holdReason: "Manually stopped." },
  ]);
  vi.mocked(changeOnDemand).mockResolvedValue(group);
  render(<OnDemandSettings stackId="s" />);
  expect(await screen.findByText("Manually stopped.")).toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "Resume Minecraft" }));
  await waitFor(() =>
    expect(changeOnDemand).toHaveBeenCalledWith("s", "g", "resume", 3),
  );
  expect(saveOnDemand).not.toHaveBeenCalled();
});
