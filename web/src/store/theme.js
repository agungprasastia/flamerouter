import { create } from "zustand";

const storageKey = "flamerouter-theme";
const themes = ["light", "dark", "system"];

function systemTheme() {
  if (typeof window === "undefined") return "light";
  return window.matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light";
}

function applyTheme(theme) {
  const resolved = theme === "system" ? systemTheme() : theme;
  document.documentElement.classList.toggle("dark", resolved === "dark");
  return resolved;
}

export const useTheme = create((set, get) => ({
  theme: typeof window === "undefined" ? "system" : localStorage.getItem(storageKey) || "system",
  resolvedTheme: "light",
  setTheme: (theme) => {
    localStorage.setItem(storageKey, theme);
    const resolvedTheme = applyTheme(theme);
    set({ theme, resolvedTheme });
  },
  cycleTheme: () => {
    const current = get().theme;
    const next = themes[(themes.indexOf(current) + 1) % themes.length];
    get().setTheme(next);
  },
  syncTheme: () => {
    const theme = get().theme;
    set({ resolvedTheme: applyTheme(theme) });
  },
}));
