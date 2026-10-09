import { render, screen } from "@testing-library/preact";
import { expect, it } from "vitest";
import { MetricAvailability } from "./MetricAvailability";

it("retains a permission explanation when other devices have incomplete readings", () => {
  render(
    <MetricAvailability
      sources={["gpu", "device-123"]}
      coverage={[
        {
          source: "gpu",
          partial: true,
          omitted: 1,
          reason: "partial_coverage",
        },
      ]}
      reading={{
        value: null,
        state: "unavailable",
        reason: "permission_denied",
        sampledAt: "2026-10-09T00:00:00Z",
      }}
    />,
  );
  expect(
    screen.getByText("Porty does not have permission to read this metric."),
  ).toBeVisible();
  expect(screen.getByText(/At least 1 device isn’t shown/)).toBeVisible();
});
it("does not repeat a device failure or reveal internal identifiers", () => {
  render(
    <MetricAvailability
      sources={["device-123"]}
      coverage={[
        {
          source: "device-123",
          partial: true,
          omitted: 0,
          reason: "permission_denied",
        },
      ]}
      reading={{
        value: null,
        state: "unavailable",
        reason: "permission_denied",
        sampledAt: "2026-10-09T00:00:00Z",
      }}
    />,
  );
  expect(
    screen.getAllByText("Porty does not have permission to read this metric."),
  ).toHaveLength(1);
  expect(screen.queryByText(/device-123/)).not.toBeInTheDocument();
});
it("keeps unrelated sensor notices out of GPU details", () => {
  const view = render(
    <MetricAvailability
      sources={["gpu"]}
      coverage={[
        { source: "sensors", partial: true, omitted: 0, reason: "no_device" },
      ]}
    />,
  );
  expect(view.container).toBeEmptyDOMElement();
});
