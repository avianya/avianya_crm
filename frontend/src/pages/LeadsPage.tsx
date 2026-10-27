import { useCallback, useEffect, useState } from "react";
import { Link, useSearchParams } from "react-router-dom";
import {
    ArrowDown,
    ArrowUp,
    ChevronRight,
    Download,
    FileUp,
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

import { ApiError, api, saveFile } from "../lib/api";
import {
    BAND_ORDER,
    BAND_STYLES,
    QUALITY_ORDER,
    QUALITY_STYLES,
    SOURCE_STYLES,
    STAGE_ORDER,
    STAGE_STYLES,
    avatarColor,
    cx,
    formatDate,
    formatRelative,
    initials,
    todayISODate,
} from "../lib/display";
import type { BulkInput, Lead, LeadInput, LeadListParams, Quality, ScoreBand, Source, Stage } from "../lib/types";
import { CapiStatusBadge, QualityBadge } from "../components/QualityBadges";
import { useAuth } from "../context/auth";
import { useAsync } from "../hooks/useAsync";
import { useCloseReasonPrompt } from "../hooks/useCloseReasonPrompt";
import { useDebouncedValue } from "../hooks/useDebouncedValue";
import { memberName, useTeam } from "../hooks/useTeam";
import { useToast } from "../context/toast";
import { BulkActionBar } from "../components/BulkActionBar";
import { ConfirmDialog } from "../components/ConfirmDialog";
import { ImportLeadsModal } from "../components/ImportLeadsModal";
import { LeadFormModal } from "../components/LeadFormModal";
import { Pagination } from "../components/Pagination";
import { StagePicker } from "../components/StagePicker";
import { HealthBadge, ScorePill, StuckBadge } from "../components/LeadInsights";
import {
    Button,
    Card,
    EmptyState,
    ErrorState,
    PageHeader,
    RefreshError,
    Select,
    Skeleton,
    Spinner,
    TextInput,
} from "../components/ui";

type SortField = NonNullable<LeadListParams["sort_by"]>;

/** Mirrors model.MaxScoredLeads: the most leads one export holds. */
const MAX_EXPORT = 10_000;

/**
 * The leads list: search, filter, sort, paginate, change stage inline.
 *
 * Filter state lives in the URL query string rather than in component state. That
 * is a deliberate choice with three payoffs: the back button works, a filtered
 * view can be shared or bookmarked, and the dashboard's chart can link straight
 * to "/leads?stage=negotiation" without this page needing to know about it.
 */
export function LeadsPage() {
    const { is } = useAuth();
    // Assigning, reassigning and deleting are for admins and managers.
    const canAssign = is("admin", "manager");
    const [searchParams, setSearchParams] = useSearchParams();
    const toast = useToast();

    // --- filter state, read from the URL -----------------------------------
    const searchParam = searchParams.get("search") ?? "";
    const stage = (searchParams.get("stage") ?? "all") as Stage | "all";
    const source = (searchParams.get("source") ?? "all") as Source | "all";
    // "all", "none" (unowned) or a team member's id.
    const owner = searchParams.get("owner") ?? "all";
    const ownerFilter: LeadListParams["owner"] =
        owner === "none" ? "none" : /^[1-9]\d*$/.test(owner) ? Number(owner) : undefined;
    const band = (searchParams.get("band") ?? "all") as ScoreBand | "all";
    const quality = (searchParams.get("quality") ?? "all") as Quality | "all";
    const sortBy = (searchParams.get("sort_by") ?? "created_at") as SortField;
    const sortDir = (searchParams.get("sort_dir") ?? "desc") as "asc" | "desc";
    const page = Math.max(1, Number(searchParams.get("page") ?? "1") || 1);

    // The input is local so typing is instant; the URL (and the request) only
    // catch up once typing pauses.
    const [searchInput, setSearchInput] = useState(searchParam);
    const debouncedSearch = useDebouncedValue(searchInput, 300);

    // Opens with the panel showing when the link already filters by owner,
    // so the filter in force is visible.
    const [showFilters, setShowFilters] = useState(owner !== "all" || band !== "all" || quality !== "all");
    const team = useTeam();
    const closeReason = useCloseReasonPrompt();

    // --- dialogs and per-row busy state ------------------------------------
    const [formOpen, setFormOpen] = useState(false);
    const [editing, setEditing] = useState<Lead | undefined>();
    const [deleting, setDeleting] = useState<Lead | undefined>();
    // Which row is mid-stage-change, so only that row shows a spinner.
    const [stageBusyId, setStageBusyId] = useState<number | null>(null);
    const [importOpen, setImportOpen] = useState(false);
    const [exporting, setExporting] = useState(false);

    // Ticked leads, for the bulk action bar. Only rows on this page can be
    // ticked, so a new page or filter starts with none.
    const [selected, setSelected] = useState<Set<number>>(() => new Set());
    const [bulkBusy, setBulkBusy] = useState(false);
    const [bulkDeleting, setBulkDeleting] = useState(false);

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

    // The URL is the source of truth: when it changes from outside the input
    // (Clear filters, Back/Forward, a link), the input follows it.
    useEffect(() => {
        setSearchInput(searchParam);
    }, [searchParam]);

    // Push the debounced search term into the URL once typing settles. Both
    // guards matter: waiting until the debounced value has caught up with the
    // input stops a just-cleared search from being written back for 300ms, and
    // the equality check stops the effect re-running on its own URL change.
    useEffect(() => {
        if (debouncedSearch !== searchInput || debouncedSearch === searchParam) return;
        updateParams({ search: debouncedSearch });
    }, [debouncedSearch, searchInput, searchParam, updateParams]);

    // --- data ---------------------------------------------------------------
    const { data, error, loading, fetching, refetch, setData } = useAsync(
        (signal) =>
            api.listLeads(
                { search: searchParam, stage, source, owner: ownerFilter, band, quality, sort_by: sortBy, sort_dir: sortDir, page },
                signal,
            ),
        [searchParam, stage, source, ownerFilter, band, quality, sortBy, sortDir, page],
    );

    // A page past the end -- the last row of the last page was just deleted,
    // or an old ?page= link -- steps back to the real last page instead of
    // showing an empty list with no pagination.
    const totalPages = data?.pagination.total_pages ?? 0;
    useEffect(() => {
        if (totalPages > 0 && page > totalPages) {
            updateParams({ page: String(totalPages) }, false);
        }
    }, [page, totalPages, updateParams]);

    const leads = data?.data ?? [];
    const hasActiveFilters =
        Boolean(searchParam) ||
        stage !== "all" ||
        source !== "all" ||
        ownerFilter !== undefined ||
        band !== "all" ||
        quality !== "all";

    useEffect(() => {
        setSelected(new Set());
    }, [searchParam, stage, source, ownerFilter, band, quality, sortBy, sortDir, page]);

    // --- selection and bulk actions ------------------------------------------

    const selectedLeads = leads.filter((lead) => selected.has(lead.id));
    const allOnPageSelected = leads.length > 0 && selectedLeads.length === leads.length;

    const toggleSelected = (id: number) =>
        setSelected((current) => {
            const next = new Set(current);
            if (next.has(id)) next.delete(id);
            else next.add(id);
            return next;
        });

    const togglePage = () => setSelected(allOnPageSelected ? new Set() : new Set(leads.map((lead) => lead.id)));

    /**
     * Sends one bulk change and reports it. Leads that could not be changed
     * stay ticked, so the person can see which and try again.
     */
    const runBulk = async (input: BulkInput, describe: (count: number) => string) => {
        setBulkBusy(true);
        try {
            const result = await api.bulkLeads(input);
            if (result.changed > 0) toast.success(describe(result.changed));
            if (result.failed.length > 0) {
                const first = result.failed[0];
                const name = leads.find((lead) => lead.id === first.id)?.name ?? "A lead";
                toast.error(
                    result.failed.length === 1
                        ? `${name} was not changed: ${first.message}`
                        : `${result.failed.length} leads were not changed. ${name}: ${first.message}`,
                );
            }
            setSelected(new Set(result.failed.map((f) => f.id)));
            refetch();
        } catch (cause) {
            toast.error(cause instanceof ApiError ? cause.message : "Could not change the leads.");
        } finally {
            setBulkBusy(false);
        }
    };

    const leadsWord = (count: number) => (count === 1 ? "1 lead" : `${count} leads`);
    const ids = selectedLeads.map((lead) => lead.id);

    const bulkOwner = (ownerId: number | null) =>
        runBulk({ ids, action: "owner", owner_id: ownerId }, (n) =>
            ownerId === null
                ? `${leadsWord(n)} now unowned.`
                : `${leadsWord(n)} given to ${memberName(team.data, ownerId) ?? "the new owner"}.`,
        );

    const bulkStage = async (next: Stage) => {
        // Won and Lost ask why once, for all of them.
        const first = selectedLeads[0];
        if (!first) return;
        const close = await closeReason.ask({ ...first, name: leadsWord(ids.length) }, next);
        if (close === null) return;
        await runBulk({ ids, action: "stage", stage: next, ...close }, (n) =>
            `${leadsWord(n)} moved to ${STAGE_STYLES[next].label}.`,
        );
    };

    const bulkSource = (next: Source) =>
        runBulk({ ids, action: "source", source: next }, (n) =>
            `${leadsWord(n)} now from ${SOURCE_STYLES[next].label}.`,
        );

    const bulkDelete = async () => {
        await runBulk({ ids, action: "delete" }, (n) => `${leadsWord(n)} deleted.`);
    };

    const exportSelected = async () => {
        setBulkBusy(true);
        try {
            const file = await api.exportLeads({ ids: ids.join(","), sort_by: sortBy, sort_dir: sortDir });
            saveFile(file, `leads-${todayISODate()}.csv`);
        } catch (cause) {
            toast.error(cause instanceof ApiError ? cause.message : "Could not export the leads.");
        } finally {
            setBulkBusy(false);
        }
    };

    // --- export -------------------------------------------------------------

    /** Every lead matching the filters and sort, not just this page. */
    const exportCSV = async () => {
        setExporting(true);
        try {
            const file = await api.exportLeads({
                search: searchParam, stage, source, owner: ownerFilter, band, quality, sort_by: sortBy, sort_dir: sortDir,
            });
            saveFile(file, `leads-${todayISODate()}.csv`);
            const total = data?.pagination.total_items ?? 0;
            if (total > MAX_EXPORT) {
                toast.error(`Only the first ${MAX_EXPORT.toLocaleString("en-IN")} of ${total.toLocaleString("en-IN")} leads were exported. Filter the list to export the rest.`);
            }
        } catch (cause) {
            toast.error(cause instanceof ApiError ? cause.message : "Could not export the leads.");
        } finally {
            setExporting(false);
        }
    };

    // --- mutations ----------------------------------------------------------

    const handleStageChange = async (lead: Lead, next: Stage) => {
        // Won and Lost ask why first; cancelling leaves the lead as it was.
        const close = await closeReason.ask(lead, next);
        if (close === null) return;

        setStageBusyId(lead.id);
        try {
            const updated = await api.updateLeadStage(lead.id, next, close);

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
            // Text sorts read better A→Z; dates, stages and scores read better
            // newest, furthest-along or hottest first.
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
                    <div className="flex flex-wrap gap-2">
                        <Button onClick={() => setImportOpen(true)}>
                            <FileUp className="size-4" aria-hidden="true" />
                            Import
                        </Button>
                        <Button
                            onClick={exportCSV}
                            loading={exporting}
                            disabled={exporting || !data || data.pagination.total_items === 0}
                            title={hasActiveFilters ? "Export the leads matching your filters" : "Export every lead"}
                        >
                            <Download className="size-4" aria-hidden="true" />
                            Export
                        </Button>
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
                    </div>
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
                        <div className="grid gap-3 pt-1 animate-slide-up sm:grid-cols-2 lg:grid-cols-5">
                            <div>
                                <label
                                    htmlFor="filter-quality"
                                    className="mb-1 block text-xs font-medium text-slate-600 dark:text-slate-400"
                                >
                                    Quality
                                </label>
                                <Select
                                    id="filter-quality"
                                    value={quality}
                                    onChange={(event) => updateParams({ quality: event.target.value })}
                                >
                                    <option value="all">Any quality</option>
                                    {QUALITY_ORDER.map((option) => (
                                        <option key={option} value={option}>
                                            {QUALITY_STYLES[option].label}
                                        </option>
                                    ))}
                                </Select>
                            </div>

                            <div>
                                <label
                                    htmlFor="filter-band"
                                    className="mb-1 block text-xs font-medium text-slate-600 dark:text-slate-400"
                                >
                                    Score
                                </label>
                                <Select
                                    id="filter-band"
                                    value={band}
                                    onChange={(event) => updateParams({ band: event.target.value })}
                                >
                                    <option value="all">Any score</option>
                                    {BAND_ORDER.map((option) => (
                                        <option key={option} value={option}>
                                            {BAND_STYLES[option].label}
                                        </option>
                                    ))}
                                </Select>
                            </div>

                            {canAssign && (
                            <div>
                                <label
                                    htmlFor="filter-owner"
                                    className="mb-1 block text-xs font-medium text-slate-600 dark:text-slate-400"
                                >
                                    Owner
                                </label>
                                <Select
                                    id="filter-owner"
                                    value={ownerFilter === undefined ? "all" : String(ownerFilter)}
                                    onChange={(event) => updateParams({ owner: event.target.value })}
                                >
                                    <option value="all">Anyone</option>
                                    <option value="none">No owner</option>
                                    {(team.data ?? []).map((member) => (
                                        <option key={member.id} value={member.id}>
                                            {member.name}
                                        </option>
                                    ))}
                                    {/* A link to someone not (or no longer) in the
                                        team still shows what it is filtering by. */}
                                    {typeof ownerFilter === "number" &&
                                        team.data &&
                                        !team.data.some((member) => member.id === ownerFilter) && (
                                            <option value={ownerFilter}>Removed member</option>
                                        )}
                                </Select>
                            </div>
                            )}

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
                                    <option value="score">Score</option>
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
                                updateParams({ search: null, stage: null, source: null, owner: null, band: null, quality: null });
                            }}
                            className="inline-flex items-center gap-1 text-xs font-medium text-slate-500 transition-colors hover:text-slate-800 dark:text-slate-400 dark:hover:text-slate-200"
                        >
                            <X className="size-3.5" aria-hidden="true" />
                            Clear filters
                        </button>
                    )}
                </div>

                {/* --- Results --- */}
                {error && data && (
                    <RefreshError message={error.message} onRetry={refetch} className="mx-4 mt-3 sm:mx-5" />
                )}

                {loading ? (
                    <TableSkeleton />
                ) : error && !data ? (
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
                                        updateParams({ search: null, stage: null, source: null, owner: null, band: null, quality: null });
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
                                        <th scope="col" className="w-10 py-2.5 pl-4">
                                            <Checkbox
                                                label={allOnPageSelected ? "Untick every lead on this page" : "Tick every lead on this page"}
                                                checked={allOnPageSelected}
                                                indeterminate={selectedLeads.length > 0 && !allOnPageSelected}
                                                onChange={togglePage}
                                            />
                                        </th>
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
                                            Owner
                                        </th>
                                        <th scope="col" className="px-4 py-2.5 font-medium text-slate-500 dark:text-slate-400">
                                            Health
                                        </th>
                                        <SortableHeader
                                            field="score"
                                            label="Score"
                                            sortBy={sortBy}
                                            sortDir={sortDir}
                                            onSort={toggleSort}
                                            align="right"
                                            title="Lead score, 0-100: Hot 70+, Warm 40-69, Cold under 40. Open a lead to see how it is worked out."
                                        />
                                        <th
                                            scope="col"
                                            className="px-4 py-2.5 font-medium text-slate-500 dark:text-slate-400"
                                            title="Lead quality (Good / Average / Poor) and the latest event sent to Meta"
                                        >
                                            Quality
                                        </th>
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
                                                className={cx(
                                                    "group transition-colors hover:bg-slate-50 dark:hover:bg-slate-800/40",
                                                    selected.has(lead.id) && "bg-brand-50/60 dark:bg-brand-950/30",
                                                )}
                                            >
                                                <td className="w-10 py-3 pl-4">
                                                    <Checkbox
                                                        label={`Tick ${lead.name}`}
                                                        checked={selected.has(lead.id)}
                                                        onChange={() => toggleSelected(lead.id)}
                                                    />
                                                </td>
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
                                                    <OwnerCell name={memberName(team.data, lead.owner_id)} />
                                                </td>

                                                <td className="px-4 py-3">
                                                    <div className="flex flex-wrap gap-1.5">
                                                        <HealthBadge lead={lead} />
                                                        <StuckBadge lead={lead} />
                                                    </div>
                                                </td>

                                                <td className="px-4 py-3 text-right">
                                                    <ScorePill score={lead.score} />
                                                </td>

                                                <td className="px-4 py-3">
                                                    <div className="flex flex-col items-start gap-1">
                                                        <QualityBadge
                                                            quality={lead.quality}
                                                            score={lead.quality_override ?? lead.quality_score}
                                                            title={lead.quality_reason}
                                                        />
                                                        {lead.meta_capi_status && (
                                                            <CapiStatusBadge
                                                                status={lead.meta_capi_status}
                                                                eventName={`Meta: ${lead.meta_last_event_name}`}
                                                                at={lead.meta_last_event_at}
                                                            />
                                                        )}
                                                    </div>
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
                                                        {canAssign && (
                                                            <IconButton
                                                                label={`Delete ${lead.name}`}
                                                                danger
                                                                onClick={() => setDeleting(lead)}
                                                            >
                                                                <Trash2 className="size-4" aria-hidden="true" />
                                                            </IconButton>
                                                        )}
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
                            {leads.length > 0 && (
                                <li className="flex items-center gap-3 px-4 py-2 text-xs text-slate-500 dark:text-slate-400">
                                    <Checkbox
                                        label={allOnPageSelected ? "Untick every lead on this page" : "Tick every lead on this page"}
                                        checked={allOnPageSelected}
                                        indeterminate={selectedLeads.length > 0 && !allOnPageSelected}
                                        onChange={togglePage}
                                    />
                                    {selectedLeads.length > 0 ? `${selectedLeads.length} selected` : "Select all on this page"}
                                </li>
                            )}
                            {leads.map((lead) => (
                                <li
                                    key={lead.id}
                                    className={cx("px-4 py-3.5", selected.has(lead.id) && "bg-brand-50/60 dark:bg-brand-950/30")}
                                >
                                    <div className="flex items-start gap-3">
                                        <span className="pt-2">
                                            <Checkbox
                                                label={`Tick ${lead.name}`}
                                                checked={selected.has(lead.id)}
                                                onChange={() => toggleSelected(lead.id)}
                                            />
                                        </span>
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
                                                <HealthBadge lead={lead} />
                                                <StuckBadge lead={lead} />
                                                {lead.score && (
                                                    <span className="text-[11px] text-slate-400 dark:text-slate-500">
                                                        Score <ScorePill score={lead.score} />
                                                    </span>
                                                )}
                                                <span className="text-[11px] text-slate-400 dark:text-slate-500">
                                                    {memberName(team.data, lead.owner_id) ?? "No owner"} ·{" "}
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

            {selectedLeads.length > 0 && (
                <BulkActionBar
                    count={selectedLeads.length}
                    team={team.data ?? []}
                    busy={bulkBusy}
                    onOwner={canAssign ? bulkOwner : undefined}
                    onStage={bulkStage}
                    onSource={bulkSource}
                    onExport={exportSelected}
                    onDelete={canAssign ? () => setBulkDeleting(true) : undefined}
                    onClear={() => setSelected(new Set())}
                />
            )}

            <ConfirmDialog
                open={bulkDeleting}
                onClose={() => setBulkDeleting(false)}
                onConfirm={bulkDelete}
                title={`Delete ${leadsWord(selectedLeads.length)}`}
                confirmLabel="Delete"
                message={`Delete ${leadsWord(selectedLeads.length)}? Their entire activity history will be removed too. This cannot be undone.`}
            />

            {closeReason.dialog}

            <LeadFormModal
                open={formOpen}
                lead={editing}
                onClose={() => setFormOpen(false)}
                onSubmit={handleSubmit}
            />

            <ImportLeadsModal
                open={importOpen}
                onClose={() => setImportOpen(false)}
                onImported={(count) => {
                    toast.success(count === 1 ? "1 lead imported." : `${count} leads imported.`);
                    refetch();
                }}
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
    align = "left",
    title,
}: {
    field: SortField;
    label: string;
    sortBy: SortField;
    sortDir: "asc" | "desc";
    onSort: (field: SortField) => void;
    align?: "left" | "right";
    title?: string;
}) {
    const active = sortBy === field;

    return (
        <th
            scope="col"
            className={cx("px-4 py-2.5 font-medium", align === "right" && "text-right")}
            title={title}
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

/** The owner's name with their initials, or a quiet "No owner". */
function OwnerCell({ name }: { name: string | null }) {
    if (!name) {
        return <span className="text-xs whitespace-nowrap text-slate-400 dark:text-slate-500">No owner</span>;
    }
    return (
        <span className="inline-flex items-center gap-1.5 text-xs whitespace-nowrap text-slate-600 dark:text-slate-300">
            <span
                className={cx(
                    "flex size-5 shrink-0 items-center justify-center rounded-full text-[9px] font-semibold",
                    avatarColor(name),
                )}
                aria-hidden="true"
            >
                {initials(name)}
            </span>
            <span className="max-w-[8rem] truncate">{name}</span>
        </span>
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

/**
 * A row's tick box. indeterminate (some but not all rows ticked) can only be
 * set from script, hence the ref callback.
 */
function Checkbox({
    label,
    checked,
    indeterminate = false,
    onChange,
}: {
    label: string;
    checked: boolean;
    indeterminate?: boolean;
    onChange: () => void;
}) {
    return (
        <input
            type="checkbox"
            aria-label={label}
            checked={checked}
            ref={(el) => {
                if (el) el.indeterminate = indeterminate;
            }}
            onChange={onChange}
            className="size-4 cursor-pointer rounded border-slate-300 text-brand-600 focus:ring-brand-500 dark:border-slate-600"
        />
    );
}
