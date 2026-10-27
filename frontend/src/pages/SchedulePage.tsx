import { useEffect, useMemo, useRef, useState } from "react";
import type { DragEvent, FormEvent, MouseEvent } from "react";
import { Link } from "react-router-dom";
import { Bell, CalendarDays, Check, ChevronLeft, ChevronRight, Clock, Pencil, Plus } from "lucide-react";

import { ApiError, api } from "../lib/api";
import {
    DEFAULT_TASK_DURATION,
    TASK_TYPE_STYLES,
    cx,
    formatClock,
    formatReminder,
    formatTimeSlot,
    minutesOfDay,
    timeFromMinutes,
    todayISODate,
} from "../lib/display";
import type { Lead, TaskType, TaskWithLead } from "../lib/types";
import { useAsync } from "../hooks/useAsync";
import { defaultReminderFor, memberName, useTeam } from "../hooks/useTeam";
import { useToast } from "../context/toast";
import { AssigneeSelect, ReminderSelect } from "../components/EventFields";
import { StageBadge } from "../components/StageBadge";
import { TaskEditModal } from "../components/TaskEditModal";
import { Modal } from "../components/Modal";
import { TaskTypeIcon, TaskTypePicker } from "../components/TaskTypePicker";
import { TimeSlotPicker } from "../components/TimeSlotPicker";
import {
    Button,
    Card,
    CardHeader,
    EmptyState,
    ErrorState,
    Field,
    PageHeader,
    Select,
    Skeleton,
    TextInput,
    Textarea,
} from "../components/ui";

type View = "month" | "week" | "day";

const WEEKDAYS = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"];

/** How many tasks a month cell lists before collapsing the rest into "+N more". */
const MONTH_CELL_LIMIT = 3;

/** Height of one hour in the day/week time grid, in pixels. */
const HOUR_HEIGHT = 48;

/** The grid opens scrolled to the start of a working day. */
const SCROLL_TO_HOUR = 8;

/** Clicking an empty slot books it on this grid, in minutes. */
const SLOT_MINUTES = 30;

/** A dragged task snaps to this grid, in minutes. */
const DROP_SNAP_MINUTES = 15;

/** What the create dialog opens with: a date, and a start time or "" for all day. */
interface Slot {
    date: string;
    time: string;
}

/** The task being dragged, and how far below its start it was grabbed. */
interface DragState {
    item: TaskWithLead;
    grabMinutes: number;
}

/**
 * Drag-to-reschedule, handed to every view. Only open tasks can be dragged:
 * a completed one is history, not a plan.
 */
interface DragControls {
    draggingId: number | null;
    current: () => DragState | null;
    begin: (item: TaskWithLead, event: DragEvent<HTMLElement>, grabMinutes?: number) => void;
    end: () => void;
    /** time is "HH:MM", "" for all day, or null to keep the task's own time. */
    drop: (date: string, time: string | null) => void;
    /** Sets a timed task's duration, from dragging its bottom edge. */
    resize: (item: TaskWithLead, minutes: number) => void;
}

/** Completed and cancelled events are history, not plans. */
const isClosed = (item: TaskWithLead) => item.task.status === "completed" || item.task.status === "cancelled";

const canDrag = (item: TaskWithLead) => !isClosed(item);

/** Lets a cell accept a drop: the browser refuses one unless dragover is cancelled. */
function acceptDrop(event: DragEvent<HTMLElement>) {
    event.preventDefault();
    event.dataTransfer.dropEffect = "move";
}

/** dragleave also fires when the pointer moves onto a child; ignore those. */
function leftElement(event: DragEvent<HTMLElement>): boolean {
    return !event.currentTarget.contains(event.relatedTarget as Node | null);
}

// ---------------------------------------------------------------------------
// Date helpers -- everything is a local calendar date, "YYYY-MM-DD"
// ---------------------------------------------------------------------------

function toISO(date: Date): string {
    const month = String(date.getMonth() + 1).padStart(2, "0");
    const day = String(date.getDate()).padStart(2, "0");
    return `${date.getFullYear()}-${month}-${day}`;
}

/** Parsed as a local date: new Date("YYYY-MM-DD") would be UTC midnight. */
function fromISO(iso: string): Date {
    const [year, month, day] = iso.split("-").map(Number);
    return new Date(year, month - 1, day);
}

function addDays(date: Date, days: number): Date {
    return new Date(date.getFullYear(), date.getMonth(), date.getDate() + days);
}

function startOfWeek(date: Date): Date {
    return addDays(date, -date.getDay());
}

/** The days shown: six full weeks for a month, seven for a week, one for a day. */
function visibleDays(anchor: Date, view: View): Date[] {
    if (view === "day") return [anchor];
    const first =
        view === "month"
            ? startOfWeek(new Date(anchor.getFullYear(), anchor.getMonth(), 1))
            : startOfWeek(anchor);
    const count = view === "month" ? 42 : 7;
    return Array.from({ length: count }, (_, index) => addDays(first, index));
}

function rangeLabel(anchor: Date, view: View): string {
    if (view === "month") {
        return anchor.toLocaleDateString(undefined, { month: "long", year: "numeric" });
    }
    if (view === "day") {
        return anchor.toLocaleDateString(undefined, { weekday: "long", month: "long", day: "numeric", year: "numeric" });
    }
    const start = startOfWeek(anchor);
    const end = addDays(start, 6);
    const sameMonth = start.getMonth() === end.getMonth();
    const startText = start.toLocaleDateString(undefined, { month: "short", day: "numeric" });
    const endText = end.toLocaleDateString(undefined, sameMonth ? { day: "numeric" } : { month: "short", day: "numeric" });
    return `${startText} – ${endText}, ${end.getFullYear()}`;
}

function nowMinutes(): number {
    const now = new Date();
    return now.getHours() * 60 + now.getMinutes();
}

// ---------------------------------------------------------------------------
// Task appearance and ordering
// ---------------------------------------------------------------------------

type TaskState = "done" | "cancelled" | "overdue" | "open";

/** The server works the status out, so a timed event is overdue once it ends. */
function stateOf(item: TaskWithLead): TaskState {
    switch (item.task.status) {
        case "completed":
            return "done";
        case "cancelled":
            return "cancelled";
        case "overdue":
            return "overdue";
        default:
            return "open";
    }
}

const CHIP_STYLES: Record<TaskState, string> = {
    cancelled:
        "border-l-slate-300 border-dashed bg-transparent text-slate-400 line-through hover:bg-slate-50 dark:border-l-slate-600 dark:text-slate-500 dark:hover:bg-slate-800/60",
    open: "border-l-brand-500 bg-brand-50 text-brand-800 hover:bg-brand-100 dark:bg-brand-950/50 dark:text-brand-200 dark:hover:bg-brand-900/60",
    overdue:
        "border-l-rose-500 bg-rose-50 text-rose-800 hover:bg-rose-100 dark:bg-rose-950/40 dark:text-rose-200 dark:hover:bg-rose-900/50",
    done: "border-l-teal-500 bg-slate-50 text-slate-400 line-through hover:bg-slate-100 dark:bg-slate-800/60 dark:text-slate-500 dark:hover:bg-slate-800",
};

/** Solid blocks in the time grid, so they stand out from the hour lines. */
const BLOCK_STYLES: Record<TaskState, string> = {
    cancelled:
        "border-dashed border-slate-300 bg-slate-50/70 text-slate-400 line-through hover:bg-slate-100 dark:border-slate-600 dark:bg-slate-900/60 dark:text-slate-500 dark:hover:bg-slate-800",
    open: "border-brand-600 bg-brand-100 text-brand-900 hover:bg-brand-200 dark:border-brand-400 dark:bg-brand-900/70 dark:text-brand-100 dark:hover:bg-brand-900",
    overdue:
        "border-rose-600 bg-rose-100 text-rose-900 hover:bg-rose-200 dark:border-rose-400 dark:bg-rose-900/60 dark:text-rose-100 dark:hover:bg-rose-900",
    done: "border-teal-500 bg-slate-100 text-slate-500 line-through hover:bg-slate-200 dark:bg-slate-800 dark:text-slate-400 dark:hover:bg-slate-700",
};

function startOf(item: TaskWithLead): number {
    return item.task.start_time ? minutesOfDay(item.task.start_time) : -1;
}

function endOf(item: TaskWithLead): number {
    return startOf(item) + (item.task.duration_minutes ?? DEFAULT_TASK_DURATION);
}

/** All-day tasks first, then by start time; open before done; then by id. */
function byTime(a: TaskWithLead, b: TaskWithLead): number {
    if (startOf(a) !== startOf(b)) return startOf(a) - startOf(b);
    const doneA = isClosed(a) ? 1 : 0;
    const doneB = isClosed(b) ? 1 : 0;
    if (doneA !== doneB) return doneA - doneB;
    return a.task.id - b.task.id;
}

/** "9:30 AM Call Priya" in a month cell; just the title for an all-day task. */
function chipLabel(item: TaskWithLead): string {
    return item.task.start_time ? `${formatClock(item.task.start_time)} ${item.task.title}` : item.task.title;
}

interface Placed {
    item: TaskWithLead;
    column: number;
    columns: number;
}

/**
 * Lays overlapping timed tasks side by side, the way a calendar does.
 *
 * Tasks that overlap, directly or through a chain, form a cluster. Each one goes
 * in the first column of its cluster that is free by its start time, and every
 * task in the cluster is drawn at 1/n of the width, n being the columns used.
 */
function layoutTimed(items: TaskWithLead[]): Placed[] {
    const sorted = [...items].sort((a, b) => startOf(a) - startOf(b) || endOf(b) - endOf(a));
    const placed: Placed[] = [];

    let cluster: Placed[] = [];
    let columnEnds: number[] = [];
    let clusterEnd = -1;

    const closeCluster = () => {
        for (const p of cluster) p.columns = columnEnds.length;
        cluster = [];
        columnEnds = [];
    };

    for (const item of sorted) {
        if (startOf(item) >= clusterEnd) closeCluster();

        let column = columnEnds.findIndex((end) => end <= startOf(item));
        if (column === -1) {
            column = columnEnds.length;
            columnEnds.push(endOf(item));
        } else {
            columnEnds[column] = endOf(item);
        }

        const p: Placed = { item, column, columns: 1 };
        cluster.push(p);
        placed.push(p);
        clusterEnd = Math.max(clusterEnd, endOf(item));
    }
    closeCluster();

    return placed;
}

/**
 * A calendar of every follow-up, laid out on its due date and, for timed
 * tasks, its time slot -- like a meetings calendar.
 *
 * The Tasks page is the prioritised work queue; this is the same data seen by
 * date, for planning a week and spotting days that are overloaded or empty.
 */
export function SchedulePage() {
    const toast = useToast();
    const today = todayISODate();

    const [view, setView] = useState<View>("week");
    const [anchor, setAnchor] = useState(() => fromISO(today));
    const [selected, setSelected] = useState(today);
    const [createSlot, setCreateSlot] = useState<Slot | null>(null);
    const [busyId, setBusyId] = useState<number | null>(null);
    const [editing, setEditing] = useState<TaskWithLead | null>(null);

    // Exactly the days on screen, whatever their status -- so completed and
    // cancelled events in older weeks show up too, not just the latest few.
    const days = visibleDays(anchor, view);
    const rangeFrom = toISO(days[0]);
    const rangeTo = toISO(days[days.length - 1]);
    const { data, error, loading, fetching, refetch, setData } = useAsync(
        (signal) => api.listTasksBetween(rangeFrom, rangeTo, signal),
        [rangeFrom, rangeTo],
    );

    const byDate = useMemo(() => {
        const map = new Map<string, TaskWithLead[]>();
        for (const item of data ?? []) {
            const list = map.get(item.task.due_date) ?? [];
            list.push(item);
            map.set(item.task.due_date, list);
        }
        for (const list of map.values()) list.sort(byTime);
        return map;
    }, [data]);

    const items = data ?? [];
    const openCount = items.filter((item) => !isClosed(item)).length;
    const overdueCount = items.filter((item) => stateOf(item) === "overdue").length;

    const step = (direction: -1 | 1) => {
        if (view === "month") {
            setAnchor((current) => new Date(current.getFullYear(), current.getMonth() + direction, 1));
        } else {
            const next = addDays(anchor, (view === "week" ? 7 : 1) * direction);
            setAnchor(next);
            if (view === "day") setSelected(toISO(next));
            if (view === "week") setSelected(toISO(addDays(fromISO(selected), 7 * direction)));
        }
    };

    const goToday = () => {
        setAnchor(fromISO(today));
        setSelected(today);
    };

    const selectDay = (iso: string) => {
        setSelected(iso);
        // Clicking a greyed day from the next/previous month moves the month there.
        const date = fromISO(iso);
        if (view === "month" && date.getMonth() !== anchor.getMonth()) setAnchor(date);
    };

    const changeView = (next: View) => {
        setView(next);
        // Day and week views centre on the selected day, so switching keeps it in view.
        setAnchor(fromISO(selected));
    };

    const replaceTask = (task: TaskWithLead["task"]) =>
        setData((current) => (current ?? []).map((i) => (i.task.id === task.id ? { ...i, task } : i)));

    /**
     * Moves a task to a new slot. The calendar updates at once and the save
     * happens behind it; if the server refuses, the task jumps back.
     */
    const moveTask = async (item: TaskWithLead, date: string, time: string | null) => {
        const before = item.task;
        const startTime = time === null ? (before.start_time ?? "") : time;
        if (date === before.due_date && startTime === (before.start_time ?? "")) return;

        const duration = startTime ? (before.duration_minutes ?? DEFAULT_TASK_DURATION) : null;
        replaceTask({ ...before, due_date: date, start_time: startTime || null, duration_minutes: duration });
        setSelected(date);

        try {
            const saved = await api.rescheduleTask(before.id, {
                due_date: date,
                ...(startTime && duration && { start_time: startTime, duration_minutes: duration }),
            });
            replaceTask(saved);
            const day = fromISO(date).toLocaleDateString(undefined, { weekday: "short", month: "short", day: "numeric" });
            toast.success(`Moved "${before.title}" to ${day}${startTime ? `, ${formatClock(startTime)}` : " (all day)"}.`);
        } catch (cause) {
            replaceTask(before);
            toast.error(cause instanceof ApiError ? cause.message : "Could not move the task.");
        }
    };

    /** Changes a timed task's length, keeping its date and start. Same optimistic save. */
    const resizeTask = async (item: TaskWithLead, minutes: number) => {
        const before = item.task;
        if (!before.start_time || minutes === before.duration_minutes) return;

        replaceTask({ ...before, duration_minutes: minutes });
        try {
            const saved = await api.rescheduleTask(before.id, {
                due_date: before.due_date,
                start_time: before.start_time,
                duration_minutes: minutes,
            });
            replaceTask(saved);
            toast.success(`"${before.title}" is now ${formatTimeSlot(saved)}.`);
        } catch (cause) {
            replaceTask(before);
            toast.error(cause instanceof ApiError ? cause.message : "Could not change the task's length.");
        }
    };

    // The drag lives in a ref so dragover, which fires many times a second,
    // can read it without re-rendering; draggingId is state only for styling.
    const dragRef = useRef<DragState | null>(null);
    const [draggingId, setDraggingId] = useState<number | null>(null);

    const drag: DragControls = {
        draggingId,
        current: () => dragRef.current,
        begin: (item, event, grabMinutes = 0) => {
            dragRef.current = { item, grabMinutes };
            event.dataTransfer.effectAllowed = "move";
            // Firefox will not start a drag without some data attached.
            event.dataTransfer.setData("text/plain", String(item.task.id));
            // Restyling the source inside dragstart can cancel the drag in
            // Chrome, so the "being dragged" look is applied a frame later.
            requestAnimationFrame(() => setDraggingId(item.task.id));
        },
        end: () => {
            dragRef.current = null;
            setDraggingId(null);
        },
        drop: (date, time) => {
            const state = dragRef.current;
            dragRef.current = null;
            setDraggingId(null);
            if (state) void moveTask(state.item, date, time);
        },
        resize: (item, minutes) => void resizeTask(item, minutes),
    };

    const toggle = async (item: TaskWithLead) => {
        const completing = item.task.status !== "completed";
        setBusyId(item.task.id);
        try {
            const result = await api.setTaskCompleted(item.task.id, completing);
            replaceTask(result.task);
            toast.success(completing ? `Done — logged on ${item.lead.name}'s timeline.` : `Reopened "${item.task.title}".`);
        } catch (cause) {
            toast.error(cause instanceof ApiError ? cause.message : "Could not update the task.");
        } finally {
            setBusyId(null);
        }
    };

    return (
        <>
            <PageHeader
                title="Schedule"
                subtitle={data ? `${openCount} open · ${overdueCount} overdue in view` : "Every follow-up, on its due date."}
                action={
                    <Button variant="primary" onClick={() => setCreateSlot({ date: selected, time: "" })}>
                        <Plus className="size-4" aria-hidden="true" />
                        Schedule task
                    </Button>
                }
            />

            {/* Toolbar: today, prev/next, the range, and the view switch. */}
            <div className="mb-4 flex flex-wrap items-center gap-2">
                <Button size="sm" onClick={goToday}>
                    Today
                </Button>
                <div className="flex">
                    <Button size="sm" variant="ghost" onClick={() => step(-1)} aria-label={`Previous ${view}`}>
                        <ChevronLeft className="size-4" aria-hidden="true" />
                    </Button>
                    <Button size="sm" variant="ghost" onClick={() => step(1)} aria-label={`Next ${view}`}>
                        <ChevronRight className="size-4" aria-hidden="true" />
                    </Button>
                </div>
                <h2 className="text-lg font-semibold text-slate-900 dark:text-slate-100" aria-live="polite">
                    {rangeLabel(anchor, view)}
                </h2>
                <div className="ml-auto">
                    <ViewToggle view={view} onChange={changeView} />
                </div>
            </div>

            {loading ? (
                <Skeleton className="h-[600px] w-full rounded-2xl" />
            ) : error ? (
                <ErrorState message={error.message} onRetry={refetch} />
            ) : (
                <div className={cx("grid gap-6 transition-opacity xl:grid-cols-[1fr_320px]", fetching && "opacity-60")}>
                    {view === "month" ? (
                        <MonthGrid
                            days={days}
                            anchor={anchor}
                            today={today}
                            selected={selected}
                            byDate={byDate}
                            drag={drag}
                            onEdit={setEditing}
                            onSelect={selectDay}
                            onAdd={(iso) => setCreateSlot({ date: iso, time: "" })}
                        />
                    ) : (
                        <TimeGrid
                            days={days}
                            today={today}
                            selected={selected}
                            byDate={byDate}
                            drag={drag}
                            onEdit={setEditing}
                            onSelect={selectDay}
                            onAdd={setCreateSlot}
                        />
                    )}

                    <DayAgenda
                        iso={selected}
                        tasks={byDate.get(selected) ?? []}
                        busyId={busyId}
                        onToggle={toggle}
                        onEdit={setEditing}
                        onAdd={() => setCreateSlot({ date: selected, time: "" })}
                    />
                </div>
            )}

            {data && items.some(canDrag) && (
                <p className="mt-3 text-xs text-slate-400 dark:text-slate-500">
                    Drag a task to another day or time to reschedule it, or drag its bottom edge to change how long it runs. Drop it on “All day” to clear its time.
                </p>
            )}

            <TaskEditModal
                task={editing?.task ?? null}
                subtitle={editing ? `${editing.lead.name} · ${editing.lead.company}` : undefined}
                onClose={() => setEditing(null)}
                onSaved={(saved, message) => {
                    replaceTask(saved);
                    setSelected(saved.due_date);
                    setAnchor(fromISO(saved.due_date));
                    setEditing(null);
                    toast.success(message);
                }}
                onDeleted={(deleted) => {
                    setData((current) => (current ?? []).filter((i) => i.task.id !== deleted.id));
                    setEditing(null);
                    toast.success(`Deleted "${deleted.title}".`);
                }}
            />

            <ScheduleTaskModal
                slot={createSlot}
                onClose={() => setCreateSlot(null)}
                onCreated={(item) => {
                    setData((current) => [...(current ?? []), item]);
                    setSelected(item.task.due_date);
                    setAnchor(fromISO(item.task.due_date));
                    setCreateSlot(null);
                    toast.success(`Scheduled "${item.task.title}".`);
                }}
            />
        </>
    );
}

// ---------------------------------------------------------------------------
// Toolbar
// ---------------------------------------------------------------------------

function ViewToggle({ view, onChange }: { view: View; onChange: (view: View) => void }) {
    const options: { value: View; label: string }[] = [
        { value: "day", label: "Day" },
        { value: "week", label: "Week" },
        { value: "month", label: "Month" },
    ];

    return (
        <div
            className="inline-flex rounded-lg border border-slate-200 bg-white p-0.5 dark:border-slate-700 dark:bg-slate-900"
            role="group"
            aria-label="Calendar view"
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

// ---------------------------------------------------------------------------
// Month view
// ---------------------------------------------------------------------------

function MonthGrid({
    days,
    anchor,
    today,
    selected,
    byDate,
    drag,
    onEdit,
    onSelect,
    onAdd,
}: {
    days: Date[];
    anchor: Date;
    today: string;
    selected: string;
    byDate: Map<string, TaskWithLead[]>;
    drag: DragControls;
    onEdit: (item: TaskWithLead) => void;
    onSelect: (iso: string) => void;
    onAdd: (iso: string) => void;
}) {
    return (
        <Card className="overflow-hidden">
            <div className="grid grid-cols-7 border-b border-slate-200 bg-slate-50 dark:border-slate-800 dark:bg-slate-900/60">
                {WEEKDAYS.map((day) => (
                    <div
                        key={day}
                        className="px-2 py-2 text-center text-[11px] font-semibold tracking-wide text-slate-500 uppercase dark:text-slate-400"
                    >
                        {day}
                    </div>
                ))}
            </div>

            <div className="grid grid-cols-7">
                {days.map((date) => {
                    const iso = toISO(date);
                    return (
                        <MonthCell
                            key={iso}
                            date={date}
                            isToday={iso === today}
                            isSelected={iso === selected}
                            outside={date.getMonth() !== anchor.getMonth()}
                            tasks={byDate.get(iso) ?? []}
                            drag={drag}
                            onEdit={onEdit}
                            onSelect={() => onSelect(iso)}
                            onAdd={() => onAdd(iso)}
                            onDrop={() => drag.drop(iso, null)}
                        />
                    );
                })}
            </div>
        </Card>
    );
}

function MonthCell({
    date,
    isToday,
    isSelected,
    outside,
    tasks,
    drag,
    onEdit,
    onSelect,
    onAdd,
    onDrop,
}: {
    date: Date;
    isToday: boolean;
    isSelected: boolean;
    outside: boolean;
    tasks: TaskWithLead[];
    drag: DragControls;
    onEdit: (item: TaskWithLead) => void;
    onSelect: () => void;
    onAdd: () => void;
    onDrop: () => void;
}) {
    const [dropping, setDropping] = useState(false);
    const shown = tasks.slice(0, MONTH_CELL_LIMIT);
    const hidden = tasks.length - shown.length;
    const label = date.toLocaleDateString(undefined, { weekday: "long", month: "long", day: "numeric" });

    return (
        <div
            role="button"
            tabIndex={0}
            aria-label={`${label}, ${tasks.length} task${tasks.length === 1 ? "" : "s"}`}
            aria-pressed={isSelected}
            onClick={onSelect}
            onKeyDown={(event) => {
                if (event.key === "Enter" || event.key === " ") {
                    event.preventDefault();
                    onSelect();
                }
            }}
            onDragOver={(event) => {
                if (!drag.current()) return;
                acceptDrop(event);
                setDropping(true);
            }}
            onDragLeave={(event) => leftElement(event) && setDropping(false)}
            onDrop={(event) => {
                event.preventDefault();
                setDropping(false);
                onDrop();
            }}
            className={cx(
                "group relative flex min-h-[104px] cursor-pointer flex-col gap-1 border-r border-b border-slate-100 p-1.5 text-left transition-colors outline-none focus-visible:ring-2 focus-visible:ring-brand-500 focus-visible:ring-inset sm:p-2 dark:border-slate-800",
                "[&:nth-child(7n)]:border-r-0",
                outside && "bg-slate-50/60 dark:bg-slate-950/40",
                isSelected ? "bg-brand-50/60 dark:bg-brand-950/30" : "hover:bg-slate-50 dark:hover:bg-slate-800/40",
                dropping && "bg-brand-100/70 ring-2 ring-brand-400 ring-inset dark:bg-brand-900/40",
            )}
        >
            <div className="flex items-center justify-between">
                <DayNumber date={date} isToday={isToday} muted={outside} />
                <AddButton label={label} onAdd={onAdd} />
            </div>

            {shown.map((item) => (
                <TaskChip key={item.task.id} item={item} drag={drag} label={chipLabel(item)} onEdit={onEdit} />
            ))}

            {hidden > 0 && (
                <span className="px-1.5 text-[11px] font-medium text-slate-500 dark:text-slate-400">
                    +{hidden} more
                </span>
            )}
        </div>
    );
}

/** A one-line task in a month cell or the all-day row; draggable while open. */
function TaskChip({
    item,
    drag,
    label,
    onEdit,
}: {
    item: TaskWithLead;
    drag: DragControls;
    label: string;
    onEdit: (item: TaskWithLead) => void;
}) {
    const draggable = canDrag(item);
    return (
        <span
            role="button"
            tabIndex={0}
            // The click also reaches the cell, which selects the day.
            onClick={() => onEdit(item)}
            onKeyDown={(event) => {
                if (event.key === "Enter" || event.key === " ") {
                    event.preventDefault();
                    event.stopPropagation();
                    onEdit(item);
                }
            }}
            draggable={draggable}
            onDragStart={(event) => drag.begin(item, event)}
            onDragEnd={drag.end}
            title={`${item.task.title} — ${item.lead.name}\nClick to edit${draggable ? ", drag to reschedule" : ""}`}
            className={cx(
                "block truncate rounded border-l-2 px-1.5 py-0.5 text-[11px] leading-tight font-medium",
                CHIP_STYLES[stateOf(item)],
                draggable && "cursor-grab active:cursor-grabbing",
                drag.draggingId === item.task.id && "opacity-40",
            )}
        >
            <TaskTypeIcon type={item.task.type} className="mr-1 size-3 align-[-2px]" />
            {label}
        </span>
    );
}

function DayNumber({ date, isToday, muted }: { date: Date; isToday: boolean; muted?: boolean }) {
    return (
        <span
            className={cx(
                "flex size-6 items-center justify-center rounded-full text-xs font-medium",
                isToday
                    ? "bg-brand-600 text-white dark:bg-brand-500"
                    : muted
                      ? "text-slate-400 dark:text-slate-600"
                      : "text-slate-700 dark:text-slate-300",
            )}
        >
            {date.getDate()}
        </span>
    );
}

function AddButton({ label, onAdd }: { label: string; onAdd: () => void }) {
    return (
        <button
            type="button"
            onClick={(event) => {
                event.stopPropagation();
                onAdd();
            }}
            aria-label={`Schedule a task on ${label}`}
            className="rounded p-0.5 text-slate-400 opacity-0 transition-opacity group-hover:opacity-100 hover:bg-slate-200 hover:text-slate-700 focus:opacity-100 dark:hover:bg-slate-700 dark:hover:text-slate-200"
        >
            <Plus className="size-3.5" aria-hidden="true" />
        </button>
    );
}

// ---------------------------------------------------------------------------
// Day / week view: an hourly grid with tasks as blocks, like a meetings calendar
// ---------------------------------------------------------------------------

function TimeGrid({
    days,
    today,
    selected,
    byDate,
    drag,
    onEdit,
    onSelect,
    onAdd,
}: {
    days: Date[];
    today: string;
    selected: string;
    byDate: Map<string, TaskWithLead[]>;
    drag: DragControls;
    onEdit: (item: TaskWithLead) => void;
    onSelect: (iso: string) => void;
    onAdd: (slot: Slot) => void;
}) {
    const scrollRef = useRef<HTMLDivElement>(null);
    const [now, setNow] = useState(nowMinutes);

    // Where the dragged task would land: a timed slot, or a day's all-day row.
    const [preview, setPreview] = useState<{ iso: string; minutes: number } | null>(null);
    const [allDayTarget, setAllDayTarget] = useState<string | null>(null);

    // A drag that ends anywhere (including outside the grid) clears the previews.
    useEffect(() => {
        if (drag.draggingId === null) {
            setPreview(null);
            setAllDayTarget(null);
        }
    }, [drag.draggingId]);

    // Open at the start of the working day rather than at midnight.
    useEffect(() => {
        if (scrollRef.current) scrollRef.current.scrollTop = SCROLL_TO_HOUR * HOUR_HEIGHT;
    }, []);

    // Keep the "now" line moving.
    useEffect(() => {
        const timer = window.setInterval(() => setNow(nowMinutes()), 60_000);
        return () => window.clearInterval(timer);
    }, []);

    const columns = `3.5rem repeat(${days.length}, minmax(0, 1fr))`;
    const wide = days.length === 1;

    /** Books the half-hour under the pointer. */
    const addAt = (iso: string, event: MouseEvent<HTMLDivElement>) => {
        const offset = event.clientY - event.currentTarget.getBoundingClientRect().top;
        const minutes = Math.floor((offset / HOUR_HEIGHT) * (60 / SLOT_MINUTES)) * SLOT_MINUTES;
        onAdd({ date: iso, time: timeFromMinutes(Math.min(Math.max(minutes, 0), 1440 - SLOT_MINUTES)) });
    };

    /**
     * The start time a drop here would give the dragged task: the pointer's
     * position, less where on the block it was grabbed, snapped to 15 minutes.
     */
    const dropMinutes = (event: DragEvent<HTMLDivElement>, state: DragState): number => {
        const offset = event.clientY - event.currentTarget.getBoundingClientRect().top;
        const raw = (offset / HOUR_HEIGHT) * 60 - state.grabMinutes;
        const snapped = Math.round(raw / DROP_SNAP_MINUTES) * DROP_SNAP_MINUTES;
        return Math.min(Math.max(snapped, 0), 1440 - DROP_SNAP_MINUTES);
    };

    const previewDuration = drag.current()?.item.task.duration_minutes ?? DEFAULT_TASK_DURATION;

    return (
        <Card className="overflow-hidden">
            <div className="overflow-x-auto">
                <div className={cx(!wide && "min-w-[640px]")}>
                    {/* Day headers */}
                    <div
                        className="grid border-b border-slate-200 bg-slate-50 dark:border-slate-800 dark:bg-slate-900/60"
                        style={{ gridTemplateColumns: columns }}
                    >
                        <div />
                        {days.map((date) => {
                            const iso = toISO(date);
                            return (
                                <button
                                    key={iso}
                                    type="button"
                                    onClick={() => onSelect(iso)}
                                    aria-pressed={iso === selected}
                                    className={cx(
                                        "flex flex-col items-center gap-0.5 border-l border-slate-200 py-2 transition-colors dark:border-slate-800",
                                        iso === selected
                                            ? "bg-brand-50/70 dark:bg-brand-950/40"
                                            : "hover:bg-slate-100 dark:hover:bg-slate-800/60",
                                    )}
                                >
                                    <span className="text-[11px] font-semibold tracking-wide text-slate-500 uppercase dark:text-slate-400">
                                        {WEEKDAYS[date.getDay()]}
                                    </span>
                                    <DayNumber date={date} isToday={iso === today} />
                                </button>
                            );
                        })}
                    </div>

                    {/* All-day row */}
                    <div
                        className="grid border-b border-slate-200 dark:border-slate-800"
                        style={{ gridTemplateColumns: columns }}
                    >
                        <div className="flex items-start justify-end pt-1.5 pr-2 text-[10px] font-medium text-slate-400 uppercase dark:text-slate-500">
                            All day
                        </div>
                        {days.map((date) => {
                            const iso = toISO(date);
                            const allDay = (byDate.get(iso) ?? []).filter((item) => !item.task.start_time);
                            return (
                                <div
                                    key={iso}
                                    className={cx(
                                        "min-h-9 space-y-1 border-l border-slate-200 p-1 transition-colors dark:border-slate-800",
                                        allDayTarget === iso && "bg-brand-100/70 ring-2 ring-brand-400 ring-inset dark:bg-brand-900/40",
                                    )}
                                    onClick={() => onSelect(iso)}
                                    onDragOver={(event) => {
                                        if (!drag.current()) return;
                                        acceptDrop(event);
                                        setAllDayTarget(iso);
                                    }}
                                    onDragLeave={(event) => leftElement(event) && setAllDayTarget(null)}
                                    onDrop={(event) => {
                                        event.preventDefault();
                                        drag.drop(iso, "");
                                    }}
                                >
                                    {allDay.map((item) => (
                                        <TaskChip key={item.task.id} item={item} drag={drag} label={item.task.title} onEdit={onEdit} />
                                    ))}
                                </div>
                            );
                        })}
                    </div>

                    {/* Hours */}
                    <div ref={scrollRef} className="max-h-[560px] overflow-y-auto">
                        <div className="relative grid" style={{ gridTemplateColumns: columns }}>
                            <div className="relative" style={{ height: 24 * HOUR_HEIGHT }}>
                                {Array.from({ length: 23 }, (_, index) => index + 1).map((hour) => (
                                    <span
                                        key={hour}
                                        className="absolute right-2 -translate-y-1/2 text-[10px] text-slate-400 dark:text-slate-500"
                                        style={{ top: hour * HOUR_HEIGHT }}
                                    >
                                        {formatClock(timeFromMinutes(hour * 60))}
                                    </span>
                                ))}
                            </div>

                            {days.map((date) => {
                                const iso = toISO(date);
                                const timed = (byDate.get(iso) ?? []).filter((item) => item.task.start_time);
                                return (
                                    <div
                                        key={iso}
                                        className={cx(
                                            "relative cursor-pointer border-l border-slate-200 dark:border-slate-800",
                                            iso === selected && "bg-brand-50/30 dark:bg-brand-950/20",
                                        )}
                                        style={{
                                            height: 24 * HOUR_HEIGHT,
                                            // Hour lines, drawn once as a background instead of 24 elements per day.
                                            backgroundImage: `repeating-linear-gradient(to bottom, transparent 0, transparent ${HOUR_HEIGHT - 1}px, var(--grid-line) ${HOUR_HEIGHT - 1}px, var(--grid-line) ${HOUR_HEIGHT}px)`,
                                        }}
                                        onClick={(event) => addAt(iso, event)}
                                        onDragOver={(event) => {
                                            const state = drag.current();
                                            if (!state) return;
                                            acceptDrop(event);
                                            const minutes = dropMinutes(event, state);
                                            // dragover fires constantly; only re-render when the slot changes.
                                            if (preview?.iso !== iso || preview.minutes !== minutes) {
                                                setPreview({ iso, minutes });
                                            }
                                        }}
                                        onDragLeave={(event) => leftElement(event) && setPreview(null)}
                                        onDrop={(event) => {
                                            event.preventDefault();
                                            const state = drag.current();
                                            if (state) drag.drop(iso, timeFromMinutes(dropMinutes(event, state)));
                                        }}
                                        title="Click to schedule a task at this time"
                                    >
                                        {layoutTimed(timed).map((placed) => (
                                            <TaskBlock
                                                key={placed.item.task.id}
                                                placed={placed}
                                                wide={wide}
                                                drag={drag}
                                                onSelect={() => onSelect(iso)}
                                                onEdit={() => onEdit(placed.item)}
                                            />
                                        ))}

                                        {preview?.iso === iso && (
                                            <div
                                                className="pointer-events-none absolute inset-x-0.5 z-20 rounded-md border-2 border-dashed border-brand-500 bg-brand-500/10 px-1.5 py-0.5 text-[11px] font-semibold text-brand-700 dark:text-brand-300"
                                                style={{
                                                    top: (preview.minutes / 60) * HOUR_HEIGHT,
                                                    height: Math.max(
                                                        (Math.min(previewDuration, 1440 - preview.minutes) / 60) * HOUR_HEIGHT,
                                                        18,
                                                    ),
                                                }}
                                                aria-hidden="true"
                                            >
                                                {formatClock(timeFromMinutes(preview.minutes))}
                                            </div>
                                        )}

                                        {iso === today && (
                                            <div
                                                className="pointer-events-none absolute inset-x-0 z-10 flex items-center"
                                                style={{ top: (now / 60) * HOUR_HEIGHT }}
                                                aria-hidden="true"
                                            >
                                                <span className="-ml-1 size-2 rounded-full bg-rose-500" />
                                                <span className="h-px flex-1 bg-rose-500" />
                                            </div>
                                        )}
                                    </div>
                                );
                            })}
                        </div>
                    </div>
                </div>
            </div>
        </Card>
    );
}

function TaskBlock({
    placed,
    wide,
    drag,
    onSelect,
    onEdit,
}: {
    placed: Placed;
    wide: boolean;
    drag: DragControls;
    onSelect: () => void;
    onEdit: () => void;
}) {
    const { item, column, columns } = placed;
    const start = startOf(item);
    const draggable = canDrag(item);
    const dragging = drag.draggingId !== null;

    // While the bottom edge is being dragged, the block follows the live length;
    // the ref holds the same value for the pointerup handler, and also tells
    // dragstart that this gesture is a resize, not a move.
    const [liveDuration, setLiveDuration] = useState<number | null>(null);
    const resizeRef = useRef<number | null>(null);
    const resizing = liveDuration !== null;

    const duration = liveDuration ?? item.task.duration_minutes ?? DEFAULT_TASK_DURATION;
    const slot = { start_time: item.task.start_time, duration_minutes: duration };
    // A slot running past midnight is cut off at the bottom of the day.
    const height = Math.max((Math.min(duration, 1440 - start) / 60) * HOUR_HEIGHT - 2, 18);
    const short = height < 36;

    const finishResize = (save: boolean) => {
        const minutes = resizeRef.current;
        resizeRef.current = null;
        setLiveDuration(null);
        if (save && minutes !== null) drag.resize(item, minutes);
    };

    return (
        <button
            type="button"
            draggable={draggable && !resizing}
            onDragStart={(event) => {
                if (resizeRef.current !== null) {
                    event.preventDefault();
                    return;
                }
                // Remember where on the block it was grabbed, so dropping keeps
                // that point under the pointer instead of snapping the top to it.
                const top = event.currentTarget.getBoundingClientRect().top;
                drag.begin(item, event, ((event.clientY - top) / HOUR_HEIGHT) * 60);
            }}
            onDragEnd={drag.end}
            onClick={(event) => {
                // Opening this task, not booking the slot behind it.
                event.stopPropagation();
                onSelect();
                onEdit();
            }}
            title={`${item.task.title} — ${item.lead.name}\n${formatTimeSlot(slot)}\nClick to edit${draggable ? ", drag to reschedule, or drag the bottom edge to change the length" : ""}`}
            className={cx(
                "group/block absolute overflow-hidden rounded-md border-l-[3px] px-1.5 py-0.5 text-left text-[11px] leading-tight shadow-sm transition-colors",
                BLOCK_STYLES[stateOf(item)],
                resizing ? "z-30 ring-2 ring-brand-400" : "z-[1]",
                draggable && "cursor-grab active:cursor-grabbing",
                // While dragging, blocks let the pointer through to the column
                // underneath, so the drop slot is measured against the day.
                dragging && "pointer-events-none",
                drag.draggingId === item.task.id && "opacity-40",
            )}
            style={{
                top: (start / 60) * HOUR_HEIGHT + 1,
                height,
                left: `calc(${(column / columns) * 100}% + 2px)`,
                width: `calc(${100 / columns}% - 4px)`,
            }}
        >
            <span className={cx("block font-semibold", short ? "truncate" : "line-clamp-2")}>
                <TaskTypeIcon type={item.task.type} className="mr-1 size-3 align-[-2px]" />
                {short && <span className="font-normal">{formatClock(item.task.start_time!)} </span>}
                {item.task.title}
            </span>
            {!short && (
                <span className="block truncate opacity-80">
                    {formatTimeSlot(slot)}
                    {wide && ` · ${item.lead.name}, ${item.lead.company}`}
                </span>
            )}
            {!short && !wide && <span className="block truncate opacity-70">{item.lead.name}</span>}

            {/* The new end time, shown while resizing even on a block too short for it. */}
            {resizing && short && (
                <span className="block truncate font-medium opacity-90">{formatTimeSlot(slot)}</span>
            )}

            {/* Resize handle: drag the bottom edge to change the length. Pointer
                events rather than drag-and-drop, so it follows the pointer
                smoothly and works with touch too. */}
            {draggable && (
                <span
                    aria-hidden="true"
                    className="absolute inset-x-0 bottom-0 flex h-2 cursor-ns-resize touch-none items-end justify-center"
                    onPointerDown={(event) => {
                        // Stops the block's own drag-to-move from starting.
                        event.preventDefault();
                        event.stopPropagation();
                        event.currentTarget.setPointerCapture(event.pointerId);
                        resizeRef.current = item.task.duration_minutes ?? DEFAULT_TASK_DURATION;
                        setLiveDuration(resizeRef.current);
                    }}
                    onPointerMove={(event) => {
                        if (resizeRef.current === null) return;
                        const blockTop = event.currentTarget.parentElement!.getBoundingClientRect().top;
                        const raw = ((event.clientY - blockTop) / HOUR_HEIGHT) * 60;
                        const snapped = Math.round(raw / DROP_SNAP_MINUTES) * DROP_SNAP_MINUTES;
                        const minutes = Math.min(Math.max(snapped, DROP_SNAP_MINUTES), 1440 - start);
                        if (minutes !== resizeRef.current) {
                            resizeRef.current = minutes;
                            setLiveDuration(minutes);
                        }
                    }}
                    onPointerUp={() => finishResize(true)}
                    onPointerCancel={() => finishResize(false)}
                    // The click that ends a resize should not also select the day.
                    onClick={(event) => event.stopPropagation()}
                >
                    <span
                        className={cx(
                            "mb-0.5 h-1 w-6 rounded-full bg-current transition-opacity",
                            resizing ? "opacity-60" : "opacity-0 group-hover/block:opacity-40",
                        )}
                    />
                </span>
            )}
        </button>
    );
}

// ---------------------------------------------------------------------------
// Selected day
// ---------------------------------------------------------------------------

function DayAgenda({
    iso,
    tasks,
    busyId,
    onToggle,
    onEdit,
    onAdd,
}: {
    iso: string;
    tasks: TaskWithLead[];
    busyId: number | null;
    onToggle: (item: TaskWithLead) => void;
    onEdit: (item: TaskWithLead) => void;
    onAdd: () => void;
}) {
    const title = fromISO(iso).toLocaleDateString(undefined, { weekday: "long", month: "long", day: "numeric" });

    return (
        <Card className="self-start">
            <CardHeader
                title={title}
                subtitle={`${tasks.length} task${tasks.length === 1 ? "" : "s"}`}
                action={
                    <Button size="sm" onClick={onAdd}>
                        <Plus className="size-3.5" aria-hidden="true" />
                        Add
                    </Button>
                }
            />
            {tasks.length === 0 ? (
                <EmptyState icon={CalendarDays} title="Nothing scheduled" description="Add a follow-up for this day." />
            ) : (
                <ul className="divide-y divide-slate-100 dark:divide-slate-800">
                    {tasks.map((item) => {
                        const { task, lead } = item;
                        const done = task.status === "completed";
                        const cancelled = task.status === "cancelled";
                        const busy = busyId === task.id;
                        return (
                            <li key={task.id} className={cx("group/row flex items-start gap-3 px-5 py-3", busy && "opacity-60")}>
                                {cancelled ? (
                                    <span
                                        className="mt-0.5 size-5 shrink-0 rounded-md border border-dashed border-slate-300 dark:border-slate-600"
                                        title="Cancelled"
                                        aria-hidden="true"
                                    />
                                ) : (
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
                                        className={cx("size-3.5", done ? "" : "text-teal-500 opacity-0 group-hover:opacity-100")}
                                        aria-hidden="true"
                                    />
                                </button>
                                )}
                                <div className="min-w-0 flex-1">
                                    <p className="flex items-center gap-1 text-xs font-medium text-slate-500 dark:text-slate-400">
                                        <Clock className="size-3" aria-hidden="true" />
                                        {formatTimeSlot(task) ?? "All day"}
                                        {task.type !== "todo" && <TaskTypeLabel type={task.type} />}
                                        {cancelled && <span>· Cancelled</span>}
                                        {!done && !cancelled && task.reminder_minutes && (
                                            <span className="inline-flex items-center gap-0.5" title="Reminder">
                                                · <Bell className="size-3" aria-hidden="true" />
                                                {formatReminder(task.reminder_minutes)}
                                            </span>
                                        )}
                                    </p>
                                    <p
                                        className={cx(
                                            "mt-0.5 text-sm break-words",
                                            done || cancelled
                                                ? "text-slate-400 line-through dark:text-slate-500"
                                                : stateOf(item) === "overdue"
                                                  ? "text-rose-700 dark:text-rose-300"
                                                  : "text-slate-900 dark:text-slate-100",
                                        )}
                                    >
                                        {task.title}
                                    </p>
                                    <p className="mt-0.5 truncate text-xs text-slate-500 dark:text-slate-400">
                                        <Link
                                            to={`/leads/${lead.id}`}
                                            className="font-medium text-brand-600 hover:underline dark:text-brand-400"
                                        >
                                            {lead.name}
                                        </Link>{" "}
                                        · {lead.company}
                                    </p>
                                    <div className="mt-1.5">
                                        <StageBadge stage={lead.stage} showIcon={false} />
                                    </div>
                                </div>
                                <button
                                    type="button"
                                    onClick={() => onEdit(item)}
                                    aria-label={`Edit "${task.title}"`}
                                    className="rounded-md p-1 text-slate-400 transition-opacity hover:bg-slate-100 hover:text-slate-700 focus:opacity-100 sm:opacity-0 sm:group-hover/row:opacity-100 dark:hover:bg-slate-800 dark:hover:text-slate-200"
                                >
                                    <Pencil className="size-3.5" aria-hidden="true" />
                                </button>
                            </li>
                        );
                    })}
                </ul>
            )}
        </Card>
    );
}

/** "· Call", with the type's icon, after a task's time in the day panel. */
function TaskTypeLabel({ type }: { type: TaskType }) {
    const { label, icon: Icon } = TASK_TYPE_STYLES[type];
    return (
        <span className="inline-flex items-center gap-1">
            <span aria-hidden="true">·</span>
            <Icon className="size-3" aria-hidden="true" />
            {label}
        </span>
    );
}

// ---------------------------------------------------------------------------
// Create
// ---------------------------------------------------------------------------

/**
 * Creates a task from the calendar. Tasks always belong to a lead, so this asks
 * for one; the leads are only fetched while the dialog is open.
 */
function ScheduleTaskModal({
    slot,
    onClose,
    onCreated,
}: {
    slot: Slot | null;
    onClose: () => void;
    onCreated: (item: TaskWithLead) => void;
}) {
    // Remounting the form per opening resets its fields to the clicked slot.
    return (
        <Modal open={slot !== null} onClose={onClose} title="Schedule a task" description="Book a follow-up with a lead.">
            {slot && (
                <ScheduleTaskForm key={`${slot.date} ${slot.time}`} slot={slot} onCancel={onClose} onCreated={onCreated} />
            )}
        </Modal>
    );
}

function ScheduleTaskForm({
    slot,
    onCancel,
    onCreated,
}: {
    slot: Slot;
    onCancel: () => void;
    onCreated: (item: TaskWithLead) => void;
}) {
    const [leadId, setLeadId] = useState("");
    const [title, setTitle] = useState("");
    // Booking a time slot on the calendar is usually a meeting; anything else
    // starts as a plain to-do.
    const [type, setType] = useState<TaskType>(slot.time ? "meeting" : "todo");
    const [dueDate, setDueDate] = useState(slot.date);
    const [startTime, setStartTime] = useState(slot.time);
    const [duration, setDuration] = useState(DEFAULT_TASK_DURATION);
    const [notes, setNotes] = useState("");
    // null means "the lead's owner"; the server fills that in.
    const [assigneeId, setAssigneeId] = useState<number | null>(null);
    // undefined until touched: follows the default of whoever it is for.
    const [reminderChoice, setReminderChoice] = useState<number | null | undefined>(undefined);
    const [errors, setErrors] = useState<Record<string, string>>({});
    const [saving, setSaving] = useState(false);
    const team = useTeam();

    const leads = useAsync(
        (signal) =>
            api
                .listLeads({ sort_by: "name", sort_dir: "asc", page_size: 100 }, signal)
                .then((result) => result.data),
        [],
    );

    const chosenLead = (leads.data ?? []).find((l: Lead) => String(l.id) === leadId) ?? null;
    const ownerName = memberName(team.data, chosenLead?.owner_id);
    const reminder =
        reminderChoice === undefined
            ? defaultReminderFor(team.data, assigneeId ?? chosenLead?.owner_id)
            : reminderChoice;

    const submit = async (event: FormEvent) => {
        event.preventDefault();
        const next: Record<string, string> = {};
        if (!leadId) next.lead = "Choose a lead.";
        if (!title.trim()) next.title = "Enter what needs doing.";
        if (!dueDate) next.due_date = "Pick a date.";
        setErrors(next);
        if (Object.keys(next).length > 0) return;

        const lead = chosenLead;
        if (!lead) return;

        setSaving(true);
        try {
            const task = await api.createTask(lead.id, {
                title: title.trim(),
                type,
                due_date: dueDate,
                ...(startTime && { start_time: startTime, duration_minutes: duration }),
                notes: notes.trim(),
                assignee_id: assigneeId,
                reminder_minutes: reminder,
            });
            onCreated({ task, lead });
        } catch (cause) {
            if (cause instanceof ApiError && cause.isValidationError && cause.details) {
                setErrors(cause.details);
            } else {
                setErrors({ form: cause instanceof ApiError ? cause.message : "Could not schedule the task." });
            }
        } finally {
            setSaving(false);
        }
    };

    const slotError = errors.start_time ?? errors.duration_minutes;

    return (
        <form onSubmit={submit} className="space-y-4" noValidate>
            <Field id="schedule-lead" label="Lead" required error={errors.lead}>
                <Select
                    id="schedule-lead"
                    value={leadId}
                    invalid={Boolean(errors.lead)}
                    disabled={leads.loading}
                    onChange={(event) => setLeadId(event.target.value)}
                >
                    <option value="">{leads.loading ? "Loading leads…" : "Choose a lead"}</option>
                    {(leads.data ?? []).map((lead) => (
                        <option key={lead.id} value={lead.id}>
                            {lead.name} — {lead.company}
                        </option>
                    ))}
                </Select>
            </Field>

            <Field id="schedule-title" label="Task" required error={errors.title}>
                <TextInput
                    id="schedule-title"
                    value={title}
                    maxLength={200}
                    invalid={Boolean(errors.title)}
                    placeholder="e.g. Discovery call"
                    onChange={(event) => setTitle(event.target.value)}
                    autoFocus
                />
            </Field>

            <div className="space-y-1.5">
                <p className="text-xs font-medium text-slate-700 dark:text-slate-300">Type</p>
                <TaskTypePicker value={type} onChange={setType} />
                {errors.type && <p className="text-xs text-rose-600 dark:text-rose-400">{errors.type}</p>}
            </div>

            <Field id="schedule-date" label="Date" required error={errors.due_date}>
                <TextInput
                    id="schedule-date"
                    type="date"
                    value={dueDate}
                    invalid={Boolean(errors.due_date)}
                    onChange={(event) => setDueDate(event.target.value)}
                />
            </Field>

            <div className="space-y-1.5">
                <p className="text-xs font-medium text-slate-700 dark:text-slate-300">Time</p>
                <TimeSlotPicker
                    idPrefix="schedule"
                    startTime={startTime}
                    duration={duration}
                    onChange={(nextStart, nextDuration) => {
                        setStartTime(nextStart);
                        setDuration(nextDuration);
                    }}
                />
                {slotError && <p className="text-xs text-rose-600 dark:text-rose-400">{slotError}</p>}
            </div>

            <div className="grid gap-4 sm:grid-cols-2">
                <Field id="schedule-assignee" label="Assigned to" error={errors.assignee_id}>
                    <AssigneeSelect
                        id="schedule-assignee"
                        value={assigneeId}
                        onChange={setAssigneeId}
                        team={team.data}
                        loading={team.loading}
                        emptyLabel={ownerName ? `Lead owner (${ownerName})` : "Lead owner"}
                        invalid={Boolean(errors.assignee_id)}
                    />
                </Field>
                <Field
                    id="schedule-reminder"
                    label="Reminder"
                    error={errors.reminder_minutes}
                    hint={startTime ? undefined : "All-day events are reminded about from 9:00 AM."}
                >
                    <ReminderSelect id="schedule-reminder" value={reminder} onChange={setReminderChoice} />
                </Field>
            </div>

            <Field id="schedule-notes" label="Notes / agenda" error={errors.notes}>
                <Textarea
                    id="schedule-notes"
                    rows={2}
                    maxLength={2000}
                    value={notes}
                    placeholder="What to cover, links, dial-in details…"
                    onChange={(event) => setNotes(event.target.value)}
                />
            </Field>

            {(errors.form || leads.error) && (
                <p className="text-sm text-rose-600 dark:text-rose-400" role="alert">
                    {errors.form ?? leads.error?.message}
                </p>
            )}

            <div className="flex justify-end gap-2 pt-2">
                <Button onClick={onCancel}>Cancel</Button>
                <Button type="submit" variant="primary" loading={saving}>
                    Schedule
                </Button>
            </div>
        </form>
    );
}
