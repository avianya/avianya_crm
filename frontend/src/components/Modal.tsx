import { useCallback, useEffect, useRef } from "react";
import { createPortal } from "react-dom";
import { X } from "lucide-react";

import { cx } from "../lib/display";

/**
 * A modal dialog.
 *
 * Written by hand rather than pulled from a library, but not naively: a dialog
 * that is only a floating div is a genuine accessibility problem. This one does
 * the four things that matter --
 *
 *   1. Escape closes it, and a click on the backdrop does too;
 *   2. focus moves into the dialog on open and returns to the trigger on close;
 *   3. Tab is trapped inside, so keyboard users cannot wander into the page
 *      behind an overlay they cannot see;
 *   4. the page behind does not scroll while it is open.
 */
export function Modal({
    open,
    onClose,
    title,
    description,
    children,
    size = "md",
}: {
    open: boolean;
    onClose: () => void;
    title: string;
    description?: string;
    children: React.ReactNode;
    size?: "md" | "lg";
}) {
    const panelRef = useRef<HTMLDivElement>(null);

    // Remembers what was focused before opening, so focus can be handed back.
    const previouslyFocused = useRef<HTMLElement | null>(null);

    const focusableSelector =
        'button:not([disabled]), [href], input:not([disabled]):not([type="hidden"]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])';

    const handleKeyDown = useCallback(
        (event: KeyboardEvent) => {
            if (event.key === "Escape") {
                event.preventDefault();
                onClose();
                return;
            }

            if (event.key !== "Tab" || !panelRef.current) return;

            const focusable = Array.from(
                panelRef.current.querySelectorAll<HTMLElement>(focusableSelector),
            );
            if (focusable.length === 0) return;

            const first = focusable[0];
            const last = focusable[focusable.length - 1];

            // Wrap around at both ends: this is the actual "trap". Without it,
            // Tab past the last control silently moves focus to the browser
            // chrome or the page underneath.
            if (event.shiftKey && document.activeElement === first) {
                event.preventDefault();
                last.focus();
            } else if (!event.shiftKey && document.activeElement === last) {
                event.preventDefault();
                first.focus();
            }
        },
        [onClose, focusableSelector],
    );

    useEffect(() => {
        if (!open) return;

        previouslyFocused.current = document.activeElement as HTMLElement | null;

        // Stop the page behind from scrolling under the overlay.
        const originalOverflow = document.body.style.overflow;
        document.body.style.overflow = "hidden";

        document.addEventListener("keydown", handleKeyDown);

        // Move focus in. requestAnimationFrame waits for the panel to be painted;
        // focusing an element that is not laid out yet silently does nothing.
        const frame = requestAnimationFrame(() => {
            const firstField = panelRef.current?.querySelector<HTMLElement>(
                'input:not([type="hidden"]), select, textarea',
            );
            (firstField ?? panelRef.current)?.focus();
        });

        return () => {
            document.removeEventListener("keydown", handleKeyDown);
            document.body.style.overflow = originalOverflow;
            cancelAnimationFrame(frame);

            // Hand focus back to whatever opened the dialog, so keyboard users
            // do not get dumped at the top of the document.
            previouslyFocused.current?.focus();
        };
    }, [open, handleKeyDown]);

    if (!open) return null;

    // A portal renders this at the end of <body> instead of inside the component
    // tree, so no ancestor's overflow:hidden or transform can clip the overlay.
    return createPortal(
        <div className="fixed inset-0 z-50 flex items-end justify-center p-0 sm:items-center sm:p-4">
            {/* Backdrop. aria-hidden because the visible dialog is the content;
                the click handler gives the conventional "click outside to close". */}
            <div
                className="absolute inset-0 bg-slate-900/40 backdrop-blur-sm animate-fade-in dark:bg-black/60"
                onClick={onClose}
                aria-hidden="true"
            />

            <div
                ref={panelRef}
                role="dialog"
                aria-modal="true"
                aria-labelledby="modal-title"
                aria-describedby={description ? "modal-description" : undefined}
                tabIndex={-1}
                className={cx(
                    "relative flex max-h-[92vh] w-full flex-col overflow-hidden rounded-t-2xl border border-slate-200 bg-white shadow-2xl animate-scale-in sm:rounded-2xl dark:border-slate-800 dark:bg-slate-900",
                    size === "lg" ? "sm:max-w-2xl" : "sm:max-w-lg",
                )}
            >
                <div className="flex items-start justify-between gap-4 border-b border-slate-200 px-5 py-4 dark:border-slate-800">
                    <div className="min-w-0">
                        <h2
                            id="modal-title"
                            className="text-base font-semibold text-slate-900 dark:text-slate-50"
                        >
                            {title}
                        </h2>
                        {description && (
                            <p
                                id="modal-description"
                                className="mt-0.5 text-sm text-slate-500 dark:text-slate-400"
                            >
                                {description}
                            </p>
                        )}
                    </div>

                    <button
                        type="button"
                        onClick={onClose}
                        className="-mr-1 rounded-lg p-1.5 text-slate-400 transition-colors hover:bg-slate-100 hover:text-slate-600 dark:hover:bg-slate-800 dark:hover:text-slate-200"
                        aria-label="Close dialog"
                    >
                        <X className="size-5" aria-hidden="true" />
                    </button>
                </div>

                {/* The body scrolls, not the whole dialog, so the header and any
                    footer inside `children` stay put on a short screen. */}
                <div className="scrollbar-thin flex-1 overflow-y-auto">{children}</div>
            </div>
        </div>,
        document.body,
    );
}

/**
 * The padded content area of a dialog. Modal itself adds no padding, so a
 * form can put a full-width footer under its fields; every form uses this and
 * ModalFooter so they all line up.
 */
export function ModalBody({ className, children }: { className?: string; children: React.ReactNode }) {
    return <div className={cx("space-y-4 px-5 py-5", className)}>{children}</div>;
}

/** The dialog's button bar: full width, grey, buttons on the right. */
export function ModalFooter({ className, children }: { className?: string; children: React.ReactNode }) {
    return (
        <div
            className={cx(
                "flex flex-wrap items-center justify-end gap-2 border-t border-slate-200 bg-slate-50 px-5 py-3.5 dark:border-slate-800 dark:bg-slate-950/50",
                className,
            )}
        >
            {children}
        </div>
    );
}
