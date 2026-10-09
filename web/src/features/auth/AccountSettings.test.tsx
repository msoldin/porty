import {
  render,
  screen,
  fireEvent,
  within,
  waitFor,
} from "@testing-library/preact";
import { it, expect, vi } from "vitest";
import { AccountSettings } from "./AccountSettings";
import { changePassword } from "./api";
import type { RepositorySetupStatus } from "../repository/types";
vi.mock("./api", () => ({
  changePassword: vi.fn().mockResolvedValue(undefined),
}));
vi.mock("../repository/RepositorySettings", () => ({
  RepositorySettings: () => null,
}));
it("explains that password changes sign out all sessions", async () => {
  const logout = vi.fn();
  render(
    <AccountSettings
      onLogout={logout}
      onRepositoryChange={vi.fn()}
      repositoryStatus={{} as RepositorySetupStatus}
    />,
  );
  const account = screen.getByRole("region", { name: "Account" });
  expect(account).toHaveTextContent(
    "Changing your password signs out every session.",
  );
  fireEvent.input(screen.getByLabelText("Current password"), {
    target: { value: "current-password" },
  });
  fireEvent.input(screen.getByLabelText("New password"), {
    target: { value: "next-password" },
  });
  fireEvent.click(
    within(account).getByRole("button", { name: "Change password" }),
  );
  await waitFor(() => expect(logout).toHaveBeenCalledTimes(1));
  expect(changePassword).toHaveBeenCalledWith(
    "current-password",
    "next-password",
  );
});
