import { expect, it } from "vitest";
import {
  containerStatePresentation,
  deploymentTone,
  remoteTone,
  stackRuntimePresentation,
} from "./statusPresentation";

it("shows every stack runtime with a clear label and tone", () => {
  expect(stackRuntimePresentation("running")).toEqual({
    label: "RUNNING",
    tone: "success",
  });
  expect(stackRuntimePresentation("stopped")).toEqual({
    label: "STOPPED",
    tone: "danger",
  });
  expect(stackRuntimePresentation("partial")).toEqual({
    label: "PARTIALLY RUNNING",
    tone: "warning",
  });
  expect(stackRuntimePresentation("unhealthy")).toEqual({
    label: "UNHEALTHY",
    tone: "danger",
  });
  expect(stackRuntimePresentation(undefined)).toEqual({
    label: "UNKNOWN",
    tone: "neutral",
  });
  expect(stackRuntimePresentation("paused").label).toBe("UNKNOWN");
});

it("keeps Docker health visible beside the container state", () => {
  expect(containerStatePresentation("running", "healthy")).toEqual({
    label: "RUNNING",
    tone: "success",
    health: "Healthy",
    healthTone: "success",
  });
  expect(containerStatePresentation("running", "unhealthy")).toEqual({
    label: "RUNNING",
    tone: "success",
    health: "Unhealthy",
    healthTone: "danger",
  });
  expect(containerStatePresentation("created").label).toBe("STOPPED");
  expect(containerStatePresentation("exited").tone).toBe("danger");
  expect(containerStatePresentation("paused").label).toBe("UNKNOWN");
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
});
