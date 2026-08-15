import { useCallback, useMemo, useRef, useState } from "react";
import type { ReactNode } from "react";
import { X } from "lucide-react";

import { cx } from "../lib/display";
import { TOAST_DURATION_MS, TOAST_VARIANT_STYLES, ToastContext } from "./toast";
import type { Toast, ToastContextValue, ToastVariant } from "./toast";

/**
 * Toast notifications.
 *
 * Every write in this app -- create, edit, stage change, delete -- confirms itself
 * with a toast. Without that, a successful save is indistinguishable from a no-op:
 * the user clicks, the dialog closes, and nothing tells them it worked.
 *
 * The types, the context object and the `useToast` hook live in ./toast.ts.
 */
export function ToastProvider({ children }: { children: ReactNode }) {
    const [toasts, setToasts] = useState<Toast[]>([]);

    // A ref, not state: incrementing an id must not trigger a re-render, and the
    // value has to survive between renders without being a module-level global
    // (which would leak between tests).
    const nextId = useRef(1);

    const dismiss = useCallback((id: number) => {
        setToasts((current) => current.filter((toast) => toast.id !== id));
    }, []);

    const push = useCallback(
        (variant: ToastVariant, message: string) => {
            const id = nextId.current++;
            setToasts((current) => [...current, { id, variant, message }]);
            setTimeout(() => dismiss(id), TOAST_DURATION_MS[variant]);
        },
        [dismiss],
    );

    // useMemo keeps the context value referentially stable, so consumers do not
    // re-render every time a toast appears or disappears.
    const value = useMemo<ToastContextValue>(
        () => ({
            success: (message: string) => push("success", message),
            error: (message: string) => push("error", message),
            info: (message: string) => push("info", message),
        }),
        [push],
    );

    return (
        <ToastContext.Provider value={value}>
            {children}

            {/*
              aria-live="polite" makes a screen reader announce new toasts without
              interrupting whatever it is currently reading. A purely visual toast
              is invisible to anyone not looking at that corner of the screen.
            */}
            <div
                className="pointer-events-none fixed inset-x-0 bottom-0 z-100 flex flex-col items-center gap-2 p-4 sm:items-end"
                role="region"
                aria-live="polite"
                aria-label="Notifications"
            >
                {toasts.map((toast) => (
                    <ToastCard key={toast.id} toast={toast} onDismiss={dismiss} />
                ))}
            </div>
        </ToastContext.Provider>
    );
}

function ToastCard({
    toast,
    onDismiss,
}: {
    toast: Toast;
    onDismiss: (id: number) => void;
}) {
    const { icon: Icon, classes } = TOAST_VARIANT_STYLES[toast.variant];

    return (
        <div
            // pointer-events-auto re-enables clicks on the card itself; the
            // container disables them so the toast area never blocks the page.
            className={cx(
                "pointer-events-auto flex w-full max-w-sm items-start gap-3 rounded-xl border p-3.5 shadow-lg shadow-slate-900/5 animate-slide-up dark:shadow-black/30",
                classes,
            )}
        >
            <Icon className="mt-0.5 size-5 shrink-0" aria-hidden="true" />
            <p className="flex-1 text-sm leading-relaxed">{toast.message}</p>
            <button
                type="button"
                onClick={() => onDismiss(toast.id)}
                className="rounded-md p-0.5 text-slate-400 transition-colors hover:bg-slate-100 hover:text-slate-600 dark:hover:bg-slate-800 dark:hover:text-slate-200"
                aria-label="Dismiss notification"
            >
                <X className="size-4" aria-hidden="true" />
            </button>
        </div>
    );
}
