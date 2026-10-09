import { fireEvent, render, screen, within } from "@testing-library/preact";
import { expect, it } from "vitest";
import { MetricCard } from "./MetricCard";
import { MetricChart } from "./MetricChart";

it("opens a labeled history graph while keeping the card chart compact", () => {
  render(
    <MetricCard
      label="CPU"
      value="25%"
      state="available"
      chart={
        <MetricChart
          unit="percent"
          series={[{ label: "CPU", points: [{ time: 300000, value: 25 }] }]}
        />
      }
    />,
  );
  expect(screen.queryByText("100%")).not.toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "CPU details" }));
  const dialog = within(screen.getByRole("dialog"));
  expect(dialog.getByRole("heading", { name: "History" })).toBeVisible();
  expect(dialog.getByText("Last 5 minutes")).toBeVisible();
  expect(dialog.getByText("100%")).toBeVisible();
  expect(dialog.getByText("0%")).toBeVisible();
  const chart = dialog.getByRole("group", { name: "CPU history" });
  fireEvent.focus(chart);
  expect(dialog.getByRole("status")).toHaveTextContent("CPU: 25%");
});

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
