import { useRef } from "react";
import type { PointerEvent, ReactNode } from "react";

import { cx } from "../lib/display";

const BRAND = "99 102 241"; // brand-500, as rgb for the glow and ripple

function reducedMotion(): boolean {
    return window.matchMedia("(prefers-reduced-motion: reduce)").matches;
}

/**
 * Brings a card to life:
 * - it slides in on arrival, `index` cards after the first;
 * - on hover it lifts with a glow, tilts gently towards the pointer and a
 *   shine sweeps across it;
 * - on click it presses and springs back, with a ripple from the click and
 *   a ring of light.
 * Clicks in a dialog the card opened (rendered elsewhere in the page) do not
 * count. Nothing moves for anyone who asked their system to reduce motion.
 */
export function PopOnClick({ children, className, index = 0 }: { children: ReactNode; className?: string; index?: number }) {
    const ref = useRef<HTMLDivElement>(null);

    const press = (e: PointerEvent<HTMLDivElement>) => {
        const el = ref.current;
        // React passes on events from portals; only clicks on the card itself.
        if (!el || !el.contains(e.target as Node) || !el.animate || reducedMotion()) return;

        el.animate(
            [
                { transform: "scale(1)", boxShadow: `0 0 0 0 rgb(${BRAND} / 0.45)` },
                { transform: "scale(0.97)", offset: 0.25 },
                { transform: "scale(1.015)", boxShadow: `0 0 0 7px rgb(${BRAND} / 0.18)`, offset: 0.6 },
                { transform: "scale(1)", boxShadow: `0 0 0 12px rgb(${BRAND} / 0)` },
            ],
            { duration: 450, easing: "cubic-bezier(0.16, 1, 0.3, 1)" },
        );

        // A ripple spreading from where the pointer went down.
        const box = el.getBoundingClientRect();
        const size = Math.hypot(box.width, box.height) * 2;
        const ripple = document.createElement("span");
        ripple.setAttribute("aria-hidden", "true");
        Object.assign(ripple.style, {
            position: "absolute",
            left: `${e.clientX - box.left - size / 2}px`,
            top: `${e.clientY - box.top - size / 2}px`,
            width: `${size}px`,
            height: `${size}px`,
            borderRadius: "9999px",
            background: `radial-gradient(circle, rgb(${BRAND} / 0.22) 0%, rgb(${BRAND} / 0.08) 40%, transparent 70%)`,
            pointerEvents: "none",
            zIndex: "1",
        });
        el.appendChild(ripple);
        ripple
            .animate([{ transform: "scale(0)", opacity: 1 }, { transform: "scale(1)", opacity: 0 }], {
                duration: 650,
                easing: "cubic-bezier(0.22, 1, 0.36, 1)",
            })
            .finished.finally(() => ripple.remove());
    };

    // A gentle tilt towards the pointer, at most a couple of degrees.
    const tilt = (e: PointerEvent<HTMLDivElement>) => {
        const el = ref.current;
        if (!el || e.pointerType !== "mouse" || reducedMotion()) return;
        const box = el.getBoundingClientRect();
        const x = (e.clientX - box.left) / box.width - 0.5;
        const y = (e.clientY - box.top) / box.height - 0.5;
        el.style.setProperty("--ry", `${(x * 3).toFixed(2)}deg`);
        el.style.setProperty("--rx", `${(-y * 3).toFixed(2)}deg`);
    };
    const untilt = () => {
        ref.current?.style.setProperty("--ry", "0deg");
        ref.current?.style.setProperty("--rx", "0deg");
    };

    return (
        // Arrival is on this outer box, so its finished animation does not
        // hold the transform the hover and click effects use below.
        <div className={cx("animate-card-in", className)} style={{ animationDelay: `${index * 110}ms` }}>
            <div
                ref={ref}
                onPointerDown={press}
                onPointerMove={tilt}
                onPointerLeave={untilt}
                className={cx(
                    "group/pop relative isolate overflow-hidden rounded-2xl",
                    "[transform:perspective(1000px)_rotateX(var(--rx,0deg))_rotateY(var(--ry,0deg))_translateY(var(--lift,0px))]",
                    "transition-[transform,box-shadow] duration-300 ease-out",
                    "hover:[--lift:-4px] hover:shadow-xl hover:shadow-brand-500/15 dark:hover:shadow-brand-500/10",
                )}
            >
                {children}
                {/* The shine: a soft band of light that sweeps across on hover. */}
                <span
                    aria-hidden="true"
                    className={cx(
                        "pointer-events-none absolute inset-y-0 -left-1/2 z-[1] w-1/2 -skew-x-12",
                        "bg-linear-to-r from-transparent via-white/40 to-transparent dark:via-white/10",
                        "translate-x-0 opacity-0 transition-[translate,opacity] duration-700 ease-out",
                        "group-hover/pop:translate-x-[400%] group-hover/pop:opacity-100",
                    )}
                />
            </div>
        </div>
    );
}
