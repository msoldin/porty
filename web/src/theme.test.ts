import { beforeEach, describe, expect, it } from "vitest";
import {
  getThemePreference,
  setThemePreference,
  applySavedTheme,
} from "./theme";

describe("appearance preference", () => {
  beforeEach(() => {
    localStorage.clear();
    document.documentElement.removeAttribute("data-theme");
  });

  it("follows the operating system when no preference is saved", () => {
    applySavedTheme();
    expect(getThemePreference()).toBe("system");
    expect(document.documentElement).not.toHaveAttribute("data-theme");
  });

  it("persists a manual choice and restores it on the next load", () => {
    setThemePreference("dark");
    document.documentElement.removeAttribute("data-theme");
    applySavedTheme();
    expect(getThemePreference()).toBe("dark");
    expect(document.documentElement).toHaveAttribute("data-theme", "dark");
  });

  it("returns to the system theme when the override is cleared", () => {
    setThemePreference("light");
    setThemePreference("system");
    expect(localStorage.getItem("porty-theme")).toBeNull();
    expect(document.documentElement).not.toHaveAttribute("data-theme");
  });

  it("ignores invalid stored preferences", () => {
    localStorage.setItem("porty-theme", "unknown");
    applySavedTheme();
    expect(getThemePreference()).toBe("system");
    expect(document.documentElement).not.toHaveAttribute("data-theme");
  });
});
