import { useCallback, useEffect, useRef, useState } from "react";

/**
 * State of one in-flight or settled async call.
 *
 * `data` is kept while a refetch is running so the UI can show the previous
 * result dimmed instead of flashing back to a skeleton on every keystroke.
 */
interface AsyncState<T> {
    data: T | null;
    error: Error | null;
    /** True on the very first load, when there is nothing to show yet. */
    loading: boolean;
    /** True whenever a request is in flight, including refetches. */
    fetching: boolean;
}

interface AsyncResult<T> extends AsyncState<T> {
    /** Re-runs the request, e.g. after a mutation or from a "Retry" button. */
    refetch: () => void;
    /**
     * Replaces the cached data locally, without a request.
     *
     * Used after a mutation whose response already contains the updated record:
     * the server has told us the new state, so re-fetching it would be a wasted
     * round trip and a visible flicker.
     */
    setData: (updater: T | ((current: T | null) => T)) => void;
}

/**
 * Runs an async function and tracks loading/error/data, cancelling correctly.
 *
 * The AbortSignal handed to `fn` is aborted when the dependencies change or the
 * component unmounts. That solves two real bugs at once:
 *
 *  - the race where a slow response for "an" arrives after the fast one for
 *    "anan" and overwrites the newer results with older ones;
 *  - the React warning (and memory leak) from setting state after unmount.
 *
 * `deps` works like the dependency array of useEffect: the request re-runs
 * whenever one of them changes.
 */
export function useAsync<T>(
    fn: (signal: AbortSignal) => Promise<T>,
    deps: React.DependencyList,
): AsyncResult<T> {
    const [state, setState] = useState<AsyncState<T>>({
        data: null,
        error: null,
        loading: true,
        fetching: true,
    });

    // Bumping this forces the effect to re-run, which is how refetch works
    // without needing the caller to memoise `fn`.
    const [reloadToken, setReloadToken] = useState(0);

    // `fn` is usually a fresh arrow function on every render, so it cannot go in
    // the dependency array without causing an infinite loop. Holding the latest
    // version in a ref lets the effect always call current code while re-running
    // only when `deps` actually change.
    const fnRef = useRef(fn);
    fnRef.current = fn;

    useEffect(() => {
        const controller = new AbortController();

        setState((current) => ({ ...current, fetching: true, error: null }));

        fnRef
            .current(controller.signal)
            .then((data) => {
                if (controller.signal.aborted) return;
                setState({ data, error: null, loading: false, fetching: false });
            })
            .catch((error: unknown) => {
                // An abort is a cancellation we caused on purpose, not a failure
                // worth showing the user.
                if (controller.signal.aborted) return;
                setState((current) => ({
                    data: current.data,
                    error: error instanceof Error ? error : new Error(String(error)),
                    loading: false,
                    fetching: false,
                }));
            });

        // The cleanup function runs before the next effect and on unmount.
        return () => controller.abort();

        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [...deps, reloadToken]);

    const refetch = useCallback(() => setReloadToken((token) => token + 1), []);

    const setData = useCallback((updater: T | ((current: T | null) => T)) => {
        setState((current) => ({
            ...current,
            data:
                typeof updater === "function"
                    ? (updater as (value: T | null) => T)(current.data)
                    : updater,
        }));
    }, []);

    return { ...state, refetch, setData };
}
