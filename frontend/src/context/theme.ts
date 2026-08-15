/**
 * Theme context, types and the `useTheme` hook.
 *
 * Split from ThemeContext.tsx so that file exports *only* the provider component.
 * React Fast Refresh can only hot-reload a module whose exports are all
 * components; mixing a hook in with one silently degrades hot reloading to a full
 * page refresh, which the `react/only-export-components` lint rule warns about.
 */

import { createContext, useContext } from "react";

/**
 * Three states rather than two: "system" is the default and follows the OS, and
 * only an explicit click pins the app to light or dark.
 *
 * That distinction matters -- someone with their laptop set to dark at night
 * expects the app to follow unless they have said otherwise, and a two-state
 * toggle cannot express it.
 */
export type ThemePreference = "light" | "dark" | "system";

export interface ThemeContextValue {
    /** What the user chose. */
    preference: ThemePreference;
    /** What is actually on screen, once "system" has been resolved. */
    resolved: "light" | "dark";
    setPreference: (preference: ThemePreference) => void;
    /** Flips between light and dark, leaving "system" behind. */
    toggle: () => void;
}

/** Null until a ThemeProvider mounts, which is what useTheme checks for. */
export const ThemeContext = createContext<ThemeContextValue | null>(null);

/** localStorage key holding the saved preference. */
export const THEME_STORAGE_KEY = "crm-theme";

/**
 * Reads the saved preference, defaulting to "system".
 *
 * localStorage can throw in some private-browsing modes, so it is never trusted
 * to succeed.
 */
export function readStoredPreference(): ThemePreference {
    try {
        const stored = localStorage.getItem(THEME_STORAGE_KEY);
        if (stored === "light" || stored === "dark" || stored === "system") {
            return stored;
        }
    } catch {
        // Ignore and fall through to the default.
    }
    return "system";
}

/** Whether the operating system currently prefers a dark colour scheme. */
export const systemPrefersDark = (): boolean =>
    window.matchMedia("(prefers-color-scheme: dark)").matches;

/**
 * Reads the theme.
 *
 * Throws if used outside a ThemeProvider, which turns a silent "why is the toggle
 * dead" bug into an immediate, obvious error.
 */
export function useTheme(): ThemeContextValue {
    const context = useContext(ThemeContext);
    if (!context) {
        throw new Error("useTheme must be used inside a ThemeProvider");
    }
    return context;
}
