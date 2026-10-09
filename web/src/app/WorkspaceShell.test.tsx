import { fireEvent, render, screen } from "@testing-library/preact";
import { expect, it, vi } from "vitest";
import { WorkspaceShell } from "./WorkspaceShell";

function showShell(route = "/", stackSelected = false) {
  const navigate = vi.fn();
  const onSignOut = vi.fn();
  render(
    <WorkspaceShell
      unacknowledgedAlerts={2}
      username="admin"
      route={route}
      stackSelected={stackSelected}
      operationOpen={false}
      connection="Connected"
      navigate={navigate}
      onSignOut={onSignOut}
    >
      <h1>Workspace</h1>
    </WorkspaceShell>,
  );
  return { navigate, onSignOut };
}

it("opens every destination from mobile navigation and closes after selection", () => {
  const { navigate } = showShell();
  const toggle = screen.getByRole("button", { name: "Navigation" });
  expect(toggle).toHaveAttribute("aria-expanded", "false");
  fireEvent.click(toggle);
  expect(toggle).toHaveAttribute("aria-expanded", "true");
  for (const name of [
    "Dashboard",
    "Stacks",
    "Repository",
    "Operations",
    "Alerts",
    "Audit log",
    "Settings",
  ]) {
    expect(
      screen.getByRole("link", { name: new RegExp(name) }),
    ).toBeInTheDocument();
  }
  fireEvent.click(screen.getByRole("link", { name: "Settings" }));
  expect(navigate).toHaveBeenCalledWith("/settings");
  expect(toggle).toHaveAttribute("aria-expanded", "false");
});

it("marks the current stack section and retains account access", () => {
  const { onSignOut } = showShell("/stacks/monitoring", true);
  expect(screen.getByRole("link", { name: "Stacks" })).toHaveAttribute(
    "href",
    "#/stacks",
  );
  expect(screen.getByRole("link", { name: "Stacks" })).toHaveAttribute(
    "aria-current",
    "page",
  );
  expect(screen.getByText("admin")).toBeInTheDocument();
  expect(screen.getByText(/Live updates connected/)).toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "Sign out" }));
  expect(onSignOut).toHaveBeenCalledTimes(1);
});
