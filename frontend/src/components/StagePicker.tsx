import { useCallback, useEffect, useLayoutEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { Check, ChevronDown, Loader2 } from "lucide-react";

import { STAGE_ORDER, STAGE_STYLES, cx } from "../lib/display";
import type { Stage } from "../lib/types";

/**
 * The inline stage changer used in the leads table and the detail header.
 *
 * A native <select> would have been less code, but it cannot show the stage
 * colours that the rest of the UI relies on, and it looks different on every
 * platform. The tradeoff is that a custom listbox has to earn its keep on the
 * keyboard, so this implements the WAI-ARIA listbox pattern: arrow keys move,
 * Enter/Space selects, Escape closes, Home/End jump, and focus returns to the
 * trigger afterwards.
 *
 * The menu renders through a portal because in the table it would otherwise be
 * clipped by the wrapper's `overflow-x-auto`.
 */
export function StagePicker({
    stage,
    onChange,
    disabled = false,
    busy = false,
    size = "md",
}: {
    stage: Stage;
    onChange: (next: Stage) => void;
    disabled?: boolean;
    /** Shows a spinner while the change is being saved. */
    busy?: boolean;
    size?: "sm" | "md";
}) {
    const [open, setOpen] = useState(false);
    const [activeIndex, setActiveIndex] = useState(() => STAGE_ORDER.indexOf(stage));
    const [position, setPosition] = useState({ top: 0, left: 0, width: 0, flipUp: false });

    const triggerRef = useRef<HTMLButtonElement>(null);
    const menuRef = useRef<HTMLUListElement>(null);

    const style = STAGE_STYLES[stage];
    const Icon = style.icon;

    const close = useCallback(() => {
        setOpen(false);
        triggerRef.current?.focus();
    }, []);

    const select = useCallback(
        (next: Stage) => {
            setOpen(false);
            triggerRef.current?.focus();
            // Only tell the parent when something actually changed -- re-picking
            // the current stage should not fire a request or a toast.
            if (next !== stage) onChange(next);
        },
        [stage, onChange],
    );

    // Measure the trigger and place the menu. useLayoutEffect runs before the
    // browser paints, so the menu never appears at 0,0 for one frame first.
    useLayoutEffect(() => {
        if (!open || !triggerRef.current) return;

        const rect = triggerRef.current.getBoundingClientRect();
        const menuHeight = STAGE_ORDER.length * 36 + 16; // approximate
        const spaceBelow = window.innerHeight - rect.bottom;

        // Open upwards when there is not enough room below -- otherwise rows near
        // the bottom of the page get a menu that runs off the screen.
        const flipUp = spaceBelow < menuHeight && rect.top > spaceBelow;

        setPosition({
            top: flipUp ? rect.top - menuHeight - 4 : rect.bottom + 4,
            left: rect.left,
            width: Math.max(rect.width, 190),
            flipUp,
        });

        setActiveIndex(STAGE_ORDER.indexOf(stage));
    }, [open, stage]);

    // Move focus into the listbox so the arrow-key handler below receives keys
    // straight away, with no extra Tab press.
    //
    // Done explicitly rather than with React's `autoFocus` attribute: autoFocus is
    // only honoured reliably on form controls, and this is a <ul> rendered through
    // a portal. With autoFocus alone the keys went to the trigger button instead,
    // where Enter simply closed the menu again.
    useEffect(() => {
        if (!open) return;
        menuRef.current?.focus();
    }, [open]);

    // Close on an outside click, on scroll and on resize. The last two matter
    // because the menu is absolutely positioned from a measurement that a scroll
    // immediately invalidates.
    useEffect(() => {
        if (!open) return;

        const onPointerDown = (event: MouseEvent) => {
            const target = event.target as Node;
            if (
                !menuRef.current?.contains(target) &&
                !triggerRef.current?.contains(target)
            ) {
                setOpen(false);
            }
        };

        const onReposition = () => setOpen(false);

        document.addEventListener("mousedown", onPointerDown);
        window.addEventListener("scroll", onReposition, true);
        window.addEventListener("resize", onReposition);

        return () => {
            document.removeEventListener("mousedown", onPointerDown);
            window.removeEventListener("scroll", onReposition, true);
            window.removeEventListener("resize", onReposition);
        };
    }, [open]);

    const onTriggerKeyDown = (event: React.KeyboardEvent) => {
        if (open) return;
        // Down/Up/Enter/Space all open the listbox, which is what a keyboard user
        // expects from a combobox-like control.
        if (["ArrowDown", "ArrowUp", "Enter", " "].includes(event.key)) {
            event.preventDefault();
            setOpen(true);
        }
    };

    const onMenuKeyDown = (event: React.KeyboardEvent) => {
        switch (event.key) {
            case "Escape":
                event.preventDefault();
                close();
                break;
            case "ArrowDown":
                event.preventDefault();
                setActiveIndex((index) => Math.min(index + 1, STAGE_ORDER.length - 1));
                break;
            case "ArrowUp":
                event.preventDefault();
                setActiveIndex((index) => Math.max(index - 1, 0));
                break;
            case "Home":
                event.preventDefault();
                setActiveIndex(0);
                break;
            case "End":
                event.preventDefault();
                setActiveIndex(STAGE_ORDER.length - 1);
                break;
            case "Enter":
            case " ":
                event.preventDefault();
                select(STAGE_ORDER[activeIndex]);
                break;
            case "Tab":
                // Tabbing away commits nothing and dismisses the menu.
                setOpen(false);
                break;
        }
    };

    return (
        <>
            <button
                ref={triggerRef}
                type="button"
                disabled={disabled || busy}
                onClick={() => setOpen((isOpen) => !isOpen)}
                onKeyDown={onTriggerKeyDown}
                aria-haspopup="listbox"
                aria-expanded={open}
                aria-label={`Stage: ${style.label}. Change stage`}
                className={cx(
                    "group inline-flex items-center gap-1 rounded-full font-medium ring-1 ring-inset transition-all",
                    "hover:brightness-97 disabled:cursor-not-allowed disabled:opacity-60 dark:hover:brightness-115",
                    style.badge,
                    size === "sm" ? "px-2 py-0.5 text-xs" : "px-2.5 py-1 text-xs",
                )}
            >
                {busy ? (
                    <Loader2 className="size-3 animate-spin" aria-hidden="true" />
                ) : (
                    <Icon className="size-3" aria-hidden="true" />
                )}
                <span className="whitespace-nowrap">{style.label}</span>
                <ChevronDown
                    className={cx(
                        "size-3 opacity-50 transition-transform group-hover:opacity-80",
                        open && "rotate-180",
                    )}
                    aria-hidden="true"
                />
            </button>

            {open &&
                createPortal(
                    <ul
                        ref={menuRef}
                        role="listbox"
                        aria-label="Select stage"
                        // tabIndex={-1} makes the list programmatically focusable
                        // (see the focus effect above) without adding it to the
                        // page's natural tab order.
                        tabIndex={-1}
                        onKeyDown={onMenuKeyDown}
                        style={{
                            top: position.top,
                            left: position.left,
                            minWidth: position.width,
                        }}
                        className={cx(
                            "fixed z-60 max-h-none overflow-hidden rounded-xl border border-slate-200 bg-white p-1.5 shadow-xl shadow-slate-900/10 focus:outline-none dark:border-slate-700 dark:bg-slate-900 dark:shadow-black/40",
                            position.flipUp ? "animate-fade-in" : "animate-scale-in",
                        )}
                    >
                        {STAGE_ORDER.map((option, index) => {
                            const optionStyle = STAGE_STYLES[option];
                            const OptionIcon = optionStyle.icon;
                            const isCurrent = option === stage;

                            return (
                                <li
                                    key={option}
                                    role="option"
                                    aria-selected={isCurrent}
                                    onClick={() => select(option)}
                                    onMouseEnter={() => setActiveIndex(index)}
                                    className={cx(
                                        "flex cursor-pointer items-center gap-2.5 rounded-lg px-2.5 py-1.5 text-sm transition-colors",
                                        index === activeIndex
                                            ? "bg-slate-100 dark:bg-slate-800"
                                            : "hover:bg-slate-50 dark:hover:bg-slate-800/60",
                                    )}
                                >
                                    {/* A small solid dot in the stage colour: the
                                        fastest way to read the list at a glance. */}
                                    <span
                                        className={cx("size-2 shrink-0 rounded-full", optionStyle.solid)}
                                        aria-hidden="true"
                                    />
                                    <OptionIcon
                                        className={cx("size-3.5 shrink-0", optionStyle.text)}
                                        aria-hidden="true"
                                    />
                                    <span
                                        className={cx(
                                            "flex-1 whitespace-nowrap",
                                            isCurrent
                                                ? "font-semibold text-slate-900 dark:text-slate-100"
                                                : "text-slate-700 dark:text-slate-300",
                                        )}
                                    >
                                        {optionStyle.label}
                                    </span>
                                    {isCurrent && (
                                        <Check
                                            className="size-3.5 shrink-0 text-brand-600 dark:text-brand-400"
                                            aria-hidden="true"
                                        />
                                    )}
                                </li>
                            );
                        })}
                    </ul>,
                    document.body,
                )}
        </>
    );
}
