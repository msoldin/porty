import { fireEvent, render, screen } from "@testing-library/preact";
import { expect, it } from "vitest";
import { MetricChart } from "./MetricChart";
it("keeps a tapped reading visible after the finger lifts and dismisses it on blur", () => {
  render(
    <MetricChart
      unit="bytes_per_second"
      series={[{ label: "Download", points: [{ time: 300000, value: 1000 }] }]}
    />,
  );
  const chart = screen.getByRole("group", { name: "Download history" });
  fireEvent(
    chart,
    new MouseEvent("pointerdown", { clientX: 0, bubbles: true }),
  );
  const leave = new MouseEvent("pointerleave", { bubbles: true });
  Object.defineProperty(leave, "pointerType", { value: "touch" });
  fireEvent(chart, leave);
  expect(screen.getByRole("status")).toHaveTextContent("Download: 1 kB/s");
  fireEvent.blur(chart);
  expect(screen.queryByRole("status")).not.toBeInTheDocument();
});
it("hides inspection over missing readings while preserving real zero values", () => {
  render(
    <MetricChart
      unit="bytes_per_second"
      series={[
        {
          label: "Download",
          points: [
            { time: 0, value: 1000 },
            { time: 150000, value: null },
            { time: 300000, value: 0 },
          ],
        },
      ]}
    />,
  );
  const chart = screen.getByRole("group", { name: "Download history" });
  chart.getBoundingClientRect = () => ({ left: 0, width: 300 }) as DOMRect;
  fireEvent(
    chart,
    new MouseEvent("pointermove", { clientX: 0, bubbles: true }),
  );
  expect(screen.getByRole("status")).toHaveTextContent("Download: 1 kB/s");
  fireEvent(
    chart,
    new MouseEvent("pointermove", { clientX: 150, bubbles: true }),
  );
  expect(screen.queryByRole("status")).not.toBeInTheDocument();
  fireEvent(
    chart,
    new MouseEvent("pointermove", { clientX: 300, bubbles: true }),
  );
  expect(screen.getByRole("status")).toHaveTextContent("Download: 0 B/s");
});
it("does not open empty inspection on keyboard focus or touch", () => {
  render(
    <MetricChart
      unit="bytes_per_second"
      series={[{ label: "Download", points: [{ time: 300000, value: null }] }]}
    />,
  );
  const chart = screen.getByRole("group", { name: "Download history" });
  fireEvent.focus(chart);
  expect(screen.queryByRole("status")).not.toBeInTheDocument();
  fireEvent.pointerDown(chart, { clientX: 0 });
  expect(screen.queryByRole("status")).not.toBeInTheDocument();
});
it("shows the plotted temperature range without requiring inspection", () => {
  render(
    <MetricChart
      unit="celsius"
      series={[
        {
          label: "Temperature",
          points: [
            { time: 1000, value: 52 },
            { time: 3000, value: 68 },
          ],
        },
      ]}
    />,
  );
  expect(screen.getByText("Range: 51 °C–68 °C")).toBeVisible();
});
it("makes chart samples available with keyboard and touch", () => {
  const series = [
    {
      label: "CPU",
      points: [
        { time: 1000, value: 20 },
        { time: 3000, value: null },
        { time: 5000, value: 80 },
      ],
    },
  ];
  render(<MetricChart series={series} unit="percent" />);
  const chart = screen.getByRole("group", { name: "CPU history" });
  fireEvent.focus(chart);
  fireEvent.keyDown(chart, { key: "Home" });
  expect(screen.getByText(/CPU: 20%/)).toBeVisible();
  fireEvent.keyDown(chart, { key: "End" });
  expect(screen.getByText(/CPU: 80%/)).toBeVisible();
  fireEvent.pointerDown(chart, { clientX: 0 });
  expect(screen.getByRole("status")).toBeVisible();
});
it("does not connect a line across unavailable samples", () => {
  const { container } = render(
    <MetricChart
      series={[
        {
          label: "CPU",
          points: [
            { time: 0, value: 10 },
            { time: 2000, value: 20 },
            { time: 4000, value: null },
            { time: 6000, value: 40 },
            { time: 8000, value: 50 },
          ],
        },
      ]}
      unit="percent"
    />,
  );
  expect(container.querySelectorAll("polyline")).toHaveLength(2);
});
