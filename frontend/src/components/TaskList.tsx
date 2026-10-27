import { useEffect, useRef, useState } from "react";
import { Bell, CalendarClock, Check, ListTodo, Pencil, Plus, UserRound, X } from "lucide-react";

import { ApiError, api } from "../lib/api";
import {
    DEFAULT_TASK_DURATION,
    TASK_STATUS_STYLES,
    cx,
    daysUntil,
    formatDateTime,
    formatDue,
    formatReminder,
    formatTimeSlot,
    minutesOfDay,
    todayISODate,
} from "../lib/display";
import type { Task, TaskType } from "../lib/types";
import { useAsync } from "../hooks/useAsync";
import { defaultReminderFor, memberName, useTeam } from "../hooks/useTeam";
import { useToast } from "../context/toast";
import { Button, Card, CardHeader, EmptyState, ErrorState, Skeleton, TextInput } from "./ui";
import { ReminderSelect } from "./EventFields";
import { TaskEditModal } from "./TaskEditModal";
import { TaskTypeIcon, TaskTypePicker } from "./TaskTypePicker";
import { TimeSlotPicker } from "./TimeSlotPicker";

/** Adds n days to a "YYYY-MM-DD" date, staying in calendar-date land. */
function addDays(isoDate: string, days: number): string {
    const [year, month, day] = isoDate.split("-").map(Number);
    const date = new Date(Date.UTC(year, month - 1, day + days));
    return date.toISOString().slice(0, 10);
}

/**
 * One-click due dates. Picking a date from a calendar is the slow part of
 * adding a follow-up, and nearly every follow-up is one of these.
 */
const QUICK_DUES: { label: string; days: number }[] = [
    { label: "Today", days: 0 },
    { label: "Tomorrow", days: 1 },
    { label: "In 3 days", days: 3 },
    { label: "Next week", days: 7 },
];

/** A request to open one event's editor. `id` changes per request. */
export interface OpenEventRequest {
    id: number;
    taskId: number;
}

/** A pre-filled new event, e.g. from a suggested next step. `id` changes per request. */
export interface TaskDraft {
    id: number;
    type: TaskType;
    title: string;
    due_date: string;
}

const isClosed = (task: Task) => task.status === "completed" || task.status === "cancelled";

/** Colour for the due label: red when late, amber today, quiet otherwise. */
function dueTone(task: Task): string {
    if (isClosed(task)) return "text-slate-400 dark:text-slate-500";
    if (task.status === "overdue") return "text-rose-600 dark:text-rose-400";
    if (daysUntil(task.due_date) === 0) return "text-amber-600 dark:text-amber-400";
    return "text-slate-500 dark:text-slate-400";
}

/**
 * The lead's events: what has to happen next, and by when.
 *
 * Events live on the lead rather than in a separate inbox, so the next step is
 * always visible next to the history that explains it. Completing one writes it
 * to the timeline; so does cancelling one.
 *
 * onChanged fires after every add, tick, cancel and delete, because each can
 * change things outside this card: the timeline, next steps, health and score.
 */
export function TaskList({
    leadId,
    leadName,
    ownerId,
    draft,
    openRequest,
    onChanged,
}: {
    leadId: number;
    leadName: string;
    /** The lead's owner: new events are for them and use their default reminder. */
    ownerId: number | null;
    draft?: TaskDraft | null;
    openRequest?: OpenEventRequest | null;
    onChanged: () => void;
}) {
    const toast = useToast();
    const team = useTeam();
    const titleRef = useRef<HTMLInputElement>(null);

    const { data: tasks, error, loading, refetch, setData } = useAsync(
        (signal) => api.listTasks(leadId, signal),
        [leadId],
    );

    const [title, setTitle] = useState("");
    const [type, setType] = useState<TaskType>("todo");
    const [dueDate, setDueDate] = useState(() => addDays(todayISODate(), 1));
    const [startTime, setStartTime] = useState("");
    const [duration, setDuration] = useState(DEFAULT_TASK_DURATION);
    // undefined until touched: follows the owner's default reminder.
    const [reminderChoice, setReminderChoice] = useState<number | null | undefined>(undefined);
    const [saving, setSaving] = useState(false);
    const [formError, setFormError] = useState<string | null>(null);
    const [busyId, setBusyId] = useState<number | null>(null);
    const [showClosed, setShowClosed] = useState(false);
    const [editing, setEditing] = useState<Task | null>(null);
    // The event just completed, while the "schedule the next step" prompt shows.
    const [justDone, setJustDone] = useState<Task | null>(null);

    const ownerName = memberName(team.data, ownerId);
    const reminder = reminderChoice === undefined ? defaultReminderFor(team.data, ownerId) : reminderChoice;
    const trimmed = title.trim();
    const today = todayISODate();

    const prefill = (next: Omit<TaskDraft, "id">) => {
        setTitle(next.title);
        setType(next.type);
        setDueDate(next.due_date);
        setJustDone(null);
        // After the render that fills the form, so the cursor lands in it.
        requestAnimationFrame(() => {
            titleRef.current?.focus();
            titleRef.current?.scrollIntoView({ block: "center", behavior: "smooth" });
        });
    };

    // A new draft from outside (a suggested next step) fills the form. Adjusting
    // state during render, rather than in an effect, is React's pattern for
    // "reset when a prop changes".
    const [appliedDraftId, setAppliedDraftId] = useState<number | null>(null);
    if (draft && draft.id !== appliedDraftId) {
        setAppliedDraftId(draft.id);
        setTitle(draft.title);
        setType(draft.type);
        setDueDate(draft.due_date);
        setJustDone(null);
    }
    // Likewise a request to open an event, once the list holding it has loaded.
    const [appliedOpenId, setAppliedOpenId] = useState<number | null>(null);
    if (openRequest && openRequest.id !== appliedOpenId && tasks) {
        setAppliedOpenId(openRequest.id);
        setEditing(tasks.find((task) => task.id === openRequest.taskId) ?? null);
    }

    // Focusing touches the DOM, so it waits for the render that filled the form.
    useEffect(() => {
        if (appliedDraftId === null) return;
        titleRef.current?.focus();
        titleRef.current?.scrollIntoView({ block: "center", behavior: "smooth" });
    }, [appliedDraftId]);

    const replace = (task: Task) =>
        setData((current) => sortTasks((current ?? []).map((t) => (t.id === task.id ? task : t))));

    const handleAdd = async (event: React.FormEvent) => {
        event.preventDefault();
        if (!trimmed || !dueDate || saving) return;

        setSaving(true);
        setFormError(null);
        try {
            const task = await api.createTask(leadId, {
                title: trimmed,
                type,
                due_date: dueDate,
                ...(startTime && { start_time: startTime, duration_minutes: duration }),
                reminder_minutes: reminder,
            });
            // Re-sort locally the same way the server does: open first, by due date.
            setData((current) => sortTasks([...(current ?? []), task]));
            setTitle("");
            setType("todo");
            setReminderChoice(undefined);
            toast.success(
                task.reminder_minutes
                    ? `Event added. Reminder ${formatReminder(task.reminder_minutes)}.`
                    : "Event added.",
            );
            onChanged();
        } catch (cause) {
            setFormError(cause instanceof ApiError ? cause.message : "Could not add the event.");
        } finally {
            setSaving(false);
        }
    };

    const handleToggle = async (task: Task) => {
        setBusyId(task.id);
        const completing = task.status !== "completed";
        try {
            const result = await api.setTaskCompleted(task.id, completing);
            replace(result.task);
            if (result.activity) {
                toast.success("Done and logged on the timeline.");
            }
            setJustDone(completing ? result.task : null);
            onChanged();
        } catch (cause) {
            toast.error(cause instanceof ApiError ? cause.message : "Could not update the event.");
        } finally {
            setBusyId(null);
        }
    };

    const all = tasks ?? [];
    const open = all.filter((task) => !isClosed(task));
    const closed = all.filter(isClosed);
    const hasNextPlanned = open.some((task) => task.status === "upcoming");

    return (
        <Card>
            <CardHeader
                title="Upcoming events"
                subtitle={
                    tasks
                        ? open.length === 0
                            ? "Nothing planned - add a follow-up below."
                            : `${open.length} open`
                        : undefined
                }
            />

            {justDone && !hasNextPlanned && (
                <div className="flex flex-wrap items-center gap-3 border-b border-teal-200 bg-teal-50 px-5 py-3 text-sm dark:border-teal-900 dark:bg-teal-950/40">
                    <Check className="size-4 shrink-0 text-teal-600 dark:text-teal-400" aria-hidden="true" />
                    <span className="min-w-0 flex-1 text-teal-900 dark:text-teal-200">
                        “{justDone.title}” is done. Nothing else is planned for {leadName}.
                    </span>
                    <Button
                        size="sm"
                        variant="primary"
                        onClick={() =>
                            prefill({ type: "follow_up", title: `Follow up with ${leadName}`, due_date: addDays(today, 2) })
                        }
                    >
                        Schedule next follow-up
                    </Button>
                    <button
                        type="button"
                        onClick={() => setJustDone(null)}
                        aria-label="Dismiss"
                        className="rounded-md p-1 text-teal-700 hover:bg-teal-100 dark:text-teal-300 dark:hover:bg-teal-900"
                    >
                        <X className="size-4" aria-hidden="true" />
                    </button>
                </div>
            )}

            <form onSubmit={handleAdd} className="space-y-2.5 border-b border-slate-200 px-5 py-4 dark:border-slate-800">
                <label htmlFor="task-title" className="sr-only">
                    New event
                </label>
                <TextInput
                    ref={titleRef}
                    id="task-title"
                    value={title}
                    maxLength={200}
                    placeholder="e.g. Send the revised quote"
                    onChange={(event) => setTitle(event.target.value)}
                />

                <TaskTypePicker compact value={type} onChange={setType} />

                <div className="flex flex-wrap items-center gap-1.5">
                    {QUICK_DUES.map((option) => {
                        const value = addDays(today, option.days);
                        return (
                            <button
                                key={option.label}
                                type="button"
                                onClick={() => setDueDate(value)}
                                className={cx(
                                    "rounded-full px-2.5 py-1 text-xs font-medium ring-1 ring-inset transition-colors",
                                    dueDate === value
                                        ? "bg-brand-50 text-brand-700 ring-brand-200 dark:bg-brand-950 dark:text-brand-300 dark:ring-brand-800"
                                        : "text-slate-600 ring-slate-200 hover:bg-slate-50 dark:text-slate-300 dark:ring-slate-700 dark:hover:bg-slate-800",
                                )}
                            >
                                {option.label}
                            </button>
                        );
                    })}
                    <label htmlFor="task-due" className="sr-only">
                        Due date
                    </label>
                    <input
                        id="task-due"
                        type="date"
                        value={dueDate}
                        onChange={(event) => setDueDate(event.target.value)}
                        className="h-7 rounded-md border border-slate-300 bg-white px-2 text-xs text-slate-700 dark:border-slate-700 dark:bg-slate-950 dark:text-slate-200"
                    />
                </div>

                <TimeSlotPicker
                    idPrefix="task"
                    compact
                    startTime={startTime}
                    duration={duration}
                    onChange={(nextStart, nextDuration) => {
                        setStartTime(nextStart);
                        setDuration(nextDuration);
                    }}
                />

                <div className="flex flex-wrap items-center gap-2 text-xs text-slate-500 dark:text-slate-400">
                    <Bell className="size-3.5" aria-hidden="true" />
                    <label htmlFor="task-reminder" className="sr-only">
                        Reminder
                    </label>
                    <ReminderSelect id="task-reminder" compact value={reminder} onChange={setReminderChoice} />
                    <span>
                        {ownerName ? `for ${ownerName}` : "No owner yet: reminders show in the app only."}
                    </span>
                </div>

                <div className="flex items-center justify-between gap-3">
                    <span className="text-xs text-slate-500 dark:text-slate-400">
                        {dueDate && formatDue(dueDate)}
                    </span>
                    <Button
                        type="submit"
                        variant="primary"
                        size="sm"
                        loading={saving}
                        disabled={!trimmed || !dueDate}
                    >
                        {!saving && <Plus className="size-3.5" aria-hidden="true" />}
                        Add event
                    </Button>
                </div>

                {formError && (
                    <p className="text-xs text-rose-600 dark:text-rose-400" role="alert">
                        {formError}
                    </p>
                )}
            </form>

            {loading ? (
                <div className="space-y-3 px-5 py-4">
                    {Array.from({ length: 2 }).map((_, index) => (
                        <Skeleton key={index} className="h-8 w-full" />
                    ))}
                </div>
            ) : error ? (
                <div className="p-4">
                    <ErrorState message={error.message} onRetry={refetch} />
                </div>
            ) : open.length === 0 && closed.length === 0 ? (
                <EmptyState
                    icon={ListTodo}
                    title="No events yet"
                    description="A lead with no next step is the one that goes cold."
                />
            ) : (
                <>
                    <ul className="divide-y divide-slate-100 dark:divide-slate-800">
                        {open.map((task) => (
                            <TaskRow
                                key={task.id}
                                task={task}
                                assignee={memberName(team.data, task.assignee_id)}
                                busy={busyId === task.id}
                                onToggle={handleToggle}
                                onEdit={setEditing}
                            />
                        ))}
                    </ul>

                    {closed.length > 0 && (
                        <div className="border-t border-slate-100 dark:border-slate-800">
                            <button
                                type="button"
                                onClick={() => setShowClosed((value) => !value)}
                                className="w-full px-5 py-2.5 text-left text-xs font-medium text-slate-500 hover:text-slate-800 dark:text-slate-400 dark:hover:text-slate-200"
                                aria-expanded={showClosed}
                            >
                                {showClosed ? "Hide" : "Show"} {closed.length} completed or cancelled
                            </button>
                            {showClosed && (
                                <ul className="divide-y divide-slate-100 dark:divide-slate-800">
                                    {closed.map((task) => (
                                        <TaskRow
                                            key={task.id}
                                            task={task}
                                            assignee={memberName(team.data, task.assignee_id)}
                                            busy={busyId === task.id}
                                            onToggle={handleToggle}
                                            onEdit={setEditing}
                                        />
                                    ))}
                                </ul>
                            )}
                        </div>
                    )}
                </>
            )}

            <TaskEditModal
                task={editing}
                subtitle={leadName}
                onClose={() => setEditing(null)}
                onSaved={(saved, message) => {
                    replace(saved);
                    setEditing(null);
                    setJustDone(saved.status === "completed" ? saved : null);
                    toast.success(message);
                    onChanged();
                }}
                onDeleted={(deleted) => {
                    setData((current) => (current ?? []).filter((t) => t.id !== deleted.id));
                    setEditing(null);
                    toast.success("Event deleted.");
                    onChanged();
                }}
            />
        </Card>
    );
}

function TaskRow({
    task,
    assignee,
    busy,
    onToggle,
    onEdit,
}: {
    task: Task;
    assignee: string | null;
    busy: boolean;
    onToggle: (task: Task) => void;
    onEdit: (task: Task) => void;
}) {
    const done = task.status === "completed";
    const cancelled = task.status === "cancelled";
    const slot = formatTimeSlot(task);
    const showBadge = task.status === "overdue" || cancelled;

    return (
        <li className={cx("group flex items-start gap-3 px-5 py-3", busy && "opacity-60")}>
            {cancelled ? (
                // A cancelled event is not ticked off; it is restored from the editor.
                <span
                    className="mt-0.5 size-5 shrink-0 rounded-md border border-dashed border-slate-300 dark:border-slate-600"
                    aria-hidden="true"
                />
            ) : (
                <button
                    type="button"
                    role="checkbox"
                    aria-checked={done}
                    aria-label={done ? `Reopen "${task.title}"` : `Complete "${task.title}"`}
                    disabled={busy}
                    onClick={() => onToggle(task)}
                    className={cx(
                        "group/check mt-0.5 flex size-5 shrink-0 items-center justify-center rounded-md border transition-colors",
                        done
                            ? "border-teal-500 bg-teal-500 text-white"
                            : "border-slate-300 hover:border-teal-500 dark:border-slate-600",
                    )}
                >
                    <Check
                        className={cx("size-3.5", done ? "" : "text-teal-500 opacity-0 group-hover/check:opacity-100")}
                        aria-hidden="true"
                    />
                </button>
            )}

            <div className="min-w-0 flex-1">
                <p
                    className={cx(
                        "text-sm break-words",
                        done || cancelled
                            ? "text-slate-400 line-through dark:text-slate-500"
                            : "text-slate-900 dark:text-slate-100",
                    )}
                >
                    <TaskTypeIcon type={task.type} className="mr-1 size-3.5 align-[-2px]" />
                    {task.title}
                </p>
                <p className={cx("mt-0.5 flex flex-wrap items-center gap-x-1.5 gap-y-0.5 text-xs", dueTone(task))}>
                    {showBadge && (
                        <span
                            className={cx(
                                "rounded-full px-1.5 py-px text-[10px] font-medium ring-1 ring-inset",
                                TASK_STATUS_STYLES[task.status].badge,
                            )}
                        >
                            {TASK_STATUS_STYLES[task.status].label}
                        </span>
                    )}
                    <CalendarClock className="size-3" aria-hidden="true" />
                    {done && task.completed_at
                        ? `Done ${formatDateTime(task.completed_at)}`
                        : formatDue(task.due_date)}
                    {!done && slot && <span>· {slot}</span>}
                    {!isClosed(task) && task.reminder_minutes && (
                        <span className="inline-flex items-center gap-0.5" title="Reminder">
                            · <Bell className="size-3" aria-hidden="true" />
                            {formatReminder(task.reminder_minutes)}
                        </span>
                    )}
                    {assignee && (
                        <span className="inline-flex items-center gap-0.5" title="Assigned to">
                            · <UserRound className="size-3" aria-hidden="true" />
                            {assignee}
                        </span>
                    )}
                </p>
                {task.notes && !isClosed(task) && (
                    <p className="mt-1 line-clamp-2 text-xs whitespace-pre-line text-slate-500 dark:text-slate-400">
                        {task.notes}
                    </p>
                )}
            </div>

            <button
                type="button"
                disabled={busy}
                onClick={() => onEdit(task)}
                aria-label={`Edit "${task.title}"`}
                className="rounded-md p-1 text-slate-400 transition-opacity hover:text-slate-700 focus:opacity-100 sm:opacity-0 sm:group-hover:opacity-100 dark:hover:text-slate-200"
            >
                <Pencil className="size-3.5" aria-hidden="true" />
            </button>
        </li>
    );
}

/** Open first, then soonest due -- how the API orders them for a fresh load. */
function sortTasks(tasks: Task[]): Task[] {
    return [...tasks].sort((a, b) => {
        const closedA = isClosed(a) ? 1 : 0;
        const closedB = isClosed(b) ? 1 : 0;
        if (closedA !== closedB) return closedA - closedB;
        if (a.due_date !== b.due_date) return a.due_date < b.due_date ? -1 : 1;
        // All-day tasks first, then by start time.
        const timeA = a.start_time ? minutesOfDay(a.start_time) : -1;
        const timeB = b.start_time ? minutesOfDay(b.start_time) : -1;
        if (timeA !== timeB) return timeA - timeB;
        return a.id - b.id;
    });
}
