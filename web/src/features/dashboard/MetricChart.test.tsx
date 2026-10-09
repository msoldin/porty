import { fireEvent, render, screen } from "@testing-library/preact";
import { expect, it } from "vitest";
import { MetricChart } from "./MetricChart";
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
