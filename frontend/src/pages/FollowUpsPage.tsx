import { useCallback, useEffect, useState } from "react";
import { Link, useSearchParams } from "react-router-dom";
import {
    AlarmClock,
    CalendarClock,
    CalendarPlus,
    Check,
    CircleCheck,
    MessageSquarePlus,
    Snowflake,
    Sun,
    CalendarRange,
    Hourglass,
    ListPlus,
} from "lucide-react";
import type { LucideIcon } from "lucide-react";

import { ApiError, api } from "../lib/api";
import {
    addDaysISO,
    avatarColor,
    cx,
    formatDue,
    formatSilence,
    formatTimeSlot,
    initials,
} from "../lib/display";
import type { FollowUpSummary, Lead, TaskType, TaskWithLead, TeamMember, UserActivityType } from "../lib/types";
import { useAsync } from "../hooks/useAsync";
import { memberName, useTeam } from "../hooks/useTeam";
import { useToast } from "../context/toast";
import { HealthBadge, ScorePill, StuckBadge } from "../components/LeadInsights";
import { Modal } from "../components/Modal";
import { StageBadge } from "../components/StageBadge";
import { TaskTypeIcon, TaskTypePicker } from "../components/TaskTypePicker";
import {
    Button,
    Card,
    EmptyState,
    ErrorState,
    Field,
    PageHeader,
    RefreshError,
    Select,
    Skeleton,
    TextInput,
    Textarea,
} from "../components/ui";

// ---------------------------------------------------------------------------
// Buckets
// ---------------------------------------------------------------------------

type BucketKey = "overdue" | "today" | "week" | "stuck" | "cold" | "none";

interface Bucket {
    key: BucketKey;
    label: string;
    hint: string;
    icon: LucideIcon;
    /** Count and active-tab colours. */
    tone: string;
    ring: string;
    empty: string;
}

/** In the order they are shown, which is also the order of urgency. */
const BUCKETS: Bucket[] = [
    {
        key: "overdue",
        label: "Overdue",
        hint: "Follow-ups whose time has passed",
        icon: AlarmClock,
        tone: "text-rose-600 dark:text-rose-400",
        ring: "ring-rose-300 dark:ring-rose-800",
        empty: "Nothing overdue. Nice work.",
    },
    {
        key: "today",
        label: "Due today",
        hint: "Still to do today",
        icon: Sun,
        tone: "text-amber-600 dark:text-amber-400",
        ring: "ring-amber-300 dark:ring-amber-800",
        empty: "Nothing else due today.",
    },
    {
        key: "week",
        label: "This week",
        hint: "Due in the next 7 days",
        icon: CalendarRange,
        tone: "text-sky-600 dark:text-sky-400",
        ring: "ring-sky-300 dark:ring-sky-800",
        empty: "Nothing planned for the next 7 days.",
    },
    {
        key: "stuck",
        label: "Stuck deals",
        hint: "Open deals sitting in one stage for too long",
        icon: Hourglass,
        tone: "text-orange-600 dark:text-orange-400",
        ring: "ring-orange-300 dark:ring-orange-800",
        empty: "No deal has been in its stage for too long.",
    },
    {
        key: "cold",
        label: "Going cold",
        hint: "Open leads not contacted for too long",
        icon: Snowflake,
        tone: "text-indigo-600 dark:text-indigo-400",
        ring: "ring-indigo-300 dark:ring-indigo-800",
        empty: "Every open lead has been contacted recently.",
    },
    {
        key: "none",
        label: "No next step",
        hint: "Open leads with nothing planned",
        icon: ListPlus,
        tone: "text-slate-700 dark:text-slate-200",
        ring: "ring-slate-300 dark:ring-slate-600",
        empty: "Every open lead has a next step planned.",
    },
];

const BUCKET_KEYS = BUCKETS.map((bucket) => bucket.key);

function tasksIn(data: FollowUpSummary, key: BucketKey): TaskWithLead[] {
    if (key === "overdue") return data.overdue;
    if (key === "today") return data.due_today;
    if (key === "week") return data.this_week;
    return [];
}

function isLeadBucket(key: BucketKey): boolean {
    return key === "stuck" || key === "cold" || key === "none";
}

function leadsIn(data: FollowUpSummary, key: BucketKey): Lead[] {
    if (key === "stuck") return data.stuck;
    if (key === "cold") return data.going_cold;
    if (key === "none") return data.no_next_step;
    return [];
}

function countOf(data: FollowUpSummary, key: BucketKey): number {
    return isLeadBucket(key) ? leadsIn(data, key).length : tasksIn(data, key).length;
}

/** The most urgent bucket with anything in it, for when no tab was picked. */
function firstBusyBucket(data: FollowUpSummary): BucketKey {
    const order: BucketKey[] = ["overdue", "today", "stuck", "cold", "none", "week"];
    return order.find((key) => countOf(data, key) > 0) ?? "today";
}

/** The owner filter is remembered per browser, so "my follow-ups" sticks. */
const OWNER_KEY = "followups.owner";

function readStoredOwner(): string | null {
    try {
        return localStorage.getItem(OWNER_KEY);
    } catch {
        return null;
    }
}

function storeOwner(value: string) {
    try {
        if (value === "all") localStorage.removeItem(OWNER_KEY);
        else localStorage.setItem(OWNER_KEY, value);
    } catch {
        // Storage blocked: the filter still works for this visit via the URL.
    }
}

// ---------------------------------------------------------------------------
// Page
// ---------------------------------------------------------------------------

/**
 * The Follow-up Center: one place that answers "who do I contact now?".
 *
 * Tasks that are overdue, due today or due this week sit beside the open
 * leads that are going cold or have no next step, and every row can be dealt
 * with here -- done, snoozed, a contact logged, a next step planned -- without
 * opening the lead.
 */
export function FollowUpsPage() {
    const [searchParams, setSearchParams] = useSearchParams();
    const toast = useToast();
    const team = useTeam();

    // "all", "none" or a member id. Falls back to the last choice made here.
    const owner = searchParams.get("owner") ?? readStoredOwner() ?? "all";
    const ownerFilter: number | "none" | undefined =
        owner === "none" ? "none" : /^[1-9]\d*$/.test(owner) ? Number(owner) : undefined;

    const { data, error, loading, fetching, refetch } = useAsync(
        (signal) => api.followUps(ownerFilter, signal),
        [ownerFilter],
    );

    const tabParam = searchParams.get("tab");
    const tab: BucketKey | null = BUCKET_KEYS.includes(tabParam as BucketKey) ? (tabParam as BucketKey) : null;
    const active: BucketKey = tab ?? (data ? firstBusyBucket(data) : "overdue");

    const setParam = useCallback(
        (key: string, value: string | null) => {
            setSearchParams(
                (current) => {
                    const next = new URLSearchParams(current);
                    if (value === null) next.delete(key);
                    else next.set(key, value);
                    return next;
                },
                { replace: true },
            );
        },
        [setSearchParams],
    );

    const changeOwner = (value: string) => {
        storeOwner(value);
        setParam("owner", value === "all" ? null : value);
    };

    // A remembered member who has since been removed falls back to everyone.
    useEffect(() => {
        if (typeof ownerFilter === "number" && team.data && !team.data.some((m) => m.id === ownerFilter)) {
            storeOwner("all");
            setParam("owner", null);
        }
    }, [ownerFilter, team.data, setParam]);

    // --- row actions ----------------------------------------------------------

    const [busyId, setBusyId] = useState<string | null>(null);
    const [logFor, setLogFor] = useState<Lead | null>(null);
    const [planFor, setPlanFor] = useState<Lead | null>(null);

    const run = async (id: string, action: () => Promise<string>) => {
        setBusyId(id);
        try {
            toast.success(await action());
            refetch();
        } catch (cause) {
            toast.error(cause instanceof ApiError ? cause.message : "Something went wrong. Try again.");
        } finally {
            setBusyId(null);
        }
    };

    const complete = (item: TaskWithLead) =>
        run(`task-${item.task.id}`, async () => {
            await api.setTaskCompleted(item.task.id, true);
            return `Done — logged on ${item.lead.name}'s timeline.`;
        });

    /** Moves a task to the day after its due date, or to tomorrow if it is late. */
    const snooze = (item: TaskWithLead) => {
        if (!data) return;
        const from = item.task.due_date < data.today ? data.today : item.task.due_date;
        const due = addDaysISO(from, 1);
        return run(`task-${item.task.id}`, async () => {
            await api.rescheduleTask(item.task.id, {
                due_date: due,
                start_time: item.task.start_time ?? undefined,
                duration_minutes: item.task.duration_minutes ?? undefined,
            });
            return `"${item.task.title}" snoozed to ${formatDue(due).replace(/^Due /, "")}.`;
        });
    };

    // --- render ---------------------------------------------------------------

    const activeBucket = BUCKETS.find((bucket) => bucket.key === active)!;

    return (
        <>
            <PageHeader
                title="Follow-ups"
                subtitle={
                    data
                        ? `${data.overdue.length} overdue · ${data.due_today.length} due today · ${data.stuck.length} stuck · ${data.going_cold.length} going cold`
                        : "Who to contact, and what is due."
                }
                action={
                    <OwnerSelect
                        value={ownerFilter === undefined ? "all" : String(ownerFilter)}
                        team={team.data}
                        onChange={changeOwner}
                    />
                }
            />

            {loading ? (
                <FollowUpsSkeleton />
            ) : error && !data ? (
                <ErrorState message={error.message} onRetry={refetch} />
            ) : data ? (
                <>
                    {error && <RefreshError message={error.message} onRetry={refetch} className="mb-4" />}

                    {/* The buckets double as tabs: the count is the reason to click. */}
                    <div
                        role="tablist"
                        aria-label="Follow-up lists"
                        className="mb-6 grid grid-cols-2 gap-3 sm:grid-cols-3 xl:grid-cols-6"
                    >
                        {BUCKETS.map((bucket) => {
                            const selected = bucket.key === active;
                            const count = countOf(data, bucket.key);
                            return (
                                <button
                                    key={bucket.key}
                                    type="button"
                                    role="tab"
                                    id={`tab-${bucket.key}`}
                                    aria-selected={selected}
                                    aria-controls="followups-panel"
                                    onClick={() => setParam("tab", bucket.key)}
                                    className={cx(
                                        "rounded-2xl border bg-white p-4 text-left shadow-sm transition-colors dark:bg-slate-900",
                                        selected
                                            ? cx("border-transparent ring-2", bucket.ring)
                                            : "border-slate-200 hover:border-slate-300 dark:border-slate-800 dark:hover:border-slate-700",
                                    )}
                                >
                                    <span className="flex items-center gap-1.5 text-xs font-medium text-slate-500 dark:text-slate-400">
                                        <bucket.icon className={cx("size-3.5", bucket.tone)} aria-hidden="true" />
                                        {bucket.label}
                                    </span>
                                    <span
                                        className={cx(
                                            "tabular mt-1 block text-2xl font-semibold",
                                            count > 0 ? bucket.tone : "text-slate-300 dark:text-slate-600",
                                        )}
                                    >
                                        {count}
                                    </span>
                                </button>
                            );
                        })}
                    </div>

                    <Card className={cx("transition-opacity", fetching && "opacity-60")}>
                        <div
                            id="followups-panel"
                            role="tabpanel"
                            aria-labelledby={`tab-${active}`}
                        >
                            <div className="border-b border-slate-200 px-5 py-4 dark:border-slate-800">
                                <h2 className="text-sm font-semibold text-slate-900 dark:text-slate-100">
                                    {activeBucket.label}
                                </h2>
                                <p className="mt-0.5 text-xs text-slate-500 dark:text-slate-400">{activeBucket.hint}</p>
                            </div>

                            {isLeadBucket(active) ? (
                                <LeadRows
                                    leads={leadsIn(data, active)}
                                    team={team.data}
                                    empty={activeBucket.empty}
                                    onLog={setLogFor}
                                    onPlan={setPlanFor}
                                />
                            ) : (
                                <TaskRows
                                    items={tasksIn(data, active)}
                                    team={team.data}
                                    late={active === "overdue"}
                                    busyId={busyId}
                                    empty={activeBucket.empty}
                                    onComplete={complete}
                                    onSnooze={snooze}
                                />
                            )}
                        </div>
                    </Card>
                </>
            ) : null}

            <LogContactModal
                lead={logFor}
                onClose={() => setLogFor(null)}
                onSaved={(message) => {
                    toast.success(message);
                    refetch();
                }}
            />
            <PlanStepModal
                lead={planFor}
                today={data?.today}
                onClose={() => setPlanFor(null)}
                onSaved={(message) => {
                    toast.success(message);
                    refetch();
                }}
            />
        </>
    );
}

// ---------------------------------------------------------------------------
// Pieces
// ---------------------------------------------------------------------------

function OwnerSelect({
    value,
    team,
    onChange,
}: {
    value: string;
    team: TeamMember[] | null;
    onChange: (value: string) => void;
}) {
    return (
        <div className="w-full sm:w-56">
            <Select
                id="followups-owner"
                aria-label="Whose follow-ups"
                value={value}
                onChange={(event) => onChange(event.target.value)}
            >
                <option value="all">Everyone</option>
                {(team ?? []).map((member) => (
                    <option key={member.id} value={member.id}>
                        {member.name}
                    </option>
                ))}
                <option value="none">Nobody (unassigned)</option>
            </Select>
        </div>
    );
}

function OwnerTag({ name }: { name: string | null }) {
    if (!name) return <span className="text-slate-400 dark:text-slate-500">Unassigned</span>;
    return (
        <span className="inline-flex items-center gap-1">
            <span
                className={cx(
                    "flex size-4 items-center justify-center rounded-full text-[8px] font-semibold",
                    avatarColor(name),
                )}
                aria-hidden="true"
            >
                {initials(name)}
            </span>
            {name}
        </span>
    );
}

function TaskRows({
    items,
    team,
    late,
    busyId,
    empty,
    onComplete,
    onSnooze,
}: {
    items: TaskWithLead[];
    team: TeamMember[] | null;
    late: boolean;
    busyId: string | null;
    empty: string;
    onComplete: (item: TaskWithLead) => void;
    onSnooze: (item: TaskWithLead) => void;
}) {
    if (items.length === 0) {
        return <EmptyState icon={CircleCheck} title={empty} />;
    }

    return (
        <ul className="divide-y divide-slate-100 dark:divide-slate-800">
            {items.map((item) => {
                const { task, lead } = item;
                const busy = busyId === `task-${task.id}`;
                const slot = formatTimeSlot(task);

                return (
                    <li
                        key={task.id}
                        className={cx("flex flex-wrap items-start gap-x-3 gap-y-2 px-5 py-3.5", busy && "opacity-60")}
                    >
                        <button
                            type="button"
                            role="checkbox"
                            aria-checked={false}
                            aria-label={`Mark "${task.title}" done`}
                            title="Mark done"
                            disabled={busy}
                            onClick={() => onComplete(item)}
                            className="group mt-0.5 flex size-5 shrink-0 items-center justify-center rounded-md border border-slate-300 transition-colors hover:border-teal-500 dark:border-slate-600"
                        >
                            <Check className="size-3.5 text-teal-500 opacity-0 group-hover:opacity-100" aria-hidden="true" />
                        </button>

                        <div className="min-w-0 flex-1 basis-56">
                            <p className="text-sm break-words text-slate-900 dark:text-slate-100">
                                <TaskTypeIcon type={task.type} className="mr-1 size-3.5 align-[-2px]" />
                                {task.title}
                            </p>
                            <p className="mt-0.5 flex flex-wrap items-center gap-x-1.5 gap-y-0.5 text-xs text-slate-500 dark:text-slate-400">
                                <Link
                                    to={`/leads/${lead.id}`}
                                    className="font-medium text-brand-600 hover:underline dark:text-brand-400"
                                >
                                    {lead.name}
                                </Link>
                                <span>· {lead.company}</span>
                                <span>·</span>
                                <OwnerTag name={memberName(team, task.assignee_id)} />
                            </p>
                        </div>

                        <div className="flex shrink-0 flex-wrap items-center gap-2 pl-8 sm:pl-0">
                            <HealthBadge lead={lead} />
                            <span
                                className={cx(
                                    "inline-flex items-center gap-1 text-xs whitespace-nowrap",
                                    late ? "font-medium text-rose-600 dark:text-rose-400" : "text-slate-500 dark:text-slate-400",
                                )}
                            >
                                <CalendarClock className="size-3 shrink-0" aria-hidden="true" />
                                {formatDue(task.due_date)}
                                {slot && ` · ${slot}`}
                            </span>
                            <Button size="sm" variant="secondary" disabled={busy} onClick={() => onSnooze(item)}>
                                Snooze 1 day
                            </Button>
                        </div>
                    </li>
                );
            })}
        </ul>
    );
}

function LeadRows({
    leads,
    team,
    empty,
    onLog,
    onPlan,
}: {
    leads: Lead[];
    team: TeamMember[] | null;
    empty: string;
    onLog: (lead: Lead) => void;
    onPlan: (lead: Lead) => void;
}) {
    if (leads.length === 0) {
        return <EmptyState icon={CircleCheck} title={empty} />;
    }

    return (
        <ul className="divide-y divide-slate-100 dark:divide-slate-800">
            {leads.map((lead) => (
                <li key={lead.id} className="flex flex-wrap items-center gap-x-3 gap-y-2 px-5 py-3.5">
                    <span
                        className={cx(
                            "flex size-8 shrink-0 items-center justify-center rounded-full text-[11px] font-semibold",
                            avatarColor(lead.name),
                        )}
                        aria-hidden="true"
                    >
                        {initials(lead.name)}
                    </span>

                    <div className="min-w-0 flex-1 basis-48">
                        <Link
                            to={`/leads/${lead.id}`}
                            className="text-sm font-medium text-slate-900 hover:text-brand-700 dark:text-slate-100 dark:hover:text-brand-400"
                        >
                            {lead.name}
                        </Link>
                        <p className="mt-0.5 flex flex-wrap items-center gap-x-1.5 text-xs text-slate-500 dark:text-slate-400">
                            <span>{lead.company}</span>
                            <span>·</span>
                            <OwnerTag name={memberName(team, lead.owner_id)} />
                            <span>· {formatSilence(lead.days_since_touch)}</span>
                        </p>
                    </div>

                    <div className="flex shrink-0 flex-wrap items-center gap-2 pl-11 sm:pl-0">
                        <StageBadge stage={lead.stage} showIcon={false} />
                        <HealthBadge lead={lead} />
                        <StuckBadge lead={lead} />
                        <span className="w-20 shrink-0 text-right">
                            <ScorePill score={lead.score} />
                        </span>
                        <Button size="sm" variant="secondary" onClick={() => onLog(lead)}>
                            <MessageSquarePlus className="size-3.5" aria-hidden="true" />
                            Log contact
                        </Button>
                        {!lead.has_open_task && (
                            <Button size="sm" variant="primary" onClick={() => onPlan(lead)}>
                                <CalendarPlus className="size-3.5" aria-hidden="true" />
                                Plan next step
                            </Button>
                        )}
                    </div>
                </li>
            ))}
        </ul>
    );
}

const CONTACT_TYPES: { value: UserActivityType; label: string }[] = [
    { value: "call", label: "Call" },
    { value: "email", label: "Email" },
    { value: "meeting", label: "Meeting" },
    { value: "note", label: "Note" },
];

/** Logs a call, email, meeting or note on a lead's timeline, which resets its health clock. */
function LogContactModal({
    lead,
    onClose,
    onSaved,
}: {
    lead: Lead | null;
    onClose: () => void;
    onSaved: (message: string) => void;
}) {
    const [type, setType] = useState<UserActivityType>("call");
    const [note, setNote] = useState("");
    const [saving, setSaving] = useState(false);
    const [error, setError] = useState<string | null>(null);

    // A fresh form each time a lead is picked.
    useEffect(() => {
        if (lead) {
            setType("call");
            setNote("");
            setError(null);
        }
    }, [lead]);

    const save = async () => {
        if (!lead || !note.trim() || saving) return;
        setSaving(true);
        setError(null);
        try {
            await api.createActivity(lead.id, { type, note: note.trim() });
            onClose();
            onSaved(`Logged on ${lead.name}'s timeline.`);
        } catch (cause) {
            setError(cause instanceof Error ? cause.message : "Could not save.");
        } finally {
            setSaving(false);
        }
    };

    return (
        <Modal open={lead !== null} onClose={onClose} title={lead ? `Log contact with ${lead.name}` : "Log contact"}>
            <form
                onSubmit={(event) => {
                    event.preventDefault();
                    void save();
                }}
            >
                <div className="space-y-4 px-5 py-5">
                    <Field id="log-type" label="Type">
                        <Select id="log-type" value={type} onChange={(event) => setType(event.target.value as UserActivityType)}>
                            {CONTACT_TYPES.map((option) => (
                                <option key={option.value} value={option.value}>
                                    {option.label}
                                </option>
                            ))}
                        </Select>
                    </Field>
                    <Field id="log-note" label="What happened?" error={error ?? undefined} required>
                        <Textarea
                            id="log-note"
                            rows={3}
                            value={note}
                            maxLength={2000}
                            autoFocus
                            invalid={Boolean(error)}
                            placeholder="e.g. Spoke to them about pricing - they will confirm on Friday."
                            onChange={(event) => setNote(event.target.value)}
                        />
                    </Field>
                </div>
                <div className="flex justify-end gap-2 border-t border-slate-200 bg-slate-50 px-5 py-3.5 dark:border-slate-800 dark:bg-slate-950/50">
                    <Button variant="secondary" onClick={onClose} disabled={saving}>
                        Cancel
                    </Button>
                    <Button type="submit" variant="primary" loading={saving} disabled={!note.trim()}>
                        Log it
                    </Button>
                </div>
            </form>
        </Modal>
    );
}

/** Schedules the lead's next step. It goes to the lead's owner, as everywhere else. */
function PlanStepModal({
    lead,
    today,
    onClose,
    onSaved,
}: {
    lead: Lead | null;
    today: string | undefined;
    onClose: () => void;
    onSaved: (message: string) => void;
}) {
    const [title, setTitle] = useState("");
    const [type, setType] = useState<TaskType>("follow_up");
    const [due, setDue] = useState("");
    const [saving, setSaving] = useState(false);
    const [error, setError] = useState<string | null>(null);

    useEffect(() => {
        if (lead) {
            setTitle(`Follow up with ${lead.name}`);
            setType("follow_up");
            setDue(today ? addDaysISO(today, 1) : "");
            setError(null);
        }
    }, [lead, today]);

    const save = async () => {
        if (!lead || !title.trim() || !due || saving) return;
        setSaving(true);
        setError(null);
        try {
            await api.createTask(lead.id, { title: title.trim(), type, due_date: due });
            onClose();
            onSaved(`Next step planned for ${lead.name}.`);
        } catch (cause) {
            setError(cause instanceof Error ? cause.message : "Could not save.");
        } finally {
            setSaving(false);
        }
    };

    return (
        <Modal open={lead !== null} onClose={onClose} title={lead ? `Next step for ${lead.name}` : "Next step"}>
            <form
                onSubmit={(event) => {
                    event.preventDefault();
                    void save();
                }}
            >
                <div className="space-y-4 px-5 py-5">
                    <Field id="plan-title" label="What" required error={error ?? undefined}>
                        <TextInput
                            id="plan-title"
                            value={title}
                            maxLength={200}
                            autoFocus
                            invalid={Boolean(error)}
                            onChange={(event) => setTitle(event.target.value)}
                        />
                    </Field>
                    <div className="space-y-1.5">
                        <span className="block text-xs font-medium text-slate-700 dark:text-slate-300">Type</span>
                        <TaskTypePicker value={type} onChange={setType} compact />
                    </div>
                    <Field id="plan-due" label="When" required>
                        <div className="flex flex-wrap items-center gap-2">
                            <div className="w-44">
                                <TextInput
                                    id="plan-due"
                                    type="date"
                                    value={due}
                                    onChange={(event) => setDue(event.target.value)}
                                />
                            </div>
                            {today &&
                                [
                                    { label: "Tomorrow", days: 1 },
                                    { label: "In 3 days", days: 3 },
                                    { label: "Next week", days: 7 },
                                ].map((option) => (
                                    <Button
                                        key={option.days}
                                        size="sm"
                                        variant={due === addDaysISO(today, option.days) ? "primary" : "ghost"}
                                        onClick={() => setDue(addDaysISO(today, option.days))}
                                    >
                                        {option.label}
                                    </Button>
                                ))}
                        </div>
                    </Field>
                </div>
                <div className="flex justify-end gap-2 border-t border-slate-200 bg-slate-50 px-5 py-3.5 dark:border-slate-800 dark:bg-slate-950/50">
                    <Button variant="secondary" onClick={onClose} disabled={saving}>
                        Cancel
                    </Button>
                    <Button type="submit" variant="primary" loading={saving} disabled={!title.trim() || !due}>
                        Plan it
                    </Button>
                </div>
            </form>
        </Modal>
    );
}

function FollowUpsSkeleton() {
    return (
        <>
            <div className="mb-6 grid grid-cols-2 gap-3 sm:grid-cols-3 xl:grid-cols-6">
                {BUCKETS.map((bucket) => (
                    <Card key={bucket.key} className="p-4">
                        <Skeleton className="h-3 w-20" />
                        <Skeleton className="mt-2 h-7 w-10" />
                    </Card>
                ))}
            </div>
            <Card>
                <div className="divide-y divide-slate-100 dark:divide-slate-800">
                    {Array.from({ length: 4 }).map((_, index) => (
                        <div key={index} className="flex items-center gap-3 px-5 py-3.5">
                            <Skeleton className="size-5 rounded-md" />
                            <div className="flex-1 space-y-1.5">
                                <Skeleton className="h-3.5 w-48" />
                                <Skeleton className="h-3 w-32" />
                            </div>
                            <Skeleton className="h-7 w-28" />
                        </div>
                    ))}
                </div>
            </Card>
        </>
    );
}
