import { AlarmClock, CalendarPlus, CircleCheck, Hourglass, Lightbulb, MailQuestionMark, Search, UserRoundPlus } from "lucide-react";
import type { LucideIcon } from "lucide-react";

import { api } from "../lib/api";
import { TASK_TYPE_STYLES, cx } from "../lib/display";
import type { NextStep } from "../lib/types";
import { useAsync } from "../hooks/useAsync";
import { Button, Card, CardHeader, Skeleton } from "./ui";

const KIND_ICONS: Record<NextStep["kind"], LucideIcon> = {
    overdue: AlarmClock,
    prepare: Hourglass,
    first_contact: UserRoundPlus,
    no_reply: MailQuestionMark,
    follow_up: CalendarPlus,
    plan: Lightbulb,
    review: Search,
};

const PRIORITY_STYLES: Record<NextStep["priority"], { dot: string; label: string }> = {
    high: { dot: "bg-rose-500", label: "High priority" },
    medium: { dot: "bg-amber-500", label: "Medium priority" },
    low: { dot: "bg-slate-400", label: "Low priority" },
};

/**
 * Suggested next steps for a lead, worked out by the server from its events,
 * timeline and health (see backend/internal/model/recommendations.go). Each
 * one says why, from the records, and offers the move that acts on it.
 *
 * `version` changes whenever something on the page might change the advice,
 * which re-fetches it.
 */
export function NextStepsCard({
    leadId,
    version,
    onSchedule,
    onOpenEvent,
}: {
    leadId: number;
    version: number;
    onSchedule: (suggestion: NonNullable<NextStep["suggest"]>) => void;
    onOpenEvent: (taskId: number) => void;
}) {
    const { data, loading, error } = useAsync((signal) => api.nextSteps(leadId, signal), [leadId, version]);

    return (
        <Card>
            <CardHeader title="Suggested next steps" subtitle="Based on this lead's events and timeline." />
            {loading ? (
                <div className="space-y-3 px-5 py-4">
                    <Skeleton className="h-10 w-full" />
                    <Skeleton className="h-10 w-2/3" />
                </div>
            ) : error ? (
                <p className="px-5 py-4 text-sm text-slate-500 dark:text-slate-400">
                    Suggestions are unavailable right now.
                </p>
            ) : !data || data.length === 0 ? (
                <div className="flex items-center gap-2 px-5 py-4 text-sm text-slate-500 dark:text-slate-400">
                    <CircleCheck className="size-4 text-teal-500" aria-hidden="true" />
                    Nothing to suggest: this lead is on track.
                </div>
            ) : (
                <ul className="divide-y divide-slate-100 dark:divide-slate-800">
                    {data.map((step, index) => {
                        const Icon = KIND_ICONS[step.kind] ?? Lightbulb;
                        const priority = PRIORITY_STYLES[step.priority];
                        return (
                            <li key={`${step.kind}-${step.task_id ?? index}`} className="flex flex-wrap items-start gap-3 px-5 py-3.5">
                                <span className="relative mt-0.5 flex size-8 shrink-0 items-center justify-center rounded-full bg-slate-100 text-slate-600 dark:bg-slate-800 dark:text-slate-300">
                                    <Icon className="size-4" aria-hidden="true" />
                                    <span
                                        className={cx("absolute -top-0.5 -right-0.5 size-2.5 rounded-full ring-2 ring-white dark:ring-slate-900", priority.dot)}
                                        title={priority.label}
                                    />
                                    <span className="sr-only">{priority.label}</span>
                                </span>
                                <div className="min-w-0 flex-1 basis-48">
                                    <p className="text-sm font-medium text-slate-900 dark:text-slate-100">{step.title}</p>
                                    <p className="mt-0.5 text-xs text-slate-500 dark:text-slate-400">{step.detail}</p>
                                </div>
                                {step.suggest ? (
                                    <Button size="sm" onClick={() => onSchedule(step.suggest!)}>
                                        <CalendarPlus className="size-3.5" aria-hidden="true" />
                                        Schedule {TASK_TYPE_STYLES[step.suggest.type].label.toLowerCase()}
                                    </Button>
                                ) : step.task_id ? (
                                    <Button size="sm" onClick={() => onOpenEvent(step.task_id!)}>
                                        Open event
                                    </Button>
                                ) : null}
                            </li>
                        );
                    })}
                </ul>
            )}
        </Card>
    );
}
