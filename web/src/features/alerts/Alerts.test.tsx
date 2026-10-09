import { render, screen, fireEvent } from "@testing-library/preact";
import { it, expect, vi } from "vitest";
import { AlertList } from "./AlertList";
import type { Alert } from "./types";
const recovered: Alert = {
  id: "a",
  key: { stackId: "s", problem: "deployment", target: "stack" },
  stackName: "App",
  revision: 2,
  episode: 1,
  count: 1,
  summary: "Deployment failed",
  firstAt: "2026-09-27T00:00:00Z",
  latestAt: "2026-09-27T00:00:00Z",
  resolvedAt: "2026-09-27T00:01:00Z",
  resolvedBy: "admin",
  canResolveManually: true,
  operationId: "old-op",
};
it("keeps recovered incidents visible until acknowledged", () => {
  const acknowledge = vi.fn();
  render(
    <AlertList
      items={[recovered]}
      busy={false}
      acknowledge={acknowledge}
      resolve={vi.fn()}
      navigate={vi.fn()}
      openOperation={vi.fn()}
    />,
  );
  expect(screen.getByText("Resolved")).toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "Acknowledge" }));
  expect(acknowledge).toHaveBeenCalledWith(recovered);
});
it("opens an older linked operation", () => {
  const open = vi.fn();
  render(
    <AlertList
      items={[recovered]}
      busy={false}
      acknowledge={vi.fn()}
      resolve={vi.fn()}
      navigate={vi.fn()}
      openOperation={open}
    />,
  );
  fireEvent.click(screen.getByRole("button", { name: "View operation" }));
  expect(open).toHaveBeenCalledWith("old-op");
});
it("keeps acknowledged unresolved incidents listed", () => {
  render(
    <AlertList
      items={[
        {
          ...recovered,
          resolvedAt: undefined,
          acknowledgedAt: recovered.latestAt,
          acknowledgedBy: "admin",
        },
      ]}
      busy={false}
      acknowledge={vi.fn()}
      resolve={vi.fn()}
      navigate={vi.fn()}
      openOperation={vi.fn()}
    />,
  );
  expect(screen.getByText("Open")).toBeInTheDocument();
  expect(
    screen.queryByRole("button", { name: "Acknowledge" }),
  ).not.toBeInTheDocument();
});
it("acknowledging an alert does not start recovery", () => {
  const acknowledge = vi.fn(),
    resolve = vi.fn(),
    open = vi.fn();
  render(
    <AlertList
      items={[
        { ...recovered, resolvedAt: undefined, canResolveManually: false },
      ]}
      busy={false}
      acknowledge={acknowledge}
      resolve={resolve}
      openOperation={open}
      navigate={vi.fn()}
    />,
  );
  expect(
    screen.getByText(/acknowledgement does not recover/i),
  ).toBeInTheDocument();
  expect(screen.getByText(/requires verified recovery/i)).toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "Acknowledge" }));
  expect(acknowledge).toHaveBeenCalledTimes(1);
  expect(resolve).not.toHaveBeenCalled();
  expect(open).not.toHaveBeenCalled();
});
