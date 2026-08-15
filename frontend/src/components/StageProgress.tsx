import { Check } from "lucide-react";

import {
    PIPELINE_STAGES,
    STAGE_STYLES,
    cx,
    isClosedStage,
    pipelineIndex,
} from "../lib/display";
import type { Stage } from "../lib/types";

/**
 * The step indicator on the lead detail page.
 *
 * Shows the five open stages as a track, filled up to wherever the lead has
 * reached, so "how far along is this deal" is answered by a glance at a shape
 * rather than by reading a word. Each step is a button, which doubles as the
 * fastest way to move a lead: click the step you want.
 *
 * Won and Lost are handled separately rather than as a sixth and seventh step.
 * They are outcomes, not progress -- a lost lead did not "reach the end", and
 * drawing it as a completed track would be actively misleading.
 */
export function StageProgress({
    stage,
    onSelect,
    disabled = false,
}: {
    stage: Stage;
    onSelect?: (stage: Stage) => void;
    disabled?: boolean;
}) {
    const closed = isClosedStage(stage);
    const currentIndex = pipelineIndex(stage);

    // A won deal has been through the whole pipeline, so every step reads as
    // complete. A lost one is shown with the track greyed out from where it died.
    const filledThrough = stage === "won" ? PIPELINE_STAGES.length - 1 : currentIndex;

    return (
        <div className="space-y-4">
            <ol className="flex items-start" role="list">
                {PIPELINE_STAGES.map((pipelineStage, index) => {
                    const style = STAGE_STYLES[pipelineStage];
                    const Icon = style.icon;

                    const isComplete = index < filledThrough || stage === "won";
                    const isCurrent = pipelineStage === stage;
                    const isUpcoming = !isComplete && !isCurrent;

                    return (
                        <li
                            key={pipelineStage}
                            className={cx(
                                "relative flex flex-1 flex-col items-center gap-2",
                                // The connector is drawn as a pseudo-element on
                                // each step except the first, so it always spans
                                // exactly the gap between two circles.
                                index > 0 &&
                                    "before:absolute before:top-4 before:right-1/2 before:left-0 before:h-0.5 before:-translate-y-1/2",
                                index > 0 &&
                                    (isComplete || isCurrent
                                        ? "before:bg-brand-500"
                                        : "before:bg-slate-200 dark:before:bg-slate-700"),
                            )}
                        >
                            <button
                                type="button"
                                disabled={disabled || !onSelect}
                                onClick={() => onSelect?.(pipelineStage)}
                                aria-current={isCurrent ? "step" : undefined}
                                aria-label={
                                    isCurrent
                                        ? `Current stage: ${style.label}`
                                        : `Move to ${style.label}`
                                }
                                title={onSelect ? `Move to ${style.label}` : style.label}
                                className={cx(
                                    "relative z-10 flex size-8 items-center justify-center rounded-full border-2 transition-all duration-300",
                                    onSelect && !disabled
                                        ? "cursor-pointer hover:scale-110"
                                        : "cursor-default",
                                    isComplete &&
                                        "border-brand-500 bg-brand-500 text-white",
                                    isCurrent &&
                                        cx(
                                            "border-brand-500 bg-white text-brand-600 ring-4 ring-brand-500/20 dark:bg-slate-900 dark:text-brand-400",
                                        ),
                                    isUpcoming &&
                                        "border-slate-200 bg-white text-slate-400 dark:border-slate-700 dark:bg-slate-900 dark:text-slate-500",
                                )}
                            >
                                {isComplete ? (
                                    <Check className="size-4" aria-hidden="true" />
                                ) : (
                                    <Icon className="size-4" aria-hidden="true" />
                                )}
                            </button>

                            <span
                                className={cx(
                                    "text-center text-[11px] leading-tight font-medium transition-colors",
                                    isCurrent
                                        ? "text-brand-700 dark:text-brand-300"
                                        : isComplete
                                          ? "text-slate-700 dark:text-slate-300"
                                          : "text-slate-400 dark:text-slate-500",
                                )}
                            >
                                {style.label}
                            </span>
                        </li>
                    );
                })}
            </ol>

            {/* The two outcomes. Shown as a pair of buttons while the deal is
                open, and as a single verdict banner once it has closed. */}
            <div className="flex gap-2 border-t border-slate-100 pt-4 dark:border-slate-800">
                {closed ? (
                    <ClosedBanner stage={stage} onReopen={onSelect} disabled={disabled} />
                ) : (
                    <>
                        <OutcomeButton
                            stage="won"
                            onSelect={onSelect}
                            disabled={disabled}
                        />
                        <OutcomeButton
                            stage="lost"
                            onSelect={onSelect}
                            disabled={disabled}
                        />
                    </>
                )}
            </div>
        </div>
    );
}

function OutcomeButton({
    stage,
    onSelect,
    disabled,
}: {
    stage: Extract<Stage, "won" | "lost">;
    onSelect?: (stage: Stage) => void;
    disabled?: boolean;
}) {
    const style = STAGE_STYLES[stage];
    const Icon = style.icon;

    return (
        <button
            type="button"
            disabled={disabled || !onSelect}
            onClick={() => onSelect?.(stage)}
            className={cx(
                "inline-flex flex-1 items-center justify-center gap-1.5 rounded-lg border px-3 py-2 text-xs font-medium transition-colors disabled:cursor-not-allowed disabled:opacity-60",
                stage === "won"
                    ? "border-emerald-200 text-emerald-700 hover:bg-emerald-50 dark:border-emerald-800 dark:text-emerald-300 dark:hover:bg-emerald-950/50"
                    : "border-rose-200 text-rose-700 hover:bg-rose-50 dark:border-rose-800 dark:text-rose-300 dark:hover:bg-rose-950/50",
            )}
        >
            <Icon className="size-3.5" aria-hidden="true" />
            Mark as {style.label}
        </button>
    );
}

function ClosedBanner({
    stage,
    onReopen,
    disabled,
}: {
    stage: Stage;
    onReopen?: (stage: Stage) => void;
    disabled?: boolean;
}) {
    const style = STAGE_STYLES[stage];
    const Icon = style.icon;
    const won = stage === "won";

    return (
        <div
            className={cx(
                "flex w-full items-center gap-3 rounded-lg border px-3 py-2.5",
                won
                    ? "border-emerald-200 bg-emerald-50 dark:border-emerald-900 dark:bg-emerald-950/40"
                    : "border-rose-200 bg-rose-50 dark:border-rose-900 dark:bg-rose-950/40",
            )}
        >
            <Icon
                className={cx("size-4 shrink-0", won ? "text-emerald-600" : "text-rose-600")}
                aria-hidden="true"
            />
            <p
                className={cx(
                    "flex-1 text-xs font-medium",
                    won
                        ? "text-emerald-900 dark:text-emerald-200"
                        : "text-rose-900 dark:text-rose-200",
                )}
            >
                {won ? "This deal was won." : "This deal was lost."}
            </p>

            {/* Reopening matters in practice: deals get revived, and a CRM that
                cannot walk a stage backwards forces people to lie to it. */}
            {onReopen && (
                <button
                    type="button"
                    disabled={disabled}
                    onClick={() => onReopen("negotiation")}
                    className="rounded-md px-2 py-1 text-xs font-medium text-slate-600 underline-offset-2 transition-colors hover:underline disabled:opacity-50 dark:text-slate-400"
                >
                    Reopen
                </button>
            )}
        </div>
    );
}
