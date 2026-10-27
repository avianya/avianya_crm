import { AlertTriangle, Flame, Gauge, Hourglass } from "lucide-react";

import {
    BAND_STYLES,
    HEALTH_STYLES,
    PENALTY_TIPS,
    SCORE_TIPS,
    STAGE_STYLES,
    cx,
    formatDateTime,
    formatSilence,
    scoreTone,
} from "../lib/display";
import type { Lead, LeadScore } from "../lib/types";
import { Card, CardHeader } from "./ui";

/**
 * Health as a small pill: a coloured dot, the label, and (optionally) how long
 * it has been. The exact last-contact time is on hover.
 */
export function HealthBadge({ lead, showDays = false }: { lead: Lead; showDays?: boolean }) {
    const style = HEALTH_STYLES[lead.health];

    return (
        <span
            className={cx(
                "inline-flex items-center gap-1.5 rounded-full px-2 py-0.5 text-xs font-medium whitespace-nowrap ring-1 ring-inset",
                style.badge,
            )}
            title={
                lead.health === "closed"
                    ? "Won and lost leads do not go cold."
                    : `Last contact ${formatDateTime(lead.last_touch_at)}`
            }
        >
            <span className={cx("size-1.5 rounded-full", style.dot)} aria-hidden="true" />
            {style.label}
            {showDays && lead.health !== "closed" && (
                <span className="font-normal opacity-80">· {lead.days_since_touch}d</span>
            )}
        </span>
    );
}

/**
 * "Stuck · 12d" when a deal has sat in its stage past the limit; nothing
 * otherwise. The working is on hover.
 */
export function StuckBadge({ lead }: { lead: Lead }) {
    if (!lead.stuck) return null;
    return (
        <span
            className="inline-flex items-center gap-1 rounded-full bg-orange-50 px-2 py-0.5 text-xs font-medium whitespace-nowrap text-orange-700 ring-1 ring-orange-200 ring-inset dark:bg-orange-950/60 dark:text-orange-300 dark:ring-orange-900"
            title={`${lead.days_in_stage} days in ${STAGE_STYLES[lead.stage].label}; deals here are flagged after ${lead.stuck_limit_days}.`}
        >
            <Hourglass className="size-3" aria-hidden="true" />
            Stuck
            <span className="font-normal opacity-80">· {lead.days_in_stage}d</span>
        </span>
    );
}

/** The score as a small pill: its band and the number. Renders a dash for closed leads. */
export function ScorePill({ score }: { score: LeadScore | null }) {
    if (!score) {
        return <span className="text-xs text-slate-300 dark:text-slate-600">—</span>;
    }

    const style = BAND_STYLES[score.band];
    return (
        <span
            className={cx(
                "inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-xs font-medium whitespace-nowrap ring-1 ring-inset",
                style.badge,
            )}
            title={`${style.label} lead: score ${score.total} / 100`}
        >
            {score.band === "hot" && <Flame className="size-3" aria-hidden="true" />}
            {style.label}
            <span className="tabular font-semibold">{score.total}</span>
        </span>
    );
}

/**
 * The full score with its working: one bar per factor, and for every factor
 * that has not earned its points, the action that would.
 *
 * Read top to bottom it is a checklist for the lead, which is the point -- a
 * score nobody can act on is just a number.
 */
export function ScoreCard({ lead }: { lead: Lead }) {
    const score = lead.score;

    return (
        <Card>
            <CardHeader
                title="Lead score"
                subtitle={
                    score
                        ? `${formatSilence(lead.days_since_touch)} · ${HEALTH_STYLES[lead.health].label}`
                        : undefined
                }
                action={
                    score && (
                        <span className={cx("tabular text-2xl font-semibold", scoreTone(score.total))}>
                            <span className="mr-1.5 align-middle text-sm font-medium">
                                {BAND_STYLES[score.band].label}
                            </span>
                            {score.total}
                            <span className="text-xs font-normal text-slate-400 dark:text-slate-500">
                                {" "}
                                / 100
                            </span>
                        </span>
                    )
                }
            />

            {!score ? (
                <div className="flex items-center gap-2 px-5 py-4 text-sm text-slate-400 dark:text-slate-500">
                    <Gauge className="size-4" aria-hidden="true" />
                    Closed leads are not scored.
                </div>
            ) : (
                <>
                    <ul className="space-y-3 px-5 py-4">
                        {score.factors.map((factor) => {
                            const full = factor.points === factor.max;
                            return (
                                <li key={factor.key}>
                                    <div className="flex items-baseline justify-between gap-3 text-xs">
                                        <span className="text-slate-700 dark:text-slate-300">
                                            {factor.label}
                                        </span>
                                        <span className="tabular text-slate-500 dark:text-slate-400">
                                            <span className="font-semibold text-slate-900 dark:text-slate-100">
                                                {factor.points}
                                            </span>
                                            /{factor.max}
                                        </span>
                                    </div>
                                    <div className="mt-1 h-1.5 overflow-hidden rounded-full bg-slate-100 dark:bg-slate-800">
                                        <div
                                            className={cx(
                                                "h-full rounded-full transition-all duration-500",
                                                full ? "bg-emerald-500" : "bg-amber-400",
                                            )}
                                            // Data-driven width, so an inline style
                                            // rather than a Tailwind class.
                                            style={{ width: `${(factor.points / factor.max) * 100}%` }}
                                        />
                                    </div>
                                    {!full && (
                                        <p className="mt-1 text-[11px] text-slate-500 dark:text-slate-400">
                                            +{factor.max - factor.points}: {SCORE_TIPS[factor.key]}
                                        </p>
                                    )}
                                </li>
                            );
                        })}
                    </ul>

                    {score.penalties.length > 0 && (
                        <ul className="space-y-2 border-t border-slate-100 px-5 py-4 dark:border-slate-800">
                            {score.penalties.map((penalty) => (
                                <li key={penalty.key} className="flex gap-2">
                                    <AlertTriangle
                                        className="mt-0.5 size-3.5 shrink-0 text-rose-500 dark:text-rose-400"
                                        aria-hidden="true"
                                    />
                                    <div className="min-w-0 flex-1">
                                        <div className="flex items-baseline justify-between gap-3 text-xs">
                                            <span className="text-slate-700 dark:text-slate-300">
                                                {penalty.label}
                                            </span>
                                            <span className="tabular font-semibold text-rose-600 dark:text-rose-400">
                                                {penalty.points}
                                            </span>
                                        </div>
                                        <p className="mt-0.5 text-[11px] text-slate-500 dark:text-slate-400">
                                            {PENALTY_TIPS[penalty.key]}
                                        </p>
                                    </div>
                                </li>
                            ))}
                        </ul>
                    )}
                </>
            )}
        </Card>
    );
}
