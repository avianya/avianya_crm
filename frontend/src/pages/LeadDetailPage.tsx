import { useRef, useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import {
    ArrowLeft,
    ArrowRight,
    Building2,
    Calendar,
    Clock,
    Hourglass,
    IndianRupee,
    Mail,
    Pencil,
    Phone,
    StickyNote,
    Trash2,
    UserRound,
    UserX,
} from "lucide-react";
import type { LucideIcon } from "lucide-react";

import { ApiError, api } from "../lib/api";
import {
    SOURCE_STYLES,
    STAGE_STYLES,
    avatarColor,
    cx,
    formatDateTime,
    formatMoney,
    formatRelative,
    initials,
    isClosedStage,
    nextStage,
    stageBeforeClosing,
} from "../lib/display";
import type {
    Activity,
    ActivityInput,
    Lead,
    LeadEmailResult,
    LeadInput,
    Stage,
    TeamMember,
    UserActivityType,
} from "../lib/types";
import { useAsync } from "../hooks/useAsync";
import { useToast } from "../context/toast";
import { LeadMetaCard, LeadQualityCard } from "../components/LeadQualityCard";
import { useAuth } from "../context/auth";
import { ActivityComposer, ActivityTimeline } from "../components/ActivityTimeline";
import { ConfirmDialog } from "../components/ConfirmDialog";
import { LeadFormModal } from "../components/LeadFormModal";
import { SendEmailModal } from "../components/SendEmailModal";
import { useCloseReasonPrompt } from "../hooks/useCloseReasonPrompt";
import { reasonLabel } from "../lib/closeReasons";
import { StageHistoryCard } from "../components/StageHistory";
import { StageProgress } from "../components/StageProgress";
import { StagePicker } from "../components/StagePicker";
import { CustomerLinksCard } from "../components/CustomerLinksCard";
import { NextStepsCard } from "../components/NextStepsCard";
import { TaskList } from "../components/TaskList";
import type { OpenEventRequest, TaskDraft } from "../components/TaskList";
import { memberName, useTeam } from "../hooks/useTeam";
import { HealthBadge, ScoreCard, StuckBadge } from "../components/LeadInsights";
import {
    Button,
    Card,
    CardHeader,
    EmptyState,
    ErrorState,
    RefreshError,
    Select,
    Skeleton,
    Textarea,
} from "../components/ui";

/** The activity types offered in the composer, matching model.UserActivityTypes. */
const ACTIVITY_OPTIONS: { value: UserActivityType; label: string }[] = [
    { value: "note", label: "Note" },
    { value: "call", label: "Call" },
    { value: "email", label: "Email" },
    { value: "meeting", label: "Meeting" },
];

/**
 * The full profile of one lead: contact details, where it is in the lifecycle, and
 * everything that has ever happened to it.
 *
 * The single GET /api/leads/:id returns the lead with its timeline already
 * attached, so this whole screen is one request rather than two.
 *
 * The route renders it with key={id} (see App.tsx), so moving from one lead to
 * another -- e.g. from a notification -- mounts a fresh page: no flash of the
 * previous lead, and no half-written draft or open dialog carried across.
 */
export function LeadDetailPage() {
    const { is } = useAuth();
    // Reassigning and deleting are for admins and managers.
    const canAssign = is("admin", "manager");
    // Route params always arrive as strings; the API needs a number.
    const { id } = useParams<{ id: string }>();
    const leadId = Number(id);

    const navigate = useNavigate();
    const toast = useToast();

    const [editOpen, setEditOpen] = useState(false);
    const [emailOpen, setEmailOpen] = useState(false);
    const [confirmDelete, setConfirmDelete] = useState(false);
    const [stageBusy, setStageBusy] = useState(false);

    // Bumped after anything that can change the suggested next steps.
    const [version, setVersion] = useState(0);
    const [draft, setDraft] = useState<TaskDraft | null>(null);
    const [openRequest, setOpenRequest] = useState<OpenEventRequest | null>(null);
    const team = useTeam();

    const { data: lead, error, loading, refetch, setData } = useAsync(
        (signal) => api.getLead(leadId, signal),
        [leadId],
    );

    // Win probabilities come from the backend, so the "how much is this worth
    // to the forecast" line uses exactly the numbers the dashboard does.
    const { data: meta } = useAsync((signal) => api.meta(signal), []);

    // Asks why, whenever the lead is about to be won or lost.
    const closeReason = useCloseReasonPrompt();

    // --- mutations ----------------------------------------------------------

    /**
     * Moves the lead to another stage, asking why first when it is closing.
     * With changingReason, it stays in its closed stage and only the reason
     * changes.
     */
    const handleStageChange = async (next: Stage, changingReason = false) => {
        if (!lead || (next === lead.stage && !changingReason)) return;

        const close = await closeReason.ask(lead, next, { changing: changingReason });
        if (close === null) return; // cancelled: the deal stays where it is

        setStageBusy(true);
        try {
            const updated = await api.updateLeadStage(lead.id, next, close);
            // Show the move at once; the timeline entry follows below.
            setData((current) => ({ ...updated, activities: current?.activities ?? [] }));
            toast.success(
                changingReason ? "Reason updated." : `Moved to ${STAGE_STYLES[next].label}.`,
            );
        } catch (cause) {
            toast.error(
                cause instanceof ApiError ? cause.message : "Could not change the stage.",
            );
            setStageBusy(false);
            return;
        }

        // The stage response does not include the timeline or stage history,
        // but the server has just added to both. Fetched separately, so a
        // failure here cannot be mistaken for the stage change failing.
        try {
            const fresh = await api.getLead(lead.id);
            setData(fresh);
        } catch {
            // The move itself succeeded; the timeline catches up on the next load.
        } finally {
            setStageBusy(false);
            setVersion((v) => v + 1);
        }
    };

    const handleEdit = async (input: LeadInput) => {
        if (!lead) return;
        const updated = await api.updateLead(lead.id, input);
        // PUT returns the lead with its timeline, so this is a complete refresh.
        setData(updated);
        setVersion((v) => v + 1);
        toast.success("Lead updated.");
    };

    /** Prepends a timeline entry the server has just written. */
    const prependActivity = (activity: Activity) => {
        setData((current) =>
            current
                ? { ...current, activities: [activity, ...(current.activities ?? [])] }
                : current!,
        );
    };

    /**
     * Re-reads the lead after a change made elsewhere on the page.
     *
     * Health and score are derived on the server from the timeline and the
     * tasks, so logging a call or ticking off a task changes them. One GET
     * brings back the lead, its timeline and both signals together. Failure is
     * quiet: the change itself already succeeded and was confirmed with a toast.
     */
    const refreshSeq = useRef(0);
    const refreshLead = () => {
        setVersion((v) => v + 1);
        // Two quick changes start two refreshes; only the later one may land,
        // or an older response could overwrite a newer one.
        const seq = ++refreshSeq.current;
        api.getLead(leadId)
            .then((fresh) => {
                if (seq === refreshSeq.current) setData(fresh);
            })
            .catch(() => {});
    };

    const handleLogActivity = async (input: ActivityInput) => {
        if (!lead) return;

        const activity = await api.createActivity(lead.id, input);

        // Prepend straight away so the entry appears instantly, then refresh in
        // the background: this contact has just reset the lead's health clock.
        prependActivity(activity);
        toast.success("Activity logged.");
        refreshLead();
    };

    /** An email was sent or logged: it is on the timeline, and counts as contact. */
    const handleEmailDone = (result: LeadEmailResult) => {
        setEmailOpen(false);
        prependActivity(result.activity);
        toast.success(
            result.sent ? `Email sent to ${result.email.to}.` : "Email logged on the timeline.",
        );
        refreshLead();
    };

    /** Gives the lead to someone else; the server logs it on the timeline. */
    const handleOwnerChange = async (ownerId: number | null) => {
        if (!lead || ownerId === lead.owner_id) return;
        const updated = await api.updateLeadOwner(lead.id, ownerId);
        setData(updated);
        // New events default to the owner, so suggestions may change too.
        setVersion((v) => v + 1);
        const name = memberName(team.data, ownerId);
        toast.success(name ? `${name} now owns this lead.` : "Owner removed.");
    };

    const handleNotesSave = async (notes: string) => {
        if (!lead) return;
        const updated = await api.updateLeadNotes(lead.id, notes);
        setData(updated);
        setVersion((v) => v + 1);
        toast.success(notes ? "Notes saved." : "Notes cleared.");
    };

    const handleEditActivity = async (activity: Activity, note: string) => {
        const updated = await api.updateActivity(activity.id, note);
        setData((current) =>
            current
                ? {
                      ...current,
                      activities: (current.activities ?? []).map((a) => (a.id === updated.id ? updated : a)),
                  }
                : current!,
        );
        toast.success("Entry updated.");
    };

    const handleDeleteActivity = async (activity: Activity) => {
        try {
            await api.deleteActivity(activity.id);
        } catch (cause) {
            toast.error(cause instanceof ApiError ? cause.message : "Could not delete the entry.");
            throw cause; // keeps the dialog open
        }
        setData((current) =>
            current
                ? { ...current, activities: (current.activities ?? []).filter((a) => a.id !== activity.id) }
                : current!,
        );
        toast.success("Entry deleted.");
        // Removing a call or note can change when the lead was last contacted.
        refreshLead();
    };

    const handleDelete = async () => {
        if (!lead) return;
        try {
            await api.deleteLead(lead.id);
            toast.success(`${lead.name} was deleted.`);
            // Nothing left to show on this route, so go back to the list.
            navigate("/leads");
        } catch (cause) {
            toast.error(
                cause instanceof ApiError ? cause.message : "Could not delete the lead.",
            );
            throw cause;
        }
    };

    // --- render -------------------------------------------------------------

    if (loading) return <DetailSkeleton />;

    // Only a failed first load replaces the page. If a later reload fails, the
    // lead already on screen stays, with a notice above it.
    if (error && !lead) {
        const notFound = error instanceof ApiError && error.isNotFound;

        return (
            <>
                <BackLink />
                {notFound ? (
                    <Card>
                        <EmptyState
                            icon={UserX}
                            title="Lead not found"
                            description="It may have been deleted, or the link may be wrong."
                            action={
                                <Button variant="primary" onClick={() => navigate("/leads")}>
                                    Back to leads
                                </Button>
                            }
                        />
                    </Card>
                ) : (
                    <ErrorState message={error.message} onRetry={refetch} />
                )}
            </>
        );
    }

    if (!lead) return null;

    const suggested = nextStage(lead.stage);
    const sourceStyle = SOURCE_STYLES[lead.source];
    const closedFrom = stageBeforeClosing(lead.stage_history);

    return (
        <>
            <BackLink />

            {error && <RefreshError message={error.message} onRetry={refetch} className="mb-4" />}

            {/* --- Header --- */}
            <Card className="mb-6 overflow-hidden">
                {/* A thin bar in the stage colour: the lead's status is legible
                    from the far side of the room, before any text is read. */}
                <div
                    className={cx("h-1 w-full transition-colors duration-500", STAGE_STYLES[lead.stage].solid)}
                    aria-hidden="true"
                />

                <div className="flex flex-col gap-5 p-5 lg:flex-row lg:items-start lg:justify-between">
                    <div className="flex min-w-0 items-start gap-4">
                        <span
                            className={cx(
                                "flex size-14 shrink-0 items-center justify-center rounded-2xl text-lg font-semibold",
                                avatarColor(lead.name),
                            )}
                            aria-hidden="true"
                        >
                            {initials(lead.name)}
                        </span>

                        <div className="min-w-0">
                            <h1 className="truncate text-xl font-semibold tracking-tight text-slate-900 dark:text-slate-50">
                                {lead.name}
                            </h1>
                            <p className="mt-0.5 flex items-center gap-1.5 text-sm text-slate-500 dark:text-slate-400">
                                <Building2 className="size-3.5 shrink-0" aria-hidden="true" />
                                <span className="truncate">{lead.company}</span>
                            </p>

                            <div className="mt-3 flex flex-wrap items-center gap-2">
                                <StagePicker
                                    stage={lead.stage}
                                    busy={stageBusy}
                                    onChange={(stage) => handleStageChange(stage)}
                                />
                                <span className="inline-flex items-center gap-1.5 rounded-full bg-slate-100 px-2.5 py-1 text-xs font-medium text-slate-600 dark:bg-slate-800 dark:text-slate-300">
                                    <sourceStyle.icon className="size-3" aria-hidden="true" />
                                    {sourceStyle.label}
                                </span>
                                <HealthBadge lead={lead} showDays />
                                <StuckBadge lead={lead} />
                            </div>
                        </div>
                    </div>

                    <div className="flex shrink-0 flex-wrap gap-2">
                        {/* The single most likely action on this page, offered as
                            one click instead of a dropdown interaction. */}
                        {suggested && (
                            <Button
                                variant="primary"
                                loading={stageBusy}
                                onClick={() => handleStageChange(suggested)}
                            >
                                Advance to {STAGE_STYLES[suggested].label}
                                <ArrowRight className="size-4" aria-hidden="true" />
                            </Button>
                        )}
                        <Button variant="secondary" onClick={() => setEmailOpen(true)}>
                            <Mail className="size-4" aria-hidden="true" />
                            Email
                        </Button>
                        <Button variant="secondary" onClick={() => setEditOpen(true)}>
                            <Pencil className="size-4" aria-hidden="true" />
                            Edit
                        </Button>
                        {canAssign && (
                            <Button variant="ghost" onClick={() => setConfirmDelete(true)}>
                                <Trash2 className="size-4" aria-hidden="true" />
                                <span className="sr-only sm:not-sr-only">Delete</span>
                            </Button>
                        )}
                    </div>
                </div>
            </Card>

            {lead.stuck && (
                <div
                    role="status"
                    className="mb-6 flex items-start gap-3 rounded-xl border border-orange-200 bg-orange-50 px-4 py-3 text-sm text-orange-900 dark:border-orange-900 dark:bg-orange-950/40 dark:text-orange-200"
                >
                    <Hourglass className="mt-0.5 size-4 shrink-0" aria-hidden="true" />
                    <p>
                        <span className="font-medium">
                            Stuck in {STAGE_STYLES[lead.stage].label} for {lead.days_in_stage} days
                        </span>{" "}
                        (deals here are flagged after {lead.stuck_limit_days}). Plan a next step to move it
                        forward, or update the stage if it has already moved on.
                    </p>
                </div>
            )}

            {isClosedStage(lead.stage) && (
                <div
                    role="status"
                    className={cx(
                        "mb-6 flex flex-wrap items-start gap-x-3 gap-y-1 rounded-xl border px-4 py-3 text-sm",
                        lead.stage === "lost"
                            ? "border-rose-200 bg-rose-50 text-rose-900 dark:border-rose-900 dark:bg-rose-950/40 dark:text-rose-200"
                            : "border-emerald-200 bg-emerald-50 text-emerald-900 dark:border-emerald-900 dark:bg-emerald-950/40 dark:text-emerald-200",
                    )}
                >
                    <p className="min-w-0 flex-1">
                        <span className="font-medium">{STAGE_STYLES[lead.stage].label}:</span>{" "}
                        {lead.close_reason ? (
                            <>
                                {reasonLabel(meta, lead.stage, lead.close_reason)}
                                {lead.close_note && <span className="opacity-80"> · {lead.close_note}</span>}
                            </>
                        ) : (
                            <span className="opacity-80">no reason recorded.</span>
                        )}
                    </p>
                    <button
                        type="button"
                        onClick={() => handleStageChange(lead.stage, true)}
                        disabled={stageBusy}
                        className="text-xs font-medium underline-offset-2 hover:underline disabled:opacity-50"
                    >
                        {lead.close_reason ? "Change reason" : "Add a reason"}
                    </button>
                </div>
            )}

            <div className="grid gap-6 lg:grid-cols-3">
                {/* --- Left column: details --- */}
                <div className="space-y-6 lg:col-span-1">
                    <Card>
                        <CardHeader title="Contact" />
                        <dl className="divide-y divide-slate-100 dark:divide-slate-800">
                            <DetailRow icon={Mail} label="Email">
                                <a
                                    href={`mailto:${lead.email}`}
                                    className="break-all text-brand-600 hover:underline dark:text-brand-400"
                                >
                                    {lead.email}
                                </a>
                            </DetailRow>

                            <DetailRow icon={Phone} label="Phone">
                                {lead.phone ? (
                                    <a
                                        href={`tel:${lead.phone}`}
                                        className="text-brand-600 hover:underline dark:text-brand-400"
                                    >
                                        {lead.phone}
                                    </a>
                                ) : (
                                    <span className="text-slate-400 dark:text-slate-500">
                                        Not provided
                                    </span>
                                )}
                            </DetailRow>

                            <DetailRow icon={IndianRupee} label="Deal value">
                                <DealValue
                                    lead={lead}
                                    probability={
                                        meta?.stages.find((s) => s.value === lead.stage)
                                            ?.probability
                                    }
                                />
                            </DetailRow>

                            <DetailRow icon={UserRound} label="Assigned to">
                                {canAssign ? (
                                    <OwnerPicker
                                        ownerId={lead.owner_id}
                                        team={team.data}
                                        teamError={team.error?.message}
                                        onChange={handleOwnerChange}
                                    />
                                ) : (
                                    <span className="text-sm text-slate-800 dark:text-slate-200">
                                        {memberName(team.data, lead.owner_id) ?? "Nobody"}
                                    </span>
                                )}
                            </DetailRow>

                            <DetailRow icon={Calendar} label="Added">
                                <span title={formatDateTime(lead.created_at)}>
                                    {formatDateTime(lead.created_at)}
                                </span>
                            </DetailRow>

                            <DetailRow icon={Clock} label="Last updated">
                                <span title={formatDateTime(lead.updated_at)}>
                                    {formatRelative(lead.updated_at)}
                                </span>
                            </DetailRow>
                        </dl>
                    </Card>

                    <CustomerLinksCard lead={lead} />

                    <ScoreCard lead={lead} />

                    <Card>
                        <CardHeader
                            title="Lifecycle"
                            subtitle="Click a step to move this lead."
                        />
                        <div className="px-5 py-5">
                            <StageProgress
                                stage={lead.stage}
                                closedFrom={closedFrom}
                                disabled={stageBusy}
                                onSelect={(stage) => handleStageChange(stage)}
                            />
                        </div>
                    </Card>

                    <StageHistoryCard history={lead.stage_history ?? []} />

                    <LeadQualityCard key={`q-${lead.id}`} lead={lead} onChange={setData} />

                    <LeadMetaCard lead={lead} onChange={setData} />

                    <NotesCard notes={lead.notes} onSave={handleNotesSave} />
                </div>

                {/* --- Right column: next steps and timeline --- */}
                <div className="space-y-6 lg:col-span-2">
                    <NextStepsCard
                        leadId={lead.id}
                        version={version}
                        onSchedule={(suggestion) => setDraft({ ...suggestion, id: Date.now() })}
                        onOpenEvent={(taskId) => setOpenRequest({ taskId, id: Date.now() })}
                    />

                    <TaskList
                        leadId={lead.id}
                        leadName={lead.name}
                        ownerId={lead.owner_id}
                        draft={draft}
                        openRequest={openRequest}
                        onChanged={refreshLead}
                    />

                    <Card>
                        <CardHeader
                            title="Activity"
                            subtitle={`${lead.activities?.length ?? 0} ${
                                (lead.activities?.length ?? 0) === 1 ? "entry" : "entries"
                            } · newest first`}
                        />

                        <div className="border-b border-slate-200 bg-slate-50/50 dark:border-slate-800 dark:bg-slate-950/30">
                            <ActivityComposer
                                onSubmit={handleLogActivity}
                                types={ACTIVITY_OPTIONS}
                            />
                        </div>

                        <ActivityTimeline
                            activities={lead.activities ?? []}
                            onEdit={handleEditActivity}
                            onDelete={handleDeleteActivity}
                        />
                    </Card>
                </div>
            </div>

            <LeadFormModal
                open={editOpen}
                lead={lead}
                onClose={() => setEditOpen(false)}
                onSubmit={handleEdit}
            />

            <SendEmailModal
                open={emailOpen}
                lead={lead}
                onClose={() => setEmailOpen(false)}
                onDone={handleEmailDone}
            />

            {closeReason.dialog}

            <ConfirmDialog
                open={confirmDelete}
                onClose={() => setConfirmDelete(false)}
                onConfirm={handleDelete}
                title="Delete lead"
                message={`Delete ${lead.name} from ${lead.company}? Their entire activity history will be removed too. This cannot be undone.`}
            />
        </>
    );
}

// ---------------------------------------------------------------------------
// Pieces
// ---------------------------------------------------------------------------

function BackLink() {
    return (
        <Link
            to="/leads"
            className="mb-4 inline-flex items-center gap-1.5 text-sm font-medium text-slate-500 transition-colors hover:text-slate-900 dark:text-slate-400 dark:hover:text-slate-100"
        >
            <ArrowLeft className="size-4" aria-hidden="true" />
            All leads
        </Link>
    );
}

/**
 * One label/value pair inside a <dl>.
 *
 * A description list is the semantically correct element for this -- <dt> is the
 * term, <dd> the value -- which is why it is not a grid of divs.
 */
function DetailRow({
    icon: Icon,
    label,
    children,
}: {
    icon: LucideIcon;
    label: string;
    children: React.ReactNode;
}) {
    return (
        <div className="flex items-start gap-3 px-5 py-3">
            <Icon
                className="mt-0.5 size-4 shrink-0 text-slate-400 dark:text-slate-500"
                aria-hidden="true"
            />
            <div className="min-w-0 flex-1">
                <dt className="text-xs font-medium text-slate-500 dark:text-slate-400">
                    {label}
                </dt>
                <dd className="mt-0.5 text-sm text-slate-900 dark:text-slate-100">
                    {children}
                </dd>
            </div>
        </div>
    );
}

/**
 * The deal value, plus what it is worth to the forecast at the current stage.
 *
 * Showing the weighted figure here, next to the raw one, is what makes the
 * dashboard forecast explainable: every lead shows its own contribution.
 */
function DealValue({ lead, probability }: { lead: Lead; probability?: number }) {
    if (lead.deal_value === null) {
        return <span className="text-slate-400 dark:text-slate-500">Not estimated</span>;
    }

    return (
        <>
            <span className="tabular font-medium">{formatMoney(lead.deal_value)}</span>
            {probability !== undefined && !isClosedStage(lead.stage) && (
                <span className="mt-0.5 block text-xs text-slate-500 dark:text-slate-400">
                    {probability}% at {STAGE_STYLES[lead.stage].label} ={" "}
                    <span className="tabular">
                        {formatMoney(Math.floor((lead.deal_value * probability) / 100))}
                    </span>{" "}
                    in the forecast
                </span>
            )}
        </>
    );
}

/**
 * Who owns the lead, changeable in place. Saves as soon as a person is picked;
 * the server logs the handover on the timeline.
 */
function OwnerPicker({
    ownerId,
    team,
    teamError,
    onChange,
}: {
    ownerId: number | null;
    team: TeamMember[] | null;
    teamError?: string;
    /** Rejects with a message to show under the picker. */
    onChange: (ownerId: number | null) => Promise<void>;
}) {
    const [saving, setSaving] = useState(false);
    const [error, setError] = useState<string | null>(null);

    if (teamError && !team) {
        return <span className="text-xs text-rose-600 dark:text-rose-400">Could not load the team: {teamError}</span>;
    }
    if (!team) return <Skeleton className="h-8 w-40" />;

    const change = async (value: string) => {
        setSaving(true);
        setError(null);
        try {
            await onChange(value ? Number(value) : null);
        } catch (cause) {
            setError(cause instanceof Error ? cause.message : "Could not change the owner.");
        } finally {
            setSaving(false);
        }
    };

    return (
        <div className="space-y-1">
            <Select
                id="lead-owner-inline"
                aria-label="Lead owner"
                value={ownerId ?? ""}
                disabled={saving}
                invalid={Boolean(error)}
                onChange={(event) => void change(event.target.value)}
                className="h-8 text-sm"
            >
                <option value="">No owner</option>
                {team.map((member) => (
                    <option key={member.id} value={member.id}>
                        {member.name}
                    </option>
                ))}
            </Select>
            {error ? (
                <p className="text-xs text-rose-600 dark:text-rose-400">{error}</p>
            ) : ownerId === null ? (
                <p className="text-xs text-slate-400 dark:text-slate-500">
                    {team.length === 0 ? (
                        <>
                            Add people on the{" "}
                            <Link to="/team" className="text-brand-600 hover:underline dark:text-brand-400">
                                Team page
                            </Link>{" "}
                            to assign this lead.
                        </>
                    ) : (
                        "Unowned: reminders won't be emailed."
                    )}
                </p>
            ) : null}
        </div>
    );
}

/** The lead's free-text notes, editable in place without opening the full form. */
function NotesCard({ notes, onSave }: { notes: string; onSave: (notes: string) => Promise<void> }) {
    const [editing, setEditing] = useState(false);
    const [draft, setDraft] = useState(notes);
    const [saving, setSaving] = useState(false);
    const [error, setError] = useState<string | null>(null);

    const start = () => {
        setDraft(notes);
        setError(null);
        setEditing(true);
    };

    const save = async () => {
        if (saving) return;
        if (draft.trim() === notes) {
            setEditing(false);
            return;
        }
        setSaving(true);
        setError(null);
        try {
            await onSave(draft.trim());
            setEditing(false);
        } catch (cause) {
            setError(cause instanceof Error ? cause.message : "Could not save the notes.");
        } finally {
            setSaving(false);
        }
    };

    return (
        <Card>
            <CardHeader
                title="Notes"
                action={
                    !editing && (
                        <Button size="sm" variant="ghost" onClick={start} aria-label={notes ? "Edit notes" : "Add notes"}>
                            <Pencil className="size-3.5" aria-hidden="true" />
                            {notes ? "Edit" : "Add"}
                        </Button>
                    )
                }
            />
            <div className="px-5 py-4">
                {editing ? (
                    <div className="space-y-2">
                        <Textarea
                            id="lead-notes-inline"
                            aria-label="Notes"
                            rows={6}
                            value={draft}
                            maxLength={5000}
                            autoFocus
                            invalid={Boolean(error)}
                            placeholder="Anything worth remembering: budget, decision makers, preferences…"
                            onChange={(event) => setDraft(event.target.value)}
                            onKeyDown={(event) => {
                                if (event.key === "Escape") {
                                    event.preventDefault();
                                    setEditing(false);
                                } else if (event.key === "Enter" && (event.ctrlKey || event.metaKey)) {
                                    event.preventDefault();
                                    void save();
                                }
                            }}
                        />
                        {error && <p className="text-xs text-rose-600 dark:text-rose-400">{error}</p>}
                        <div className="flex items-center justify-end gap-2">
                            <span className="mr-auto text-xs text-slate-400 dark:text-slate-500">{draft.length} / 5000</span>
                            <Button size="sm" variant="ghost" onClick={() => setEditing(false)} disabled={saving}>
                                Cancel
                            </Button>
                            <Button size="sm" variant="primary" loading={saving} onClick={() => void save()}>
                                Save
                            </Button>
                        </div>
                    </div>
                ) : notes ? (
                    // whitespace-pre-wrap preserves the line breaks someone
                    // typed, which plain HTML would collapse into one paragraph.
                    <p className="text-sm break-words whitespace-pre-wrap text-slate-600 dark:text-slate-300">{notes}</p>
                ) : (
                    <button
                        type="button"
                        onClick={start}
                        className="flex items-center gap-2 text-sm text-slate-400 transition-colors hover:text-slate-600 dark:text-slate-500 dark:hover:text-slate-300"
                    >
                        <StickyNote className="size-4" aria-hidden="true" />
                        No notes yet. Add some.
                    </button>
                )}
            </div>
        </Card>
    );
}

function DetailSkeleton() {
    return (
        <>
            <Skeleton className="mb-4 h-4 w-24" />

            <Card className="mb-6 p-5">
                <div className="flex items-start gap-4">
                    <Skeleton className="size-14 shrink-0 rounded-2xl" />
                    <div className="flex-1 space-y-2">
                        <Skeleton className="h-6 w-48" />
                        <Skeleton className="h-4 w-32" />
                        <Skeleton className="h-6 w-28 rounded-full" />
                    </div>
                </div>
            </Card>

            <div className="grid gap-6 lg:grid-cols-3">
                <div className="space-y-6 lg:col-span-1">
                    <Card className="p-5">
                        <Skeleton className="h-4 w-20" />
                        <div className="mt-4 space-y-3">
                            {Array.from({ length: 4 }).map((_, index) => (
                                <Skeleton key={index} className="h-8 w-full" />
                            ))}
                        </div>
                    </Card>
                </div>

                <div className="lg:col-span-2">
                    <Card className="p-5">
                        <Skeleton className="h-4 w-24" />
                        <div className="mt-4 space-y-4">
                            {Array.from({ length: 5 }).map((_, index) => (
                                <div key={index} className="flex gap-3">
                                    <Skeleton className="size-8 shrink-0 rounded-full" />
                                    <div className="flex-1 space-y-1.5">
                                        <Skeleton className="h-3 w-28" />
                                        <Skeleton className="h-3.5 w-full max-w-md" />
                                    </div>
                                </div>
                            ))}
                        </div>
                    </Card>
                </div>
            </div>
        </>
    );
}
