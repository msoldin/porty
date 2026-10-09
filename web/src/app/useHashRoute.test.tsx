import { act, renderHook } from "@testing-library/preact";
import { expect, it } from "vitest";
import { useHashRoute } from "./useHashRoute";
it("cancels hash navigation without losing editor context and confirms only explicitly", () => {
  history.replaceState(null, "", "#/stacks/one");
  const view = renderHook(() => useHashRoute());
  act(() => view.result.current.setDirty(true));
  act(() => view.result.current.navigate("/repository"));
  expect(view.result.current.route).toBe("/stacks/one");
  expect(view.result.current.pendingRoute).toBe("/repository");
  act(() => view.result.current.cancelNavigation());
  expect(location.hash).toBe("#/stacks/one");
  expect(view.result.current.dirty).toBe(true);
  act(() => view.result.current.navigate("/repository"));
  act(() => view.result.current.confirmNavigation());
  expect(view.result.current.route).toBe("/repository");
  expect(view.result.current.dirty).toBe(false);
});
it("restores the visible hash while browser navigation awaits a decision", () => {
  history.replaceState(null, "", "#/stacks/one");
  const view = renderHook(() => useHashRoute());
  act(() => view.result.current.setDirty(true));
  act(() => {
    location.hash = "/operations";
    window.dispatchEvent(new HashChangeEvent("hashchange"));
  });
  expect(location.hash).toBe("#/stacks/one");
  expect(view.result.current.pendingRoute).toBe("/operations");
  act(() => view.result.current.cancelNavigation());
  expect(view.result.current.route).toBe("/stacks/one");
  const event = new Event("beforeunload", { cancelable: true });
  window.dispatchEvent(event);
  expect(event.defaultPrevented).toBe(true);
});
