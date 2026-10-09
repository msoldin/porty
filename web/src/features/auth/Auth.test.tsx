import { render, screen, fireEvent } from "@testing-library/preact";
import { it, expect, vi } from "vitest";
import { Auth } from "./Auth";
import { signIn } from "./api";
vi.mock("./api", () => ({ signIn: vi.fn() }));
it("explains password requirements before creating the administrator", () => {
  render(<Auth registered={false} onSession={vi.fn()} />);
  expect(screen.getByText("Use at least 12 characters.")).toBeInTheDocument();
  expect(screen.getByLabelText("Password")).toHaveAttribute(
    "autocomplete",
    "new-password",
  );
});
it("keeps the username and password-manager semantics after rejected sign in", async () => {
  vi.mocked(signIn).mockRejectedValueOnce(new Error("Sign in failed"));
  render(<Auth registered onSession={vi.fn()} />);
  fireEvent.input(screen.getByLabelText("Username"), {
    target: { value: "admin" },
  });
  fireEvent.input(screen.getByLabelText("Password"), {
    target: { value: "entered-secret" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Sign in" }));
  expect(await screen.findByRole("alert")).toHaveTextContent("Sign in failed");
  expect(screen.getByLabelText("Username")).toHaveValue("admin");
  expect(screen.getByLabelText("Password")).toHaveAttribute(
    "autocomplete",
    "current-password",
  );
});
