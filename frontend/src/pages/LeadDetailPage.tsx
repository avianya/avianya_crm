import { useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import {
    ArrowLeft,
    ArrowRight,
    Building2,
    Calendar,
    Clock,
    Mail,
    Pencil,
    Phone,
    StickyNote,
    Trash2,
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
    formatRelative,
    initials,
    nextStage,
} from "../lib/display";
import type { ActivityInput, LeadInput, Stage, UserActivityType } from "../lib/types";
import { useAsync } from "../hooks/useAsync";
import { useToast } from "../context/toast";
import { ActivityComposer, ActivityTimeline } from "../components/ActivityTimeline";
import { ConfirmDialog } from "../components/ConfirmDialog";
import { LeadFormModal } from "../components/LeadFormModal";
import { StageProgress } from "../components/StageProgress";
import { StagePicker } from "../components/StagePicker";
import {
    Button,
    Card,
    CardHeader,
    EmptyState,
    ErrorState,
    Skeleton,
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
 */
export function LeadDetailPage() {
    // Route params always arrive as strings; the API needs a number.
    const { id } = useParams<{ id: string }>();
    const leadId = Number(id);

    const navigate = useNavigate();
    const toast = useToast();

    const [editOpen, setEditOpen] = useState(false);
    const [confirmDelete, setConfirmDelete] = useState(false);
    const [stageBusy, setStageBusy] = useState(false);

    const { data: lead, error, loading, refetch, setData } = useAsync(
        (signal) => api.getLead(leadId, signal),
        [leadId],
    );

    // --- mutations ----------------------------------------------------------

    const handleStageChange = async (next: Stage) => {
        if (!lead || next === lead.stage) return;

        setStageBusy(true);
        try {
            const updated = await api.updateLeadStage(lead.id, next);

            // The stage response does not include the timeline, but the server has
            // just appended a stage_change entry to it -- so the fresh activities
            // are fetched and merged, keeping the timeline honest without
            // re-reading the whole lead.
            const activities = await api.listActivities(lead.id);
            setData({ ...updated, activities });

            toast.success(`Moved to ${STAGE_STYLES[next].label}.`);
        } catch (cause) {
            toast.error(
                cause instanceof ApiError ? cause.message : "Could not change the stage.",
            );
        } finally {
            setStageBusy(false);
        }
    };

    const handleEdit = async (input: LeadInput) => {
        if (!lead) return;
        const updated = await api.updateLead(lead.id, input);
        // PUT returns the lead with its timeline, so this is a complete refresh.
        setData(updated);
        toast.success("Lead updated.");
    };

    const handleLogActivity = async (input: ActivityInput) => {
        if (!lead) return;

        const activity = await api.createActivity(lead.id, input);

        // Prepend rather than refetch: the timeline is newest-first, and the
        // server returned the row it just created.
        setData((current) =>
            current
                ? { ...current, activities: [activity, ...(current.activities ?? [])] }
                : current!,
        );
        toast.success("Activity logged.");
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

    if (error) {
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

    return (
        <>
            <BackLink />

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
                                    onChange={handleStageChange}
                                />
                                <span className="inline-flex items-center gap-1.5 rounded-full bg-slate-100 px-2.5 py-1 text-xs font-medium text-slate-600 dark:bg-slate-800 dark:text-slate-300">
                                    <sourceStyle.icon className="size-3" aria-hidden="true" />
                                    {sourceStyle.label}
                                </span>
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
                        <Button variant="secondary" onClick={() => setEditOpen(true)}>
                            <Pencil className="size-4" aria-hidden="true" />
                            Edit
                        </Button>
                        <Button variant="ghost" onClick={() => setConfirmDelete(true)}>
                            <Trash2 className="size-4" aria-hidden="true" />
                            <span className="sr-only sm:not-sr-only">Delete</span>
                        </Button>
                    </div>
                </div>
            </Card>

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

                    <Card>
                        <CardHeader
                            title="Lifecycle"
                            subtitle="Click a step to move this lead."
                        />
                        <div className="px-5 py-5">
                            <StageProgress
                                stage={lead.stage}
                                disabled={stageBusy}
                                onSelect={handleStageChange}
                            />
                        </div>
                    </Card>

                    <Card>
                        <CardHeader title="Notes" />
                        <div className="px-5 py-4">
                            {lead.notes ? (
                                // whitespace-pre-wrap preserves the line breaks
                                // someone typed in the textarea, which plain HTML
                                // would otherwise collapse into one paragraph.
                                <p className="text-sm whitespace-pre-wrap text-slate-600 dark:text-slate-300">
                                    {lead.notes}
                                </p>
                            ) : (
                                <div className="flex items-center gap-2 text-sm text-slate-400 dark:text-slate-500">
                                    <StickyNote className="size-4" aria-hidden="true" />
                                    No notes yet.
                                </div>
                            )}
                        </div>
                    </Card>
                </div>

                {/* --- Right column: timeline --- */}
                <div className="lg:col-span-2">
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

                        <ActivityTimeline activities={lead.activities ?? []} />
                    </Card>
                </div>
            </div>

            <LeadFormModal
                open={editOpen}
                lead={lead}
                onClose={() => setEditOpen(false)}
                onSubmit={handleEdit}
            />

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
