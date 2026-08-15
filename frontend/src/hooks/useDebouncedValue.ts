import { useEffect, useState } from "react";

/**
 * Returns `value` delayed by `delayMs`, resetting the timer on every change.
 *
 * The search box is bound to state that updates on every keystroke (so typing
 * feels instant), but the *request* is driven by this debounced copy. Typing
 * "Ananya" therefore fires one query instead of six -- less load on Postgres, and
 * no flicker from six overlapping responses landing out of order.
 */
export function useDebouncedValue<T>(value: T, delayMs = 300): T {
    const [debounced, setDebounced] = useState(value);

    useEffect(() => {
        const timer = setTimeout(() => setDebounced(value), delayMs);

        // Cleanup runs before the next effect, so a keystroke within the delay
        // cancels the pending update and starts the wait again.
        return () => clearTimeout(timer);
    }, [value, delayMs]);

    return debounced;
}
