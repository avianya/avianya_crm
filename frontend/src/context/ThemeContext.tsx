import { useCallback, useEffect, useState } from "react";
import type { ReactNode } from "react";

import {
    THEME_STORAGE_KEY,
    ThemeContext,
    readStoredPreference,
    systemPrefersDark,
} from "./theme";
import type { ThemePreference } from "./theme";

/**
 * Owns the dark-mode state and the `dark` class on <html>.
 *
 * The types, the context object and the `useTheme` hook live in ./theme.ts so this
 * file exports only a component -- see the note at the top of that file.
 */
export function ThemeProvider({ children }: { children: ReactNode }) {
    const [preference, setPreferenceState] =
        useState<ThemePreference>(readStoredPreference);
    const [systemIsDark, setSystemIsDark] = useState(systemPrefersDark);

    // Track the OS setting so a "system" preference reacts live when the user
    // changes their theme while the app is open.
    useEffect(() => {
        const query = window.matchMedia("(prefers-color-scheme: dark)");
        const onChange = (event: MediaQueryListEvent) => setSystemIsDark(event.matches);

        query.addEventListener("change", onChange);
        return () => query.removeEventListener("change", onChange);
    }, []);

    const resolved: "light" | "dark" =
        preference === "system" ? (systemIsDark ? "dark" : "light") : preference;

    // The one place the DOM is touched: index.css defines `dark:` as "inside
    // .dark", so toggling this single class re-themes the entire app.
    useEffect(() => {
        document.documentElement.classList.toggle("dark", resolved === "dark");
    }, [resolved]);

    const setPreference = useCallback((next: ThemePreference) => {
        setPreferenceState(next);
        try {
            localStorage.setItem(THEME_STORAGE_KEY, next);
        } catch {
            // Not being able to persist is not worth failing over.
        }
    }, []);

    const toggle = useCallback(
        () => setPreference(resolved === "dark" ? "light" : "dark"),
        [resolved, setPreference],
    );

    return (
        <ThemeContext.Provider value={{ preference, resolved, setPreference, toggle }}>
            {children}
        </ThemeContext.Provider>
    );
}
