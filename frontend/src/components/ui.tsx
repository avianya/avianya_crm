/**
 * Small, unstyled-on-purpose building blocks used across every screen.
 *
 * These exist so that a button looks like a button everywhere without the same
 * twelve Tailwind classes being copy-pasted into fifteen files. When the focus
 * ring or the border radius needs to change, it changes here once.
 */

import type { ButtonHTMLAttributes, InputHTMLAttributes, ReactNode, Ref, SelectHTMLAttributes, TextareaHTMLAttributes } from "react";
import { Loader2 } from "lucide-react";

import { cx } from "../lib/display";

// ---------------------------------------------------------------------------
// Button
// ---------------------------------------------------------------------------

type ButtonVariant = "primary" | "secondary" | "ghost" | "danger";
type ButtonSize = "sm" | "md";

const BUTTON_VARIANTS: Record<ButtonVariant, string> = {
    primary:
        "bg-brand-600 text-white shadow-sm hover:bg-brand-700 active:bg-brand-800 dark:bg-brand-500 dark:hover:bg-brand-400",
    secondary:
        "border border-slate-300 bg-white text-slate-700 shadow-sm hover:bg-slate-50 active:bg-slate-100 dark:border-slate-700 dark:bg-slate-900 dark:text-slate-200 dark:hover:bg-slate-800",
    ghost:
        "text-slate-600 hover:bg-slate-100 hover:text-slate-900 dark:text-slate-400 dark:hover:bg-slate-800 dark:hover:text-slate-100",
    danger:
        "bg-rose-600 text-white shadow-sm hover:bg-rose-700 active:bg-rose-800 dark:bg-rose-600 dark:hover:bg-rose-500",
};

const BUTTON_SIZES: Record<ButtonSize, string> = {
    sm: "h-8 gap-1.5 px-3 text-xs",
    md: "h-10 gap-2 px-4 text-sm",
};

interface ButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
    variant?: ButtonVariant;
    size?: ButtonSize;
    /** Shows a spinner and blocks clicks while an action is in flight. */
    loading?: boolean;
}

export function Button({
    variant = "secondary",
    size = "md",
    loading = false,
    disabled,
    className,
    children,
    ...rest
}: ButtonProps) {
    return (
        <button
            // Buttons inside a <form> default to type="submit", which is a
            // classic source of accidental form submissions. Callers that want
            // to submit pass type="submit" explicitly.
            type="button"
            // A loading button must not be clickable twice -- that is how you get
            // two identical leads from one impatient double-click.
            disabled={disabled ?? loading}
            className={cx(
                "inline-flex items-center justify-center rounded-lg font-medium transition-colors",
                "disabled:pointer-events-none disabled:opacity-50",
                BUTTON_VARIANTS[variant],
                BUTTON_SIZES[size],
                className,
            )}
            {...rest}
        >
            {loading && <Loader2 className="size-4 animate-spin" aria-hidden="true" />}
            {children}
        </button>
    );
}

// ---------------------------------------------------------------------------
// Surfaces
// ---------------------------------------------------------------------------

/** The standard panel: one border, one radius, one background, used everywhere. */
export function Card({
    className,
    children,
}: {
    className?: string;
    children: ReactNode;
}) {
    return (
        <div
            className={cx(
                "rounded-2xl border border-slate-200 bg-white shadow-sm dark:border-slate-800 dark:bg-slate-900",
                className,
            )}
        >
            {children}
        </div>
    );
}

export function CardHeader({
    title,
    subtitle,
    action,
}: {
    title: ReactNode;
    subtitle?: ReactNode;
    action?: ReactNode;
}) {
    return (
        <div className="flex items-start justify-between gap-4 border-b border-slate-200 px-5 py-4 dark:border-slate-800">
            <div className="min-w-0">
                <h2 className="text-sm font-semibold text-slate-900 dark:text-slate-100">
                    {title}
                </h2>
                {subtitle && (
                    <p className="mt-0.5 text-xs text-slate-500 dark:text-slate-400">
                        {subtitle}
                    </p>
                )}
            </div>
            {action}
        </div>
    );
}

/** A rounded pill. `tone` carries the Tailwind colour classes from display.ts. */
export function Badge({
    tone,
    className,
    children,
}: {
    tone: string;
    className?: string;
    children: ReactNode;
}) {
    return (
        <span
            className={cx(
                "inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-xs font-medium ring-1 ring-inset",
                tone,
                className,
            )}
        >
            {children}
        </span>
    );
}

// ---------------------------------------------------------------------------
// Form controls
// ---------------------------------------------------------------------------

/**
 * Wraps a control with its label, optional hint and error message.
 *
 * The htmlFor/id pairing is what lets a click on the label focus the input and
 * lets a screen reader announce which field it is reading. aria-describedby ties
 * the error text to the control so it is announced too, rather than being red
 * text that only sighted users notice.
 */
export function Field({
    id,
    label,
    error,
    hint,
    required,
    children,
}: {
    id: string;
    label: string;
    error?: string;
    hint?: string;
    required?: boolean;
    children: ReactNode;
}) {
    return (
        <div className="space-y-1.5">
            <label
                htmlFor={id}
                className="block text-xs font-medium text-slate-700 dark:text-slate-300"
            >
                {label}
                {required && (
                    <span className="ml-0.5 text-rose-500" aria-hidden="true">
                        *
                    </span>
                )}
            </label>

            {children}

            {error ? (
                <p id={`${id}-error`} className="text-xs text-rose-600 dark:text-rose-400">
                    {error}
                </p>
            ) : (
                hint && (
                    <p id={`${id}-hint`} className="text-xs text-slate-500 dark:text-slate-400">
                        {hint}
                    </p>
                )
            )}
        </div>
    );
}

/** Shared classes for every text-like control, so they line up pixel for pixel. */
const CONTROL_BASE =
    "w-full rounded-lg border bg-white px-3 text-sm text-slate-900 shadow-sm transition-colors placeholder:text-slate-400 focus:border-brand-500 focus:ring-2 focus:ring-brand-500/20 focus:outline-none disabled:cursor-not-allowed disabled:bg-slate-50 dark:bg-slate-950 dark:text-slate-100 dark:placeholder:text-slate-500 dark:disabled:bg-slate-900";

const controlBorder = (invalid?: boolean) =>
    invalid
        ? "border-rose-400 dark:border-rose-600"
        : "border-slate-300 dark:border-slate-700";

interface TextInputProps extends InputHTMLAttributes<HTMLInputElement> {
    invalid?: boolean;
    ref?: Ref<HTMLInputElement>;
}

export function TextInput({ invalid, className, ...rest }: TextInputProps) {
    return (
        <input
            className={cx(CONTROL_BASE, controlBorder(invalid), "h-10", className)}
            // Marks the field invalid for assistive technology, and points at the
            // message Field rendered.
            aria-invalid={invalid || undefined}
            aria-describedby={invalid && rest.id ? `${rest.id}-error` : undefined}
            {...rest}
        />
    );
}

interface SelectProps extends SelectHTMLAttributes<HTMLSelectElement> {
    invalid?: boolean;
}

export function Select({ invalid, className, children, ...rest }: SelectProps) {
    return (
        <select
            className={cx(
                CONTROL_BASE,
                controlBorder(invalid),
                // appearance-none drops the native arrow; select-chevron (defined
                // in index.css) draws the replacement.
                "select-chevron h-10 cursor-pointer appearance-none pr-9",
                className,
            )}
            aria-invalid={invalid || undefined}
            {...rest}
        >
            {children}
        </select>
    );
}

interface TextareaProps extends TextareaHTMLAttributes<HTMLTextAreaElement> {
    invalid?: boolean;
}

export function Textarea({ invalid, className, ...rest }: TextareaProps) {
    return (
        <textarea
            className={cx(CONTROL_BASE, controlBorder(invalid), "py-2.5 leading-relaxed", className)}
            aria-invalid={invalid || undefined}
            aria-describedby={invalid && rest.id ? `${rest.id}-error` : undefined}
            {...rest}
        />
    );
}

// ---------------------------------------------------------------------------
// Loading and empty states
// ---------------------------------------------------------------------------

export function Spinner({ className }: { className?: string }) {
    return (
        <Loader2
            className={cx("size-5 animate-spin text-slate-400", className)}
            aria-hidden="true"
        />
    );
}

/**
 * A grey placeholder block.
 *
 * Skeletons beat a centred spinner because they keep the layout stable: the page
 * does not jump when real content replaces them, and the shape of what is coming
 * is visible while it loads.
 */
export function Skeleton({ className }: { className?: string }) {
    return (
        <div
            className={cx("animate-pulse rounded-md bg-slate-200 dark:bg-slate-800", className)}
            aria-hidden="true"
        />
    );
}

/**
 * The "nothing here" state.
 *
 * Worth designing rather than leaving blank: an empty list is the first thing a
 * new user sees, and it should say what happened and offer the obvious next step.
 */
export function EmptyState({
    icon: Icon,
    title,
    description,
    action,
}: {
    icon: React.ComponentType<{ className?: string }>;
    title: string;
    description?: string;
    action?: ReactNode;
}) {
    return (
        <div className="flex flex-col items-center justify-center px-6 py-14 text-center">
            <div className="mb-4 flex size-12 items-center justify-center rounded-2xl bg-slate-100 text-slate-400 dark:bg-slate-800 dark:text-slate-500">
                <Icon className="size-6" />
            </div>
            <h3 className="text-sm font-semibold text-slate-900 dark:text-slate-100">{title}</h3>
            {description && (
                <p className="mt-1.5 max-w-sm text-sm text-slate-500 dark:text-slate-400">
                    {description}
                </p>
            )}
            {action && <div className="mt-5">{action}</div>}
        </div>
    );
}

/** An inline error panel with a retry affordance. */
export function ErrorState({
    message,
    onRetry,
}: {
    message: string;
    onRetry?: () => void;
}) {
    return (
        <div
            className="rounded-xl border border-rose-200 bg-rose-50 p-4 dark:border-rose-900 dark:bg-rose-950/40"
            role="alert"
        >
            <p className="text-sm font-medium text-rose-900 dark:text-rose-200">
                Something went wrong
            </p>
            <p className="mt-1 text-sm text-rose-700 dark:text-rose-300">{message}</p>
            {onRetry && (
                <Button size="sm" variant="secondary" className="mt-3" onClick={onRetry}>
                    Try again
                </Button>
            )}
        </div>
    );
}

// ---------------------------------------------------------------------------
// Page scaffolding
// ---------------------------------------------------------------------------

export function PageHeader({
    title,
    subtitle,
    action,
}: {
    title: string;
    subtitle?: string;
    action?: ReactNode;
}) {
    return (
        <div className="mb-6 flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
            <div>
                <h1 className="text-2xl font-semibold tracking-tight text-slate-900 dark:text-slate-50">
                    {title}
                </h1>
                {subtitle && (
                    <p className="mt-1 text-sm text-slate-500 dark:text-slate-400">{subtitle}</p>
                )}
            </div>
            {action && <div className="flex shrink-0 gap-2">{action}</div>}
        </div>
    );
}
