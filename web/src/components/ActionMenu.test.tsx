import { fireEvent, render, screen } from "@testing-library/preact";
import { expect, it, vi } from "vitest";
import { ActionMenu } from "./ActionMenu";

it("opens by keyboard, navigates enabled actions, and returns focus on Escape", () => {
  const onSelect = vi.fn();
  render(
    <ActionMenu
      label="Actions"
      items={[
        { id: "deploy", label: "Deploy" },
        {
          id: "restart",
          label: "Restart",
          disabled: true,
          reason: "Requires running stacks",
        },
        { id: "stop", label: "Stop" },
      ]}
      onSelect={onSelect}
    />,
  );
  const trigger = screen.getByRole("button", { name: "Actions" });
  trigger.focus();
  fireEvent.keyDown(trigger, { key: "Enter" });
  expect(trigger).toHaveAttribute("aria-expanded", "true");
  expect(screen.getByRole("menuitem", { name: "Deploy" })).toHaveFocus();
  fireEvent.keyDown(screen.getByRole("menu"), { key: "ArrowDown" });
  expect(screen.getByRole("menuitem", { name: "Stop" })).toHaveFocus();
  fireEvent.keyDown(screen.getByRole("menu"), { key: "Escape" });
  expect(trigger).toHaveFocus();
  expect(screen.queryByRole("menu")).toBeNull();
  expect(onSelect).not.toHaveBeenCalled();
});

it("explains disabled items and activates only enabled actions", () => {
  const onSelect = vi.fn();
  render(
    <ActionMenu
      label="Actions"
      items={[
        { id: "deploy", label: "Deploy" },
        {
          id: "stop",
          label: "Stop",
          disabled: true,
          reason: "No running selection",
        },
      ]}
      onSelect={onSelect}
    />,
  );
  fireEvent.click(screen.getByRole("button", { name: "Actions" }));
  const stop = screen.getByRole("menuitem", { name: "Stop" });
  expect(stop).toHaveAttribute("aria-disabled", "true");
  expect(stop).toHaveAttribute("title", "No running selection");
  fireEvent.click(stop);
  expect(onSelect).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole("menuitem", { name: "Deploy" }));
  expect(onSelect).toHaveBeenCalledWith("deploy");
  expect(screen.queryByRole("menu")).toBeNull();
});
