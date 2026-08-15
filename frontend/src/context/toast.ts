/**
 * Toast context, types and the `useToast` hook.
 *
 * Split from ToastContext.tsx for the same reason as ./theme.ts: that file exports
 * only components, which is what keeps React Fast Refresh working.
 */

import { createContext, useContext } from "react";
import { AlertCircle, CheckCircle2, Info } from "lucide-react";
import type { LucideIcon } from "lucide-react";

export type ToastVariant = "success" | "error" | "info";

export interface Toast {
    id: number;
    variant: ToastVariant;
    message: string;
}

export interface ToastContextValue {
    success: (message: string) => void;
    error: (message: string) => void;
    info: (message: string) => void;
}

export const ToastContext = createContext<ToastContextValue | null>(null);

/** Errors stay up longer -- they usually need reading, not just noticing. */
export const TOAST_DURATION_MS: Record<ToastVariant, number> = {
    success: 3000,
    info: 3500,
    error: 6000,
};

export const TOAST_VARIANT_STYLES: Record<
    ToastVariant,
    { icon: LucideIcon; classes: string }
> = {
    success: {
        icon: CheckCircle2,
        classes:
            "border-emerald-200 bg-white text-emerald-900 dark:border-emerald-800 dark:bg-slate-900 dark:text-emerald-200",
    },
    error: {
        icon: AlertCircle,
        classes:
            "border-rose-200 bg-white text-rose-900 dark:border-rose-800 dark:bg-slate-900 dark:text-rose-200",
    },
    info: {
        icon: Info,
        classes:
            "border-slate-200 bg-white text-slate-900 dark:border-slate-700 dark:bg-slate-900 dark:text-slate-100",
    },
};

/** Reads the toast API. Throws if used outside a ToastProvider. */
export function useToast(): ToastContextValue {
    const context = useContext(ToastContext);
    if (!context) {
        throw new Error("useToast must be used inside a ToastProvider");
    }
    return context;
}
