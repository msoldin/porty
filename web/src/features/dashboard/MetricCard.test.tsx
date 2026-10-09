import { fireEvent, render, screen, within } from "@testing-library/preact";
import { expect, it } from "vitest";
import { MetricCard } from "./MetricCard";

it("keeps unavailable details focused on the explanation instead of a placeholder value", () => {
  render(
    <MetricCard
      label="Temperature"
      value="—"
      state="unavailable"
      availability={
        <p>No temperature sensors are exposed to this environment.</p>
      }
    />,
  );
  fireEvent.click(screen.getByRole("button", { name: "Temperature details" }));
  const dialog = within(screen.getByRole("dialog"));
  expect(dialog.queryByText("—")).not.toBeInTheDocument();
  expect(
    dialog.getByText("No temperature sensors are exposed to this environment."),
  ).toBeVisible();
});
it("keeps a zero reading visible in details", () => {
  render(<MetricCard label="Download" value="0 B/s" state="available" />);
  fireEvent.click(screen.getByRole("button", { name: "Download details" }));
  expect(screen.getByRole("dialog")).toHaveTextContent("0 B/s");
});
