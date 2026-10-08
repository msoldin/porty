import { render, screen, fireEvent, waitFor } from "@testing-library/preact";
import { it, expect, vi, afterEach, beforeEach } from "vitest";
import { OnDemandSettings } from "./OnDemandSettings";
import { listOnDemand, saveOnDemand, changeOnDemand } from "./onDemandApi";
import { listStackContainers } from "./api";
vi.mock("./api", () => ({ listStackContainers: vi.fn() }));
const game = {
  id: "one",
  name: "minecraft-game-1",
  service: "game",
  state: "running",
  health: "",
  image: "itzg/minecraft-server",
  networks: [],
  ports: [
    {
      host: "0.0.0.0",
      targetPort: 25565,
      publishedPort: 25565,
      protocol: "tcp",
    },
  ],
};
beforeEach(() => {
  vi.mocked(listStackContainers).mockResolvedValue([game]);
});
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
  expect(screen.getByRole("button", { name: "Save group" })).toBeDisabled();
  fireEvent.click(await screen.findByRole("checkbox", { name: /game/ }));
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
it("explains unavailable services and prevents assigning a service twice", async () => {
  vi.mocked(listOnDemand).mockResolvedValue([group]);
  vi.mocked(listStackContainers).mockResolvedValue([
    game,
    { ...game, id: "two", service: "backup", state: "exited" },
    { ...game, id: "three", service: "proxy" },
  ]);
  render(<OnDemandSettings stackId="s" />);
  fireEvent.click(
    await screen.findByRole("button", { name: "Add on-demand group" }),
  );
  expect(await screen.findByRole("checkbox", { name: /game/ })).toBeDisabled();
  expect(screen.getByText(/Already in Minecraft/)).toBeInTheDocument();
  expect(screen.getByRole("checkbox", { name: /backup/ })).toBeDisabled();
  expect(screen.getByRole("checkbox", { name: /proxy/ })).toBeEnabled();
});
it("lets a sleeping group's services remain selected when editing", async () => {
  vi.mocked(listOnDemand).mockResolvedValue([group]);
  vi.mocked(listStackContainers).mockResolvedValue([
    { ...game, state: "exited" },
  ]);
  render(<OnDemandSettings stackId="s" />);
  fireEvent.click(
    await screen.findByRole("button", { name: "Edit Minecraft" }),
  );
  const selection = await screen.findByRole("checkbox", { name: /game/ });
  expect(selection).toBeChecked();
  expect(selection).toBeDisabled();
  expect(screen.getByRole("button", { name: "Save group" })).toBeEnabled();
});
it("unlocks service changes after the sleeping containers are started manually", async () => {
  vi.mocked(listOnDemand).mockResolvedValue([
    { ...group, pausedReason: "Manual start requires resume." },
  ]);
  render(<OnDemandSettings stackId="s" />);
  fireEvent.click(
    await screen.findByRole("button", { name: "Edit Minecraft" }),
  );
  await waitFor(() =>
    expect(screen.getByRole("checkbox", { name: /game/ })).toBeEnabled(),
  );
});
it("blocks saving when service discovery fails", async () => {
  vi.mocked(listOnDemand).mockResolvedValue([]);
  vi.mocked(listStackContainers).mockRejectedValue(
    new Error("Docker unavailable"),
  );
  render(<OnDemandSettings stackId="s" />);
  fireEvent.click(
    await screen.findByRole("button", { name: "Add on-demand group" }),
  );
  expect(await screen.findByText("Docker unavailable")).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Save group" })).toBeDisabled();
});
it("lets an existing group remove a service that is no longer deployed", async () => {
  vi.mocked(listOnDemand).mockResolvedValue([
    { ...group, phase: "running", members: ["game", "removed"] },
  ]);
  render(<OnDemandSettings stackId="s" />);
  fireEvent.click(
    await screen.findByRole("button", { name: "Edit Minecraft" }),
  );
  const removed = await screen.findByRole("checkbox", { name: /removed/ });
  expect(removed).toBeEnabled();
  expect(screen.getByRole("button", { name: "Save group" })).toBeDisabled();
  fireEvent.click(removed);
  await waitFor(() =>
    expect(screen.getByRole("button", { name: "Save group" })).toBeEnabled(),
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
