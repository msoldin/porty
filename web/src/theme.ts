export type ThemePreference = "system" | "light" | "dark";

const storageKey = "porty-theme";

export function getThemePreference(): ThemePreference {
  const saved = localStorage.getItem(storageKey);
  return saved === "light" || saved === "dark" ? saved : "system";
}

export function applySavedTheme(): void {
  const preference = getThemePreference();
  if (preference === "system") {
    document.documentElement.removeAttribute("data-theme");
  } else {
    document.documentElement.dataset.theme = preference;
  }
}

export function setThemePreference(preference: ThemePreference): void {
  if (preference === "system") {
    localStorage.removeItem(storageKey);
  } else {
    localStorage.setItem(storageKey, preference);
  }
  applySavedTheme();
}
