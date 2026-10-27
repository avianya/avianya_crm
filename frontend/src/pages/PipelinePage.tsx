import { useState } from "react";
import { Link } from "react-router-dom";
import { GripVertical, SquareKanban } from "lucide-react";

import { ApiError, api } from "../lib/api";
import {
    STAGE_STYLES,
    cx,
    formatMoney,
    formatMoneyCompact,
    isClosedStage,
} from "../lib/display";
import type { Lead, PipelineColumn, Stage } from "../lib/types";
import { useAsync } from "../hooks/useAsync";
import { useCloseReasonPrompt } from "../hooks/useCloseReasonPrompt";
import { useToast } from "../context/toast";
import { HealthBadge, ScorePill, StuckBadge } from "../components/LeadInsights";
import { StagePicker } from "../components/StagePicker";
import { Card, EmptyState, ErrorState, PageHeader, Skeleton } from "../components/ui";

/**
 * The pipeline as a board: one column per stage, drag a card to move it.
 *
 * Each column header carries the money as well as the count -- total value, the
 * stage's win probability, and the weighted amount it adds to the forecast --
 * so the board answers "where is the money stuck?", not just "where are the
 * leads?". Cards show health, so a cold deal stands out in a column of active
 * ones, and open columns are ordered best score first.
 *
 * Dragging is native HTML drag and drop: no library, and nothing to load. It
 * does not work with a keyboard or on most touch screens, so every card also
 * carries the same stage dropdown the leads table uses.
 */
export function PipelinePage() {
    const toast = useToast();
    const closeReason = useCloseReasonPrompt();

    const { data: columns, error, loading, refetch, setData } = useAsync(
        (signal) => api.pipeline(signal),
        [],
    );

    const [showClosed, setShowClosed] = useState(true);
    const [draggingId, setDraggingId] = useState<number | null>(null);
    const [dropTarget, setDropTarget] = useState<Stage | null>(null);
    const [busyId, setBusyId] = useState<number | null>(null);

    /**
     * Moves a lead to a new stage.
     *
     * The card jumps to its new column immediately, then the board is re-read:
     * the move changes the lead's score, both columns' totals, and the order
     * cards sit in, and the server is the one place all of that is worked out.
     */
    const moveLead = async (lead: Lead, to: Stage) => {
        if (lead.stage === to) return;

        // Dropping on Won or Lost asks why before the card moves; cancelling
        // leaves it where it was.
        const close = await closeReason.ask(lead, to);
        if (close === null) return;

        setData((current) => (current ? moveCard(current, lead, to) : current!));
        setBusyId(lead.id);

        try {
            await api.updateLeadStage(lead.id, to, close);
            toast.success(`${lead.name} moved to ${STAGE_STYLES[to].label}.`);
        } catch (cause) {
            toast.error(cause instanceof ApiError ? cause.message : "Could not move the lead.");
        } finally {
            setBusyId(null);
            // On success this picks up the new totals and order; on failure it
            // puts the card back where the server says it is.
            refetch();
        }
    };

    const findLead = (id: number): Lead | undefined =>
        columns?.flatMap((column) => column.leads).find((lead) => lead.id === id);

    const handleDrop = (stage: Stage) => {
        const lead = draggingId !== null ? findLead(draggingId) : undefined;
        setDraggingId(null);
        setDropTarget(null);
        if (lead) void moveLead(lead, stage);
    };

    if (loading) return <BoardSkeleton />;

    if (error || !columns) {
        return (
            <>
                <PageHeader title="Pipeline" />
                <ErrorState message={error?.message ?? "Could not load the pipeline."} onRetry={refetch} />
            </>
        );
    }

    const open = columns.filter((column) => !isClosedStage(column.stage));
    const openValue = open.reduce((sum, column) => sum + column.value, 0);
    const forecast = open.reduce((sum, column) => sum + column.weighted, 0);
    const total = columns.reduce((sum, column) => sum + column.count, 0);
    const visible = showClosed ? columns : open;

    return (
        <>
            <PageHeader
                title="Pipeline"
                subtitle={
                    total === 0
                        ? "Every lead, by stage."
                        : `${formatMoney(openValue)} open · ${formatMoney(forecast)} weighted forecast`
                }
                action={
                    <label className="inline-flex cursor-pointer items-center gap-2 text-sm text-slate-600 dark:text-slate-300">
                        <input
                            type="checkbox"
                            checked={showClosed}
                            onChange={(event) => setShowClosed(event.target.checked)}
                            className="size-4 rounded border-slate-300 accent-brand-600"
                        />
                        Show Won / Lost
                    </label>
                }
            />

            {total === 0 ? (
                <Card>
                    <EmptyState
                        icon={SquareKanban}
                        title="The board is empty"
                        description="Add a lead and it will appear in its stage's column."
                    />
                </Card>
            ) : (
                // The board scrolls sideways inside its own box, so the page
                // itself never does -- which matters on a phone.
                <div className="-mx-1 overflow-x-auto pb-4">
                    <div className="flex min-w-max gap-4 px-1">
                        {visible.map((column) => (
                            <Column
                                key={column.stage}
                                column={column}
                                isDropTarget={dropTarget === column.stage}
                                dragging={draggingId !== null}
                                busyId={busyId}
                                draggingId={draggingId}
                                onDragEnter={() => setDropTarget(column.stage)}
                                onDrop={() => handleDrop(column.stage)}
                                onDragStart={setDraggingId}
                                onDragEnd={() => {
                                    setDraggingId(null);
                                    setDropTarget(null);
                                }}
                                onMove={moveLead}
                            />
                        ))}
                    </div>
                </div>
            )}

            {closeReason.dialog}
        </>
    );
}

// ---------------------------------------------------------------------------
// Column
// ---------------------------------------------------------------------------

function Column({
    column,
    isDropTarget,
    dragging,
    draggingId,
    busyId,
    onDragEnter,
    onDrop,
    onDragStart,
    onDragEnd,
    onMove,
}: {
    column: PipelineColumn;
    isDropTarget: boolean;
    dragging: boolean;
    draggingId: number | null;
    busyId: number | null;
    onDragEnter: () => void;
    onDrop: () => void;
    onDragStart: (id: number) => void;
    onDragEnd: () => void;
    onMove: (lead: Lead, to: Stage) => void;
}) {
    const style = STAGE_STYLES[column.stage];
    const closed = isClosedStage(column.stage);
    const hidden = column.count - column.leads.length;

    return (
        <section
            aria-label={`${column.label}: ${column.count} leads`}
            // preventDefault on dragover is what marks this as a valid drop
            // target; without it the browser refuses the drop.
            onDragOver={(event) => {
                event.preventDefault();
                event.dataTransfer.dropEffect = "move";
            }}
            onDragEnter={onDragEnter}
            onDrop={(event) => {
                event.preventDefault();
                onDrop();
            }}
            className={cx(
                "flex w-72 shrink-0 flex-col rounded-2xl border bg-slate-50/70 transition-colors dark:bg-slate-900/50",
                isDropTarget
                    ? "border-brand-400 bg-brand-50/60 dark:border-brand-600 dark:bg-brand-950/30"
                    : "border-slate-200 dark:border-slate-800",
            )}
        >
            <header className="border-b border-slate-200 px-4 py-3 dark:border-slate-800">
                <div className="flex items-center justify-between gap-2">
                    <h2 className="flex items-center gap-2 text-sm font-semibold text-slate-900 dark:text-slate-100">
                        <span className={cx("size-2 rounded-full", style.solid)} aria-hidden="true" />
                        {column.label}
                    </h2>
                    <span className="tabular rounded-full bg-white px-2 py-0.5 text-xs font-medium text-slate-600 ring-1 ring-slate-200 dark:bg-slate-800 dark:text-slate-300 dark:ring-slate-700">
                        {column.count}
                    </span>
                </div>

                {/* The money in this column, with the working shown. */}
                <p
                    className="tabular mt-1.5 text-xs text-slate-500 dark:text-slate-400"
                    title={
                        closed
                            ? undefined
                            : `${formatMoney(column.value)} × ${column.probability}% = ${formatMoney(column.weighted)}`
                    }
                >
                    <span className="font-medium text-slate-700 dark:text-slate-200">
                        {formatMoneyCompact(column.value)}
                    </span>
                    {!closed && (
                        <>
                            {" "}
                            × {column.probability}% ={" "}
                            <span className="font-medium text-brand-600 dark:text-brand-400">
                                {formatMoneyCompact(column.weighted)}
                            </span>
                        </>
                    )}
                </p>
            </header>

            <ul className="flex min-h-24 flex-1 flex-col gap-2 p-2">
                {column.leads.map((lead) => (
                    <LeadCard
                        key={lead.id}
                        lead={lead}
                        busy={busyId === lead.id}
                        isDragging={draggingId === lead.id}
                        onDragStart={() => onDragStart(lead.id)}
                        onDragEnd={onDragEnd}
                        onMove={(to) => onMove(lead, to)}
                    />
                ))}

                {column.leads.length === 0 && (
                    <li
                        className={cx(
                            "flex flex-1 items-center justify-center rounded-xl border border-dashed px-3 py-6 text-center text-xs",
                            dragging
                                ? "border-brand-300 text-brand-600 dark:border-brand-700 dark:text-brand-400"
                                : "border-slate-200 text-slate-400 dark:border-slate-800 dark:text-slate-500",
                        )}
                    >
                        {dragging ? "Drop here" : "No leads"}
                    </li>
                )}

                {hidden > 0 && (
                    <li className="px-2 py-1 text-center text-xs">
                        <Link
                            to={`/leads?stage=${column.stage}`}
                            className="text-brand-600 hover:underline dark:text-brand-400"
                        >
                            +{hidden} more in the leads list
                        </Link>
                    </li>
                )}
            </ul>
        </section>
    );
}

// ---------------------------------------------------------------------------
// Card
// ---------------------------------------------------------------------------

function LeadCard({
    lead,
    busy,
    isDragging,
    onDragStart,
    onDragEnd,
    onMove,
}: {
    lead: Lead;
    busy: boolean;
    isDragging: boolean;
    onDragStart: () => void;
    onDragEnd: () => void;
    onMove: (to: Stage) => void;
}) {
    return (
        <li
            draggable={!busy}
            onDragStart={(event) => {
                // Some browsers will not start a drag without data set.
                event.dataTransfer.setData("text/plain", String(lead.id));
                event.dataTransfer.effectAllowed = "move";
                onDragStart();
            }}
            onDragEnd={onDragEnd}
            className={cx(
                "group cursor-grab rounded-xl border bg-white p-3 shadow-sm transition-all active:cursor-grabbing dark:bg-slate-900",
                // A cold deal gets a coloured edge, so it stands out in a column
                // of healthy ones without having to read every badge.
                lead.health === "cold"
                    ? "border-sky-300 border-l-4 dark:border-sky-700"
                    : lead.health === "cooling"
                      ? "border-amber-300 border-l-4 dark:border-amber-700"
                      : "border-slate-200 dark:border-slate-800",
                isDragging && "opacity-40",
                busy && "opacity-60",
            )}
        >
            <div className="flex items-start gap-2">
                <GripVertical
                    className="mt-0.5 size-4 shrink-0 text-slate-300 group-hover:text-slate-400 dark:text-slate-600"
                    aria-hidden="true"
                />
                <div className="min-w-0 flex-1">
                    {/* draggable={false}: a link is draggable on its own, which
                        would drag the URL instead of the card. */}
                    <Link
                        to={`/leads/${lead.id}`}
                        draggable={false}
                        className="block truncate text-sm font-medium text-slate-900 hover:text-brand-700 hover:underline dark:text-slate-100 dark:hover:text-brand-400"
                    >
                        {lead.name}
                    </Link>
                    <p className="truncate text-xs text-slate-500 dark:text-slate-400">{lead.company}</p>
                </div>
                <ScorePill score={lead.score} />
            </div>

            <div className="mt-2.5 flex items-center justify-between gap-2 pl-6">
                <span className="tabular text-xs font-medium text-slate-700 dark:text-slate-300">
                    {lead.deal_value === null ? (
                        <span className="font-normal text-slate-400 dark:text-slate-500">No value</span>
                    ) : (
                        formatMoney(lead.deal_value)
                    )}
                </span>
                <HealthBadge lead={lead} showDays />
            </div>
            {lead.stuck && (
                <div className="mt-1.5 pl-6">
                    <StuckBadge lead={lead} />
                </div>
            )}

            <div className="mt-2.5 pl-6">
                <StagePicker stage={lead.stage} size="sm" busy={busy} onChange={onMove} />
            </div>
        </li>
    );
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

/**
 * Moves a card between columns locally, for the instant feedback before the
 * server answers. Totals are adjusted by the card's own value; the refetch that
 * follows replaces all of this with the server's exact figures.
 */
function moveCard(columns: PipelineColumn[], lead: Lead, to: Stage): PipelineColumn[] {
    const value = lead.deal_value ?? 0;

    return columns.map((column) => {
        if (column.stage === lead.stage) {
            const nextValue = column.value - value;
            return {
                ...column,
                count: column.count - 1,
                value: nextValue,
                weighted: Math.floor((nextValue * column.probability) / 100),
                leads: column.leads.filter((card) => card.id !== lead.id),
            };
        }
        if (column.stage === to) {
            const nextValue = column.value + value;
            return {
                ...column,
                count: column.count + 1,
                value: nextValue,
                weighted: Math.floor((nextValue * column.probability) / 100),
                leads: [{ ...lead, stage: to }, ...column.leads],
            };
        }
        return column;
    });
}

function BoardSkeleton() {
    return (
        <>
            <PageHeader title="Pipeline" subtitle="Every lead, by stage." />
            <div className="flex gap-4 overflow-hidden">
                {Array.from({ length: 5 }).map((_, index) => (
                    <div
                        key={index}
                        className="w-72 shrink-0 space-y-2 rounded-2xl border border-slate-200 p-3 dark:border-slate-800"
                    >
                        <Skeleton className="h-4 w-24" />
                        <Skeleton className="h-3 w-32" />
                        <Skeleton className="mt-3 h-24 w-full rounded-xl" />
                        <Skeleton className="h-24 w-full rounded-xl" />
                    </div>
                ))}
            </div>
        </>
    );
}
