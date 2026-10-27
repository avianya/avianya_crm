import { useState } from "react";
import { Link } from "react-router-dom";
import { CalendarClock, Check, CircleCheck, ListTodo } from "lucide-react";

import { ApiError, api } from "../lib/api";
import {
    cx,
    daysUntil,
    formatDue,
    formatRelative,
    formatTimeSlot,
    formatDateTime,
} from "../lib/display";
import type { TaskWithLead } from "../lib/types";
import { useAsync } from "../hooks/useAsync";
import { useToast } from "../context/toast";
import { HealthBadge, ScorePill } from "../components/LeadInsights";
import { StageBadge } from "../components/StageBadge";
import { TaskTypeIcon } from "../components/TaskTypePicker";
import { Card, CardHeader, EmptyState, ErrorState, PageHeader, Skeleton } from "../components/ui";

type View = "open" | "completed";

/** The buckets the open list is split into, in the order they are shown. */
const GROUPS = [
    { key: "overdue", title: "Overdue", tone: "text-rose-600 dark:text-rose-400" },
    { key: "today", title: "Today", tone: "text-amber-600 dark:text-amber-400" },
    { key: "week", title: "Next 7 days", tone: "text-slate-900 dark:text-slate-100" },
    { key: "later", title: "Later", tone: "text-slate-900 dark:text-slate-100" },
] as const;

type GroupKey = (typeof GROUPS)[number]["key"];

function groupOf(item: TaskWithLead): GroupKey {
    const days = daysUntil(item.task.due_date);
    if (days < 0) return "overdue";
    if (days === 0) return "today";
    if (days < 7) return "week";
    return "later";
}

/**
 * Priority order inside a group: the lead's score first, then the due date.
 *
 * Two calls are both due today; the one with the 80-point deal in Negotiation
 * goes first. Ranking by the lead, not just the date, is what makes this a
 * work queue instead of a calendar.
 */
function byPriority(a: TaskWithLead, b: TaskWithLead): number {
    const scoreA = a.lead.score?.total ?? -1;
    const scoreB = b.lead.score?.total ?? -1;
    if (scoreA !== scoreB) return scoreB - scoreA;
    if (a.task.due_date !== b.task.due_date) return a.task.due_date < b.task.due_date ? -1 : 1;
    return a.task.id - b.task.id;
}

/**
 * Every follow-up across every lead, in one place.
 *
 * Tasks are still created on the lead itself, next to the history that
 * explains them; this page is where they are worked through.
 */
export function TasksPage() {
    const toast = useToast();
    const [view, setView] = useState<View>("open");
    const [busyId, setBusyId] = useState<number | null>(null);

    const { data, error, loading, fetching, refetch, setData } = useAsync(
        (signal) => api.listAllTasks(view, signal),
        [view],
    );

    /**
     * Ticks a task off (in the open view) or reopens it (in the completed view).
     * Either way it leaves the current list, so it is removed straight away
     * rather than waiting for a refetch.
     */
    const toggle = async (item: TaskWithLead) => {
        // Just after switching views the previous list is still on screen
        // (dimmed) while the new one loads; a click there would tick or untick
        // the wrong way, so it waits until the right list has arrived.
        if (fetching) return;

        const completing = view === "open";
        setBusyId(item.task.id);
        try {
            await api.setTaskCompleted(item.task.id, completing);
            setData((current) => (current ?? []).filter((i) => i.task.id !== item.task.id));
            toast.success(
                completing
                    ? `Done — logged on ${item.lead.name}'s timeline.`
                    : `Reopened "${item.task.title}".`,
            );
        } catch (cause) {
            toast.error(cause instanceof ApiError ? cause.message : "Could not update the task.");
        } finally {
            setBusyId(null);
        }
    };

    const items = data ?? [];
    const overdue = items.filter((item) => groupOf(item) === "overdue").length;
    const today = items.filter((item) => groupOf(item) === "today").length;

    return (
        <>
            <PageHeader
                title="Tasks"
                subtitle={
                    view === "open" && data
                        ? `${items.length} open · ${overdue} overdue · ${today} due today`
                        : "Follow-ups across every lead."
                }
                action={<ViewToggle view={view} onChange={setView} />}
            />

            {loading ? (
                <TasksSkeleton />
            ) : error ? (
                <ErrorState message={error.message} onRetry={refetch} />
            ) : items.length === 0 ? (
                <Card>
                    {view === "open" ? (
                        <EmptyState
                            icon={CircleCheck}
                            title="No open tasks"
                            description="Add a follow-up from any lead's page and it will show up here."
                            action={
                                <Link
                                    to="/leads"
                                    className="inline-flex h-10 items-center justify-center rounded-lg bg-brand-600 px-4 text-sm font-medium text-white shadow-sm transition-colors hover:bg-brand-700 dark:bg-brand-500 dark:hover:bg-brand-400"
                                >
                                    Go to leads
                                </Link>
                            }
                        />
                    ) : (
                        <EmptyState
                            icon={ListTodo}
                            title="Nothing completed yet"
                            description="Ticked-off tasks will be listed here, newest first."
                        />
                    )}
                </Card>
            ) : (
                <div className={cx("space-y-6 transition-opacity", fetching && "opacity-60")}>
                    {view === "open" ? (
                        GROUPS.map((group) => {
                            const rows = items.filter((item) => groupOf(item) === group.key).sort(byPriority);
                            if (rows.length === 0) return null;
                            return (
                                <Card key={group.key}>
                                    <CardHeader
                                        title={
                                            <span className={group.tone}>
                                                {group.title}{" "}
                                                <span className="font-normal text-slate-400 dark:text-slate-500">
                                                    {rows.length}
                                                </span>
                                            </span>
                                        }
                                        subtitle="Highest-scoring leads first."
                                    />
                                    <TaskRows rows={rows} busyId={busyId} done={false} onToggle={toggle} />
                                </Card>
                            );
                        })
                    ) : (
                        <Card>
                            <CardHeader title="Recently completed" subtitle="The latest 50, newest first." />
                            <TaskRows rows={items} busyId={busyId} done onToggle={toggle} />
                        </Card>
                    )}
                </div>
            )}
        </>
    );
}

// ---------------------------------------------------------------------------
// Pieces
// ---------------------------------------------------------------------------

function ViewToggle({ view, onChange }: { view: View; onChange: (view: View) => void }) {
    const options: { value: View; label: string }[] = [
        { value: "open", label: "Open" },
        { value: "completed", label: "Completed" },
    ];

    return (
        <div
            className="inline-flex rounded-lg border border-slate-200 bg-white p-0.5 dark:border-slate-700 dark:bg-slate-900"
            role="group"
            aria-label="Which tasks to show"
        >
            {options.map((option) => (
                <button
                    key={option.value}
                    type="button"
                    aria-pressed={view === option.value}
                    onClick={() => onChange(option.value)}
                    className={cx(
                        "rounded-md px-3 py-1.5 text-sm font-medium transition-colors",
                        view === option.value
                            ? "bg-brand-600 text-white dark:bg-brand-500"
                            : "text-slate-600 hover:text-slate-900 dark:text-slate-300 dark:hover:text-slate-100",
                    )}
                >
                    {option.label}
                </button>
            ))}
        </div>
    );
}

function TaskRows({
    rows,
    busyId,
    done,
    onToggle,
}: {
    rows: TaskWithLead[];
    busyId: number | null;
    done: boolean;
    onToggle: (item: TaskWithLead) => void;
}) {
    return (
        <ul className="divide-y divide-slate-100 dark:divide-slate-800">
            {rows.map((item) => {
                const { task, lead } = item;
                const busy = busyId === task.id;
                const late = !done && daysUntil(task.due_date) < 0;

                return (
                    <li
                        key={task.id}
                        className={cx("flex flex-wrap items-start gap-x-3 gap-y-2 px-5 py-3.5", busy && "opacity-60")}
                    >
                        <button
                            type="button"
                            role="checkbox"
                            aria-checked={done}
                            aria-label={done ? `Reopen "${task.title}"` : `Complete "${task.title}"`}
                            disabled={busy}
                            onClick={() => onToggle(item)}
                            className={cx(
                                "group mt-0.5 flex size-5 shrink-0 items-center justify-center rounded-md border transition-colors",
                                done
                                    ? "border-teal-500 bg-teal-500 text-white"
                                    : "border-slate-300 hover:border-teal-500 dark:border-slate-600",
                            )}
                        >
                            <Check
                                className={cx(
                                    "size-3.5",
                                    done ? "" : "text-teal-500 opacity-0 group-hover:opacity-100",
                                )}
                                aria-hidden="true"
                            />
                        </button>

                        <div className="min-w-0 flex-1 basis-56">
                            <p
                                className={cx(
                                    "text-sm break-words",
                                    done
                                        ? "text-slate-400 line-through dark:text-slate-500"
                                        : "text-slate-900 dark:text-slate-100",
                                )}
                            >
                                <TaskTypeIcon type={task.type} className="mr-1 size-3.5 align-[-2px]" />
                                {task.title}
                            </p>
                            <p className="mt-0.5 flex flex-wrap items-center gap-x-1.5 text-xs text-slate-500 dark:text-slate-400">
                                <Link
                                    to={`/leads/${lead.id}`}
                                    className="font-medium text-brand-600 hover:underline dark:text-brand-400"
                                >
                                    {lead.name}
                                </Link>
                                <span>· {lead.company}</span>
                                {!done && task.start_time && (
                                    <span className="font-medium text-slate-700 dark:text-slate-300">
                                        · {formatTimeSlot(task)}
                                    </span>
                                )}
                            </p>
                        </div>

                        {/* The lead's context, so priority is visible at a glance. */}
                        <div className="flex shrink-0 flex-wrap items-center gap-2 pl-8 sm:pl-0">
                            <StageBadge stage={lead.stage} showIcon={false} />
                            <HealthBadge lead={lead} />
                            <span className="w-20 shrink-0 text-right">
                                <ScorePill score={lead.score} />
                            </span>
                            <span
                                className={cx(
                                    "inline-flex w-36 items-center justify-end gap-1 text-xs",
                                    late ? "font-medium text-rose-600 dark:text-rose-400" : "text-slate-500 dark:text-slate-400",
                                )}
                                title={done && task.completed_at ? formatDateTime(task.completed_at) : task.due_date}
                            >
                                <CalendarClock className="size-3 shrink-0" aria-hidden="true" />
                                {done && task.completed_at
                                    ? `Done ${formatRelative(task.completed_at)}`
                                    : formatDue(task.due_date)}
                            </span>
                        </div>
                    </li>
                );
            })}
        </ul>
    );
}

function TasksSkeleton() {
    return (
        <Card>
            <div className="border-b border-slate-200 px-5 py-4 dark:border-slate-800">
                <Skeleton className="h-4 w-24" />
            </div>
            <div className="divide-y divide-slate-100 dark:divide-slate-800">
                {Array.from({ length: 4 }).map((_, index) => (
                    <div key={index} className="flex items-center gap-3 px-5 py-3.5">
                        <Skeleton className="size-5 rounded-md" />
                        <div className="flex-1 space-y-1.5">
                            <Skeleton className="h-3.5 w-48" />
                            <Skeleton className="h-3 w-32" />
                        </div>
                        <Skeleton className="h-5 w-40" />
                    </div>
                ))}
            </div>
        </Card>
    );
}
