import { expect, it } from "vitest";
import {
  containerStatePresentation,
  deploymentLabel,
  deploymentTone,
  remoteTone,
  stackRuntimePresentation,
} from "./statusPresentation";

it("distinguishes sleeping from failures while keeping real running state", () => {
  expect(stackRuntimePresentation("sleeping")).toEqual({
    label: "Sleeping",
    tone: "blue",
  });
  expect(stackRuntimePresentation("on_demand")).toEqual({
    label: "Running · on demand",
    tone: "blue",
  });
  expect(containerStatePresentation("exited", "unhealthy", true)).toEqual({
    label: "Sleeping",
    tone: "blue",
  });
  expect(
    containerStatePresentation("running", "unhealthy", true).healthTone,
  ).toBe("danger");
  expect(containerStatePresentation("exited", "", false).label).toBe("Stopped");
});

it("shows every stack runtime with a clear label and tone", () => {
  expect(stackRuntimePresentation("running")).toEqual({
    label: "Running",
    tone: "success",
  });
  expect(stackRuntimePresentation("stopped")).toEqual({
    label: "Stopped",
    tone: "neutral",
  });
  expect(stackRuntimePresentation("partial")).toEqual({
    label: "Partially running",
    tone: "warning",
  });
  expect(stackRuntimePresentation("unhealthy")).toEqual({
    label: "Unhealthy",
    tone: "danger",
  });
  expect(stackRuntimePresentation(undefined)).toEqual({
    label: "Unknown",
    tone: "neutral",
  });
  expect(stackRuntimePresentation("paused").label).toBe("Unknown");
});

it("keeps Docker health visible beside the container state", () => {
  expect(containerStatePresentation("running").health).toBe("No health check");
  expect(containerStatePresentation("running", "healthy")).toEqual({
    label: "Running",
    tone: "success",
    health: "Healthy",
    healthTone: "success",
  });
  expect(containerStatePresentation("running", "unhealthy")).toEqual({
    label: "Running",
    tone: "success",
    health: "Unhealthy",
    healthTone: "danger",
  });
  expect(containerStatePresentation("created").label).toBe("Stopped");
  expect(containerStatePresentation("exited").tone).toBe("neutral");
  expect(containerStatePresentation("paused").label).toBe("Unknown");
});

it("maps remote and deployment information onto the same badge palette", () => {
  expect(remoteTone(null)).toBe("neutral");
  expect(remoteTone({ ahead: 0, behind: 0 })).toBe("success");
  expect(remoteTone({ ahead: 1, behind: 0 })).toBe("warning");
  expect(remoteTone({ ahead: 0, behind: 1 })).toBe("danger");
  expect(remoteTone({ ahead: 1, behind: 1 })).toBe("danger");
  expect(deploymentTone("current")).toBe("success");
  expect(deploymentTone("changes_pending")).toBe("warning");
  expect(deploymentTone("deploying")).toBe("blue");
  expect(deploymentTone("never_deployed")).toBe("neutral");
  expect(deploymentTone("unverifiable")).toBe("neutral");
  expect(deploymentLabel("current")).toBe("Current");
  expect(deploymentLabel("changes_pending")).toBe("Changes pending");
  expect(deploymentLabel("never_deployed")).toBe("Never deployed");
  expect(deploymentLabel()).toBe("Unverified");
});
