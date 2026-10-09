import { render, screen } from "@testing-library/preact";
import { expect, it } from "vitest";
import { MetricGauge } from "./MetricGauge";

it("renders partial utilization with browser-recognized SVG dash attributes", () => {
  const view = render(<MetricGauge label="CPU usage" value={25} />);
  expect(view.container.querySelector(".gauge-activity")).toHaveAttribute(
    "stroke-dasharray",
    "25 100",
  );
  view.rerender(<MetricGauge label="Disk fullness" value={25} capacity />);
  expect(view.container.querySelector(".gauge-warning")).toHaveAttribute(
    "stroke-dashoffset",
    "-70",
  );
});

it("exposes the percentage and distinguishes capacity warnings from utilization", () => {
  const view = render(<MetricGauge label="CPU usage" value={99} />);
  expect(screen.getByRole("meter")).toHaveAttribute("aria-valuenow", "99");
  expect(screen.getByRole("meter")).toHaveAttribute("aria-valuetext", "99%");
  view.rerender(<MetricGauge label="/ disk fullness" value={99} capacity />);
  expect(screen.getByRole("meter")).toHaveAttribute(
    "aria-valuetext",
    "99% used · Almost full",
  );
});
it("does not present an unavailable reading as an empty disk", () => {
  render(<MetricGauge label="/ disk fullness" value={null} capacity />);
  expect(screen.queryByRole("meter")).not.toBeInTheDocument();
  expect(screen.getByText("—")).toBeVisible();
  expect(screen.queryByText("0%")).not.toBeInTheDocument();
});
