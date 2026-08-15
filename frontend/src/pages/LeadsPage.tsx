import { useCallback, useEffect, useState } from "react";
import { Link, useSearchParams } from "react-router-dom";
import {
    ArrowDown,
    ArrowUp,
    ChevronRight,
    Mail,
    Pencil,
    Phone,
    Plus,
    Search,
    SlidersHorizontal,
    Trash2,
    UserPlus,
    UserX,
    X,
} from "lucide-react";

import { ApiError, api } from "../lib/api";
import {
    SOURCE_STYLES,
    STAGE_ORDER,
    STAGE_STYLES,
    avatarColor,
    cx,
    formatDate,
    formatRelative,
    initials,
} from "../lib/display";
import type { Lead, LeadInput, LeadListParams, Source, Stage } from "../lib/types";
import { useAsync } from "../hooks/useAsync";
import { useDebouncedValue } from "../hooks/useDebouncedValue";
import { useToast } from "../context/toast";
import { ConfirmDialog } from "../components/ConfirmDialog";
import { LeadFormModal } from "../components/LeadFormModal";
import { Pagination } from "../components/Pagination";
import { StagePicker } from "../components/StagePicker";
import {
    Button,
    Card,
    EmptyState,
    ErrorState,
    PageHeader,
    Select,
    Skeleton,
    Spinner,
    TextInput,
} from "../components/ui";

type SortField = NonNullable<LeadListParams["sort_by"]>;

/**
 * The leads list: search, filter, sort, paginate, change stage inline.
 *
 * Filter state lives in the URL query string rather than in component state. That
 * is a deliberate choice with three payoffs: the back button works, a filtered
 * view can be shared or bookmarked, and the dashboard's chart can link straight
 * to "/leads?stage=negotiation" without this page needing to know about it.
 */
export function LeadsPage() {
    const [searchParams, setSearchParams] = useSearchParams();
    const toast = useToast();

    // --- filter state, read from the URL -----------------------------------
    const searchParam = searchParams.get("search") ?? "";
    const stage = (searchParams.get("stage") ?? "all") as Stage | "all";
    const source = (searchParams.get("source") ?? "all") as Source | "all";
    const sortBy = (searchParams.get("sort_by") ?? "created_at") as SortField;
    const sortDir = (searchParams.get("sort_dir") ?? "desc") as "asc" | "desc";
    const page = Math.max(1, Number(searchParams.get("page") ?? "1") || 1);

    // The input is local so typing is instant; the URL (and the request) only
    // catch up once typing pauses.
    const [searchInput, setSearchInput] = useState(searchParam);
    const debouncedSearch = useDebouncedValue(searchInput, 300);

    const [showFilters, setShowFilters] = useState(false);

    // --- dialogs and per-row busy state ------------------------------------
    const [formOpen, setFormOpen] = useState(false);
    const [editing, setEditing] = useState<Lead | undefined>();
    const [deleting, setDeleting] = useState<Lead | undefined>();
    // Which row is mid-stage-change, so only that row shows a spinner.
    const [stageBusyId, setStageBusyId] = useState<number | null>(null);

    /**
     * Writes filter changes back to the URL.
     *
     * Any filter change also resets to page 1: staying on page 4 of a result set
     * that now has one page would show an empty table.
     */
    const updateParams = useCallback(
        (updates: Record<string, string | null>, resetPage = true) => {
            setSearchParams(
                (current) => {
                    const next = new URLSearchParams(current);
                    for (const [key, value] of Object.entries(updates)) {
                        // Empty and "all" mean "no filter", and are dropped so the
                        // URL stays clean rather than accumulating ?stage=all.
                        if (value === null || value === "" || value === "all") {
                            next.delete(key);
                        } else {
                            next.set(key, value);
                        }
                    }
                    if (resetPage) next.delete("page");
                    return next;
                },
                // replace: true keeps filter tweaks out of the history stack, so
                // Back leaves the page instead of undoing six keystrokes.
                { replace: true },
            );
        },
        [setSearchParams],
    );

    // Push the debounced search term into the URL once typing settles. The
    // equality guard is what stops this from looping: without it, every URL
    // change would re-run the effect that caused it.
    useEffect(() => {
        if (debouncedSearch === searchParam) return;
        updateParams({ search: debouncedSearch });
    }, [debouncedSearch, searchParam, updateParams]);

    // --- data ---------------------------------------------------------------
    const { data, error, loading, fetching, refetch, setData } = useAsync(
        (signal) =>
            api.listLeads(
                { search: searchParam, stage, source, sort_by: sortBy, sort_dir: sortDir, page },
                signal,
            ),
        [searchParam, stage, source, sortBy, sortDir, page],
    );

    const leads = data?.data ?? [];
    const hasActiveFilters = Boolean(searchParam) || stage !== "all" || source !== "all";

    // --- mutations ----------------------------------------------------------

    const handleStageChange = async (lead: Lead, next: Stage) => {
        setStageBusyId(lead.id);
        try {
            const updated = await api.updateLeadStage(lead.id, next);

            // Patch the one row in place from the server's response instead of
            // refetching the page. The result is the same data with no flicker,
            // no scroll jump, and no second round trip.
            setData((current) =>
                current
                    ? {
                          ...current,
                          data: current.data.map((row) =>
                              row.id === updated.id ? { ...row, ...updated } : row,
                          ),
                      }
                    : current!,
            );

            toast.success(
                `${lead.name} moved to ${STAGE_STYLES[next].label}.`,
            );
        } catch (cause) {
            toast.error(
                cause instanceof ApiError ? cause.message : "Could not change the stage.",
            );
        } finally {
            setStageBusyId(null);
        }
    };

    const handleSubmit = async (input: LeadInput) => {
        if (editing) {
            await api.updateLead(editing.id, input);
            toast.success("Lead updated.");
        } else {
            await api.createLead(input);
            toast.success("Lead created.");
        }
        // A create can change which rows belong on this page, and an edit can
        // change the sort position, so this one does need a refetch.
        refetch();
    };

    const handleDelete = async () => {
        if (!deleting) return;
        try {
            await api.deleteLead(deleting.id);
            toast.success(`${deleting.name} was deleted.`);
            refetch();
        } catch (cause) {
            toast.error(
                cause instanceof ApiError ? cause.message : "Could not delete the lead.",
            );
            throw cause; // keeps the dialog open
        }
    };

    /** Clicking a sortable header sorts by it, or flips the direction if already active. */
    const toggleSort = (field: SortField) => {
        if (sortBy === field) {
            updateParams({ sort_dir: sortDir === "asc" ? "desc" : "asc" }, false);
        } else {
            // Text sorts read better A→Z; dates and stages read better newest or
            // furthest-along first.
            const naturalDirection = field === "name" || field === "company" ? "asc" : "desc";
            updateParams({ sort_by: field, sort_dir: naturalDirection }, false);
        }
    };

    return (
        <>
            <PageHeader
                title="Leads"
                subtitle={
                    data
                        ? `${data.pagination.total_items} ${data.pagination.total_items === 1 ? "lead" : "leads"}${hasActiveFilters ? " matching your filters" : " in the pipeline"}`
                        : "Loading…"
                }
                action={
                    <Button
                        variant="primary"
                        onClick={() => {
                            setEditing(undefined);
                            setFormOpen(true);
                        }}
                    >
                        <Plus className="size-4" aria-hidden="true" />
                        New lead
                    </Button>
                }
            />

            <Card>
                {/* --- Search and filters --- */}
                <div className="space-y-3 border-b border-slate-200 px-4 py-4 sm:px-5 dark:border-slate-800">
                    <div className="flex gap-2">
                        <div className="relative flex-1">
                            <Search
                                className="pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2 text-slate-400"
                                aria-hidden="true"
                            />
                            <TextInput
                                id="lead-search"
                                type="search"
                                value={searchInput}
                                onChange={(event) => setSearchInput(event.target.value)}
                                placeholder="Search by name or company…"
                                aria-label="Search leads by name or company"
                                className="pl-9"
                            />
                            {/* A spinner in the field, not over the table: it says
                                "this search is running" without hiding the results
                                you are still reading. */}
                            {fetching && !loading && (
                                <Spinner className="absolute top-1/2 right-3 size-4 -translate-y-1/2" />
                            )}
                        </div>

                        <Button
                            variant={showFilters || hasActiveFilters ? "primary" : "secondary"}
                            onClick={() => setShowFilters((open) => !open)}
                            aria-expanded={showFilters}
                        >
                            <SlidersHorizontal className="size-4" aria-hidden="true" />
                            <span className="hidden sm:inline">Filters</span>
                        </Button>
                    </div>

                    {/* Stage filter chips. Always visible, because filtering by
                        stage is the single most common thing anyone does here --
                        burying it behind a dropdown would be a step backwards. */}
                    <div className="scrollbar-thin -mx-1 flex gap-1.5 overflow-x-auto px-1 pb-1">
                        <StageChip
                            label="All"
                            active={stage === "all"}
                            onClick={() => updateParams({ stage: null })}
                        />
                        {STAGE_ORDER.map((option) => (
                            <StageChip
                                key={option}
                                label={STAGE_STYLES[option].label}
                                dot={STAGE_STYLES[option].solid}
                                active={stage === option}
                                onClick={() => updateParams({ stage: option })}
                            />
                        ))}
                    </div>

                    {showFilters && (
                        <div className="grid gap-3 pt-1 animate-slide-up sm:grid-cols-3">
                            <div>
                                <label
                                    htmlFor="filter-source"
                                    className="mb-1 block text-xs font-medium text-slate-600 dark:text-slate-400"
                                >
                                    Source
                                </label>
                                <Select
                                    id="filter-source"
                                    value={source}
                                    onChange={(event) =>
                                        updateParams({ source: event.target.value })
                                    }
                                >
                                    <option value="all">All sources</option>
                                    {Object.entries(SOURCE_STYLES).map(([value, style]) => (
                                        <option key={value} value={value}>
                                            {style.label}
                                        </option>
                                    ))}
                                </Select>
                            </div>

                            <div>
                                <label
                                    htmlFor="filter-sort"
                                    className="mb-1 block text-xs font-medium text-slate-600 dark:text-slate-400"
                                >
                                    Sort by
                                </label>
                                <Select
                                    id="filter-sort"
                                    value={sortBy}
                                    onChange={(event) =>
                                        updateParams({ sort_by: event.target.value }, false)
                                    }
                                >
                                    <option value="created_at">Date added</option>
                                    <option value="updated_at">Last updated</option>
                                    <option value="name">Name</option>
                                    <option value="company">Company</option>
                                    <option value="stage">Stage</option>
                                </Select>
                            </div>

                            <div>
                                <label
                                    htmlFor="filter-dir"
                                    className="mb-1 block text-xs font-medium text-slate-600 dark:text-slate-400"
                                >
                                    Direction
                                </label>
                                <Select
                                    id="filter-dir"
                                    value={sortDir}
                                    onChange={(event) =>
                                        updateParams({ sort_dir: event.target.value }, false)
                                    }
                                >
                                    <option value="desc">Descending</option>
                                    <option value="asc">Ascending</option>
                                </Select>
                            </div>
                        </div>
                    )}

                    {hasActiveFilters && (
                        <button
                            type="button"
                            onClick={() => {
                                setSearchInput("");
                                updateParams({ search: null, stage: null, source: null });
                            }}
                            className="inline-flex items-center gap-1 text-xs font-medium text-slate-500 transition-colors hover:text-slate-800 dark:text-slate-400 dark:hover:text-slate-200"
                        >
                            <X className="size-3.5" aria-hidden="true" />
                            Clear filters
                        </button>
                    )}
                </div>

                {/* --- Results --- */}
                {loading ? (
                    <TableSkeleton />
                ) : error ? (
                    <div className="p-5">
                        <ErrorState message={error.message} onRetry={refetch} />
                    </div>
                ) : leads.length === 0 ? (
                    hasActiveFilters ? (
                        <EmptyState
                            icon={UserX}
                            title="No leads match those filters"
                            description="Try a different search term, or clear the filters to see everything."
                            action={
                                <Button
                                    variant="secondary"
                                    onClick={() => {
                                        setSearchInput("");
                                        updateParams({ search: null, stage: null, source: null });
                                    }}
                                >
                                    Clear filters
                                </Button>
                            }
                        />
                    ) : (
                        <EmptyState
                            icon={UserPlus}
                            title="No leads yet"
                            description="Add your first lead to start tracking it through the pipeline."
                            action={
                                <Button
                                    variant="primary"
                                    onClick={() => {
                                        setEditing(undefined);
                                        setFormOpen(true);
                                    }}
                                >
                                    <Plus className="size-4" aria-hidden="true" />
                                    New lead
                                </Button>
                            }
                        />
                    )
                ) : (
                    <>
                        {/*
                          Two presentations of the same data: a table on wide
                          screens, cards below md. A table squeezed onto a phone is
                          either unreadable or scrolls sideways, and eight columns
                          of contact details is exactly the case where cards win.
                        */}
                        <div
                            className={cx(
                                "hidden overflow-x-auto transition-opacity md:block",
                                // Dim, don't hide, while refetching: the old rows
                                // stay readable and the layout does not collapse.
                                fetching && "opacity-60",
                            )}
                        >
                            <table className="w-full text-left text-sm">
                                <thead className="border-b border-slate-200 bg-slate-50/60 text-xs dark:border-slate-800 dark:bg-slate-950/40">
                                    <tr>
                                        <SortableHeader
                                            field="name"
                                            label="Lead"
                                            sortBy={sortBy}
                                            sortDir={sortDir}
                                            onSort={toggleSort}
                                        />
                                        <SortableHeader
                                            field="company"
                                            label="Company"
                                            sortBy={sortBy}
                                            sortDir={sortDir}
                                            onSort={toggleSort}
                                        />
                                        <th scope="col" className="px-4 py-2.5 font-medium text-slate-500 dark:text-slate-400">
                                            Contact
                                        </th>
                                        <SortableHeader
                                            field="stage"
                                            label="Stage"
                                            sortBy={sortBy}
                                            sortDir={sortDir}
                                            onSort={toggleSort}
                                        />
                                        <th scope="col" className="px-4 py-2.5 font-medium text-slate-500 dark:text-slate-400">
                                            Source
                                        </th>
                                        <SortableHeader
                                            field="created_at"
                                            label="Added"
                                            sortBy={sortBy}
                                            sortDir={sortDir}
                                            onSort={toggleSort}
                                        />
                                        <th scope="col" className="px-4 py-2.5">
                                            <span className="sr-only">Actions</span>
                                        </th>
                                    </tr>
                                </thead>

                                <tbody className="divide-y divide-slate-100 dark:divide-slate-800">
                                    {leads.map((lead) => {
                                        const sourceStyle = SOURCE_STYLES[lead.source];
                                        const SourceIcon = sourceStyle.icon;

                                        return (
                                            <tr
                                                key={lead.id}
                                                className="group transition-colors hover:bg-slate-50 dark:hover:bg-slate-800/40"
                                            >
                                                <td className="px-4 py-3">
                                                    <Link
                                                        to={`/leads/${lead.id}`}
                                                        className="flex items-center gap-3"
                                                    >
                                                        <span
                                                            className={cx(
                                                                "flex size-8 shrink-0 items-center justify-center rounded-full text-[11px] font-semibold",
                                                                avatarColor(lead.name),
                                                            )}
                                                            aria-hidden="true"
                                                        >
                                                            {initials(lead.name)}
                                                        </span>
                                                        <span className="font-medium text-slate-900 group-hover:text-brand-700 dark:text-slate-100 dark:group-hover:text-brand-400">
                                                            {lead.name}
                                                        </span>
                                                    </Link>
                                                </td>

                                                <td className="px-4 py-3 text-slate-600 dark:text-slate-300">
                                                    {lead.company}
                                                </td>

                                                <td className="px-4 py-3">
                                                    <div className="space-y-0.5 text-xs">
                                                        {/* mailto/tel links: the
                                                            point of a CRM is to get
                                                            in touch, so make the
                                                            contact details do that. */}
                                                        <a
                                                            href={`mailto:${lead.email}`}
                                                            className="flex items-center gap-1.5 text-slate-600 hover:text-brand-600 hover:underline dark:text-slate-300 dark:hover:text-brand-400"
                                                        >
                                                            <Mail className="size-3 shrink-0 opacity-60" aria-hidden="true" />
                                                            <span className="max-w-[15rem] truncate">{lead.email}</span>
                                                        </a>
                                                        {lead.phone ? (
                                                            <a
                                                                href={`tel:${lead.phone}`}
                                                                className="flex items-center gap-1.5 text-slate-500 hover:text-brand-600 hover:underline dark:text-slate-400 dark:hover:text-brand-400"
                                                            >
                                                                <Phone className="size-3 shrink-0 opacity-60" aria-hidden="true" />
                                                                {lead.phone}
                                                            </a>
                                                        ) : (
                                                            <span className="flex items-center gap-1.5 text-slate-400 dark:text-slate-500">
                                                                <Phone className="size-3 shrink-0 opacity-40" aria-hidden="true" />
                                                                No phone
                                                            </span>
                                                        )}
                                                    </div>
                                                </td>

                                                <td className="px-4 py-3">
                                                    <StagePicker
                                                        stage={lead.stage}
                                                        busy={stageBusyId === lead.id}
                                                        onChange={(next) => handleStageChange(lead, next)}
                                                    />
                                                </td>

                                                <td className="px-4 py-3">
                                                    <span className="inline-flex items-center gap-1.5 text-xs text-slate-600 dark:text-slate-300">
                                                        <SourceIcon className="size-3.5 opacity-60" aria-hidden="true" />
                                                        {sourceStyle.label}
                                                    </span>
                                                </td>

                                                <td className="px-4 py-3">
                                                    <span
                                                        className="text-xs whitespace-nowrap text-slate-500 dark:text-slate-400"
                                                        title={formatDate(lead.created_at)}
                                                    >
                                                        {formatRelative(lead.created_at)}
                                                    </span>
                                                </td>

                                                <td className="px-4 py-3">
                                                    {/* Row actions fade in on hover
                                                        to keep the table calm, but
                                                        focus-within keeps them
                                                        reachable by keyboard. */}
                                                    <div className="flex items-center justify-end gap-0.5 opacity-0 transition-opacity group-hover:opacity-100 focus-within:opacity-100">
                                                        <IconButton
                                                            label={`Edit ${lead.name}`}
                                                            onClick={() => {
                                                                setEditing(lead);
                                                                setFormOpen(true);
                                                            }}
                                                        >
                                                            <Pencil className="size-4" aria-hidden="true" />
                                                        </IconButton>
                                                        <IconButton
                                                            label={`Delete ${lead.name}`}
                                                            danger
                                                            onClick={() => setDeleting(lead)}
                                                        >
                                                            <Trash2 className="size-4" aria-hidden="true" />
                                                        </IconButton>
                                                        <Link
                                                            to={`/leads/${lead.id}`}
                                                            aria-label={`Open ${lead.name}`}
                                                            className="rounded-lg p-1.5 text-slate-400 transition-colors hover:bg-slate-200/70 hover:text-slate-700 dark:hover:bg-slate-700 dark:hover:text-slate-200"
                                                        >
                                                            <ChevronRight className="size-4" aria-hidden="true" />
                                                        </Link>
                                                    </div>
                                                </td>
                                            </tr>
                                        );
                                    })}
                                </tbody>
                            </table>
                        </div>

                        {/* --- Mobile cards --- */}
                        <ul
                            className={cx(
                                "divide-y divide-slate-100 transition-opacity md:hidden dark:divide-slate-800",
                                fetching && "opacity-60",
                            )}
                        >
                            {leads.map((lead) => (
                                <li key={lead.id} className="px-4 py-3.5">
                                    <div className="flex items-start gap-3">
                                        <span
                                            className={cx(
                                                "flex size-9 shrink-0 items-center justify-center rounded-full text-xs font-semibold",
                                                avatarColor(lead.name),
                                            )}
                                            aria-hidden="true"
                                        >
                                            {initials(lead.name)}
                                        </span>

                                        <div className="min-w-0 flex-1">
                                            <Link to={`/leads/${lead.id}`} className="block">
                                                <p className="truncate text-sm font-medium text-slate-900 dark:text-slate-100">
                                                    {lead.name}
                                                </p>
                                                <p className="truncate text-xs text-slate-500 dark:text-slate-400">
                                                    {lead.company}
                                                </p>
                                            </Link>

                                            <div className="mt-2 flex flex-wrap items-center gap-2">
                                                <StagePicker
                                                    stage={lead.stage}
                                                    size="sm"
                                                    busy={stageBusyId === lead.id}
                                                    onChange={(next) => handleStageChange(lead, next)}
                                                />
                                                <span className="text-[11px] text-slate-400 dark:text-slate-500">
                                                    {SOURCE_STYLES[lead.source].label} ·{" "}
                                                    {formatRelative(lead.created_at)}
                                                </span>
                                            </div>
                                        </div>

                                        <div className="flex shrink-0 gap-0.5">
                                            <IconButton
                                                label={`Edit ${lead.name}`}
                                                onClick={() => {
                                                    setEditing(lead);
                                                    setFormOpen(true);
                                                }}
                                            >
                                                <Pencil className="size-4" aria-hidden="true" />
                                            </IconButton>
                                            <IconButton
                                                label={`Delete ${lead.name}`}
                                                danger
                                                onClick={() => setDeleting(lead)}
                                            >
                                                <Trash2 className="size-4" aria-hidden="true" />
                                            </IconButton>
                                        </div>
                                    </div>
                                </li>
                            ))}
                        </ul>

                        {data && (
                            <div className="border-t border-slate-200 dark:border-slate-800">
                                <Pagination
                                    pagination={data.pagination}
                                    disabled={fetching}
                                    onPageChange={(nextPage) =>
                                        updateParams({ page: String(nextPage) }, false)
                                    }
                                />
                            </div>
                        )}
                    </>
                )}
            </Card>

            <LeadFormModal
                open={formOpen}
                lead={editing}
                onClose={() => setFormOpen(false)}
                onSubmit={handleSubmit}
            />

            <ConfirmDialog
                open={deleting !== undefined}
                onClose={() => setDeleting(undefined)}
                onConfirm={handleDelete}
                title="Delete lead"
                message={
                    deleting
                        ? `Delete ${deleting.name} from ${deleting.company}? Their entire activity history will be removed too. This cannot be undone.`
                        : ""
                }
            />
        </>
    );
}

// ---------------------------------------------------------------------------
// Pieces
// ---------------------------------------------------------------------------

function StageChip({
    label,
    dot,
    active,
    onClick,
}: {
    label: string;
    dot?: string;
    active: boolean;
    onClick: () => void;
}) {
    return (
        <button
            type="button"
            onClick={onClick}
            aria-pressed={active}
            className={cx(
                "inline-flex shrink-0 items-center gap-1.5 rounded-full border px-3 py-1 text-xs font-medium transition-colors",
                active
                    ? "border-brand-600 bg-brand-600 text-white dark:border-brand-500 dark:bg-brand-500"
                    : "border-slate-200 bg-white text-slate-600 hover:border-slate-300 hover:bg-slate-50 dark:border-slate-700 dark:bg-slate-900 dark:text-slate-300 dark:hover:bg-slate-800",
            )}
        >
            {dot && (
                <span
                    className={cx(
                        "size-1.5 rounded-full",
                        // On the active (solid brand) chip the stage colour would
                        // clash, so it becomes a plain white dot.
                        active ? "bg-white/70" : dot,
                    )}
                    aria-hidden="true"
                />
            )}
            {label}
        </button>
    );
}

/** A column header that sorts, showing the current direction with an arrow. */
function SortableHeader({
    field,
    label,
    sortBy,
    sortDir,
    onSort,
}: {
    field: SortField;
    label: string;
    sortBy: SortField;
    sortDir: "asc" | "desc";
    onSort: (field: SortField) => void;
}) {
    const active = sortBy === field;

    return (
        <th
            scope="col"
            className="px-4 py-2.5 font-medium"
            // aria-sort is what tells a screen reader the table is sorted, and how.
            aria-sort={active ? (sortDir === "asc" ? "ascending" : "descending") : "none"}
        >
            <button
                type="button"
                onClick={() => onSort(field)}
                className={cx(
                    "inline-flex items-center gap-1 transition-colors",
                    active
                        ? "text-slate-900 dark:text-slate-100"
                        : "text-slate-500 hover:text-slate-800 dark:text-slate-400 dark:hover:text-slate-200",
                )}
            >
                {label}
                {active ? (
                    sortDir === "asc" ? (
                        <ArrowUp className="size-3" aria-hidden="true" />
                    ) : (
                        <ArrowDown className="size-3" aria-hidden="true" />
                    )
                ) : (
                    <ArrowDown className="size-3 opacity-0 transition-opacity group-hover:opacity-40" aria-hidden="true" />
                )}
            </button>
        </th>
    );
}

function IconButton({
    label,
    onClick,
    danger,
    children,
}: {
    label: string;
    onClick: () => void;
    danger?: boolean;
    children: React.ReactNode;
}) {
    return (
        <button
            type="button"
            onClick={onClick}
            aria-label={label}
            title={label}
            className={cx(
                "rounded-lg p-1.5 transition-colors",
                danger
                    ? "text-slate-400 hover:bg-rose-50 hover:text-rose-600 dark:hover:bg-rose-950/60 dark:hover:text-rose-400"
                    : "text-slate-400 hover:bg-slate-200/70 hover:text-slate-700 dark:hover:bg-slate-700 dark:hover:text-slate-200",
            )}
        >
            {children}
        </button>
    );
}

function TableSkeleton() {
    return (
        <div className="divide-y divide-slate-100 dark:divide-slate-800">
            {Array.from({ length: 6 }).map((_, index) => (
                <div key={index} className="flex items-center gap-4 px-4 py-3.5">
                    <Skeleton className="size-8 shrink-0 rounded-full" />
                    <div className="flex-1 space-y-1.5">
                        <Skeleton className="h-3.5 w-40" />
                        <Skeleton className="h-3 w-28" />
                    </div>
                    <Skeleton className="hidden h-6 w-24 rounded-full sm:block" />
                    <Skeleton className="hidden h-3 w-16 md:block" />
                </div>
            ))}
        </div>
    );
}
