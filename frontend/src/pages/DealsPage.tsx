import { useCallback, useEffect, useMemo, useState } from "react";
import { Link, useSearchParams } from "react-router-dom";
import {
    ArrowDown,
    ArrowUp,
    BadgeIndianRupee,
    ChevronRight,
    Handshake,
    Percent,
    Search,
    SearchX,
    SquareKanban,
    Trophy,
    X,
    type LucideIcon,
} from "lucide-react";

import { api } from "../lib/api";
import { reasonLabel } from "../lib/closeReasons";
import {
    avatarColor,
    cx,
    formatDate,
    formatMoney,
    formatMoneyCompact,
    formatRelative,
    initials,
    isClosedStage,
} from "../lib/display";
import type { Lead, LeadListParams, Meta, PipelineColumn, Stage } from "../lib/types";
import { useAsync } from "../hooks/useAsync";
import { useDebouncedValue } from "../hooks/useDebouncedValue";
import { memberName, useTeam } from "../hooks/useTeam";
import { HealthBadge, StuckBadge } from "../components/LeadInsights";
import { Pagination } from "../components/Pagination";
import { StageBadge } from "../components/StageBadge";
import { Card, EmptyState, ErrorState, PageHeader, RefreshError, Skeleton, Spinner, TextInput } from "../components/ui";

type SortField = NonNullable<LeadListParams["sort_by"]>;
type DealTab = "open" | "won" | "lost";

const TABS: { value: DealTab; label: string }[] = [
    { value: "open", label: "Open" },
    { value: "won", label: "Won" },
    { value: "lost", label: "Lost" },
];

const PAGE_SIZE = 20;

/**
 * Deals: every lead seen as the deal it carries -- value, chance of closing,
 * weighted value and owner -- in a sortable list. The Pipeline page is the same
 * data as a board; this is the view for scanning and ranking by money.
 *
 * Like the leads list, all filter state lives in the URL.
 */
export function DealsPage() {
    const [searchParams, setSearchParams] = useSearchParams();

    const tabParam = searchParams.get("tab");
    const tab: DealTab = tabParam === "won" || tabParam === "lost" ? tabParam : "open";
    const searchParam = searchParams.get("search") ?? "";
    const sortBy = (searchParams.get("sort_by") ?? "deal_value") as SortField;
    const sortDir = (searchParams.get("sort_dir") ?? "desc") as "asc" | "desc";
    const page = Math.max(1, Number(searchParams.get("page") ?? "1") || 1);

    const [searchInput, setSearchInput] = useState(searchParam);
    const debouncedSearch = useDebouncedValue(searchInput, 300);

    const updateParams = useCallback(
        (updates: Record<string, string | null>, resetPage = true) => {
            setSearchParams(
                (current) => {
                    const next = new URLSearchParams(current);
                    for (const [key, value] of Object.entries(updates)) {
                        if (value === null || value === "") next.delete(key);
                        else next.set(key, value);
                    }
                    if (resetPage) next.delete("page");
                    return next;
                },
                { replace: true },
            );
        },
        [setSearchParams],
    );

    // The URL is the source of truth: when it changes from outside the input
    // (Clear, Back/Forward), the input follows it.
    useEffect(() => {
        setSearchInput(searchParam);
    }, [searchParam]);

    // Push typing into the URL once it settles. Waiting until the debounced
    // value has caught up with the input is what stops a just-cleared search
    // from being written back for 300ms.
    useEffect(() => {
        if (debouncedSearch !== searchInput || debouncedSearch === searchParam) return;
        updateParams({ search: debouncedSearch });
    }, [debouncedSearch, searchInput, searchParam, updateParams]);

    // --- data ---------------------------------------------------------------

    const { data, error, loading, fetching, refetch } = useAsync(
        (signal) =>
            api.listLeads(
                {
                    search: searchParam,
                    status: tab === "open" ? "open" : undefined,
                    stage: tab === "open" ? undefined : tab,
                    sort_by: sortBy,
                    sort_dir: sortDir,
                    page,
                    page_size: PAGE_SIZE,
                },
                signal,
            ),
        [searchParam, tab, sortBy, sortDir, page],
    );

    // The pipeline endpoint already has true per-stage totals and win
    // probabilities, so the summary never depends on which page is showing.
    const pipeline = useAsync((signal) => api.pipeline(signal), []);
    const team = useTeam();
    // Labels for the Won and Lost tabs' reason column.
    const { data: meta } = useAsync((signal) => api.meta(signal), []);

    const summary = useMemo(() => summarise(pipeline.data), [pipeline.data]);

    // A stale ?page= past the end (or the last row of the last page vanishing)
    // would otherwise show an empty list with no way back.
    const totalPages = data?.pagination.total_pages ?? 0;
    useEffect(() => {
        if (totalPages > 0 && page > totalPages) {
            updateParams({ page: String(totalPages) }, false);
        }
    }, [page, totalPages, updateParams]);

    const deals = data?.data ?? [];

    const toggleSort = (field: SortField) => {
        if (sortBy === field) {
            updateParams({ sort_dir: sortDir === "asc" ? "desc" : "asc" }, false);
        } else {
            const naturalDirection = field === "company" || field === "name" ? "asc" : "desc";
            updateParams({ sort_by: field, sort_dir: naturalDirection }, false);
        }
    };

    const clearSearch = () => {
        setSearchInput("");
        updateParams({ search: null });
    };

    const isOpenTab = tab === "open";

    return (
        <>
            <PageHeader
                title="Deals"
                subtitle={
                    summary
                        ? `${formatMoney(summary.openValue)} in open deals · ${formatMoney(summary.forecast)} weighted forecast`
                        : "Every lead's deal, ranked by value."
                }
                action={
                    <Link
                        to="/pipeline"
                        className="inline-flex items-center gap-2 rounded-lg border border-slate-200 bg-white px-3.5 py-2 text-sm font-medium text-slate-700 shadow-sm transition-colors hover:bg-slate-50 dark:border-slate-700 dark:bg-slate-900 dark:text-slate-200 dark:hover:bg-slate-800"
                    >
                        <SquareKanban className="size-4" aria-hidden="true" />
                        Board view
                    </Link>
                }
            />

            {/* --- Summary --- */}
            <div className="mb-6 grid grid-cols-2 gap-4 lg:grid-cols-4">
                <SummaryTile
                    icon={Handshake}
                    label="Open pipeline"
                    value={summary ? formatMoneyCompact(summary.openValue) : null}
                    caption={summary ? `${summary.openCount} open ${summary.openCount === 1 ? "deal" : "deals"}` : ""}
                    accent="text-brand-600 bg-brand-50 dark:text-brand-400 dark:bg-brand-950/60"
                />
                <SummaryTile
                    icon={BadgeIndianRupee}
                    label="Weighted forecast"
                    value={summary ? formatMoneyCompact(summary.forecast) : null}
                    caption="Value × stage win chance"
                    accent="text-sky-600 bg-sky-50 dark:text-sky-400 dark:bg-sky-950/60"
                />
                <SummaryTile
                    icon={Trophy}
                    label="Won"
                    value={summary ? formatMoneyCompact(summary.wonValue) : null}
                    caption={summary ? `${summary.wonCount} ${summary.wonCount === 1 ? "deal" : "deals"} closed` : ""}
                    accent="text-emerald-600 bg-emerald-50 dark:text-emerald-400 dark:bg-emerald-950/60"
                />
                <SummaryTile
                    icon={Percent}
                    label="Win rate"
                    value={summary ? (summary.winRate === null ? "—" : `${summary.winRate}%`) : null}
                    caption={summary ? `${summary.wonCount} won · ${summary.lostCount} lost` : ""}
                    accent="text-amber-600 bg-amber-50 dark:text-amber-400 dark:bg-amber-950/60"
                />
            </div>
            {pipeline.error && (
                <p className="-mt-4 mb-6 text-xs text-rose-600 dark:text-rose-400">
                    Totals could not be loaded.{" "}
                    <button type="button" onClick={pipeline.refetch} className="font-medium underline">
                        Try again
                    </button>
                </p>
            )}

            <Card>
                {/* --- Tabs and search --- */}
                <div className="flex flex-col gap-3 border-b border-slate-200 px-4 py-4 sm:flex-row sm:items-center sm:px-5 dark:border-slate-800">
                    <div className="flex gap-1.5" role="group" aria-label="Deal status">
                        {TABS.map((option) => {
                            const active = tab === option.value;
                            const count = summary?.counts[option.value];
                            return (
                                <button
                                    key={option.value}
                                    type="button"
                                    aria-pressed={active}
                                    onClick={() => updateParams({ tab: option.value === "open" ? null : option.value })}
                                    className={cx(
                                        "inline-flex items-center gap-1.5 rounded-full border px-3 py-1 text-xs font-medium transition-colors",
                                        active
                                            ? "border-brand-600 bg-brand-600 text-white dark:border-brand-500 dark:bg-brand-500"
                                            : "border-slate-200 bg-white text-slate-600 hover:border-slate-300 hover:bg-slate-50 dark:border-slate-700 dark:bg-slate-900 dark:text-slate-300 dark:hover:bg-slate-800",
                                    )}
                                >
                                    {option.label}
                                    {count !== undefined && (
                                        <span className={cx("tabular", active ? "text-white/75" : "text-slate-400 dark:text-slate-500")}>
                                            {count}
                                        </span>
                                    )}
                                </button>
                            );
                        })}
                    </div>

                    <div className="relative flex-1 sm:max-w-xs sm:ml-auto">
                        <Search
                            className="pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2 text-slate-400"
                            aria-hidden="true"
                        />
                        <TextInput
                            type="search"
                            value={searchInput}
                            onChange={(event) => setSearchInput(event.target.value)}
                            placeholder="Search by company or contact…"
                            aria-label="Search deals by company or contact"
                            className="pl-9"
                        />
                        {fetching && !loading && (
                            <Spinner className="absolute top-1/2 right-3 size-4 -translate-y-1/2" />
                        )}
                    </div>
                </div>

                {/* --- Results --- */}
                {loading ? (
                    <ListSkeleton />
                ) : error && !data ? (
                    <div className="p-5">
                        <ErrorState message={error.message} onRetry={refetch} />
                    </div>
                ) : deals.length === 0 ? (
                    searchParam ? (
                        <EmptyState
                            icon={SearchX}
                            title="No deals match that search"
                            description="Try a different company or contact name."
                            action={
                                <button
                                    type="button"
                                    onClick={clearSearch}
                                    className="inline-flex items-center gap-1 text-sm font-medium text-brand-600 hover:underline dark:text-brand-400"
                                >
                                    <X className="size-4" aria-hidden="true" />
                                    Clear search
                                </button>
                            }
                        />
                    ) : (
                        <EmptyState
                            icon={Handshake}
                            title={EMPTY_TITLES[tab]}
                            description={EMPTY_DESCRIPTIONS[tab]}
                        />
                    )
                ) : (
                    <>
                        {error && (
                            <RefreshError message={error.message} onRetry={refetch} className="mx-4 mt-3 sm:mx-5" />
                        )}

                        {/* --- Desktop table --- */}
                        <div className={cx("hidden overflow-x-auto transition-opacity md:block", fetching && "opacity-60")}>
                            <table className="w-full text-left text-sm">
                                <thead className="border-b border-slate-200 bg-slate-50/60 text-xs dark:border-slate-800 dark:bg-slate-950/40">
                                    <tr>
                                        <SortableHeader field="company" label="Deal" sortBy={sortBy} sortDir={sortDir} onSort={toggleSort} />
                                        <SortableHeader field="deal_value" label="Value" align="right" sortBy={sortBy} sortDir={sortDir} onSort={toggleSort} />
                                        <SortableHeader field="stage" label="Stage" sortBy={sortBy} sortDir={sortDir} onSort={toggleSort} />
                                        {!isOpenTab && (
                                            <th scope="col" className="px-4 py-2.5 font-medium text-slate-500 dark:text-slate-400">
                                                Reason
                                            </th>
                                        )}
                                        {isOpenTab && (
                                            <>
                                                <th scope="col" className="px-4 py-2.5 text-right font-medium text-slate-500 dark:text-slate-400" title="Chance of winning at this stage">
                                                    Win %
                                                </th>
                                                <th scope="col" className="px-4 py-2.5 text-right font-medium text-slate-500 dark:text-slate-400" title="Value × win %">
                                                    Weighted
                                                </th>
                                            </>
                                        )}
                                        <th scope="col" className="px-4 py-2.5 font-medium text-slate-500 dark:text-slate-400">
                                            Owner
                                        </th>
                                        {isOpenTab && (
                                            <th scope="col" className="px-4 py-2.5 font-medium text-slate-500 dark:text-slate-400">
                                                Health
                                            </th>
                                        )}
                                        <SortableHeader field="updated_at" label="Last updated" sortBy={sortBy} sortDir={sortDir} onSort={toggleSort} />
                                        <th scope="col" className="px-4 py-2.5">
                                            <span className="sr-only">Open</span>
                                        </th>
                                    </tr>
                                </thead>
                                <tbody className="divide-y divide-slate-100 dark:divide-slate-800">
                                    {deals.map((deal) => {
                                        const probability = summary?.probability[deal.stage];
                                        const owner = memberName(team.data, deal.owner_id);
                                        return (
                                            <tr key={deal.id} className="group transition-colors hover:bg-slate-50 dark:hover:bg-slate-800/40">
                                                <td className="px-4 py-3">
                                                    <DealName deal={deal} />
                                                </td>
                                                <td className="px-4 py-3 text-right">
                                                    <DealValue value={deal.deal_value} />
                                                </td>
                                                <td className="px-4 py-3">
                                                    <StageBadge stage={deal.stage} />
                                                </td>
                                                {!isOpenTab && (
                                                    <td className="max-w-64 px-4 py-3">
                                                        <CloseReasonText deal={deal} meta={meta} />
                                                    </td>
                                                )}
                                                {isOpenTab && (
                                                    <>
                                                        <td className="tabular px-4 py-3 text-right text-slate-600 dark:text-slate-300">
                                                            {probability === undefined ? "—" : `${probability}%`}
                                                        </td>
                                                        <td className="tabular px-4 py-3 text-right text-slate-600 dark:text-slate-300">
                                                            {deal.deal_value === null || probability === undefined
                                                                ? "—"
                                                                : formatMoney(Math.floor((deal.deal_value * probability) / 100))}
                                                        </td>
                                                    </>
                                                )}
                                                <td className="px-4 py-3">
                                                    <OwnerLabel name={owner} />
                                                </td>
                                                {isOpenTab && (
                                                    <td className="px-4 py-3">
                                                        <div className="flex flex-wrap gap-1.5">
                                                            <HealthBadge lead={deal} />
                                                            <StuckBadge lead={deal} />
                                                        </div>
                                                    </td>
                                                )}
                                                <td className="px-4 py-3">
                                                    <span className="text-xs whitespace-nowrap text-slate-500 dark:text-slate-400" title={formatDate(deal.updated_at)}>
                                                        {formatRelative(deal.updated_at)}
                                                    </span>
                                                </td>
                                                <td className="px-4 py-3 text-right">
                                                    <Link
                                                        to={`/leads/${deal.id}`}
                                                        aria-label={`Open ${deal.company}`}
                                                        className="inline-flex rounded-lg p-1.5 text-slate-400 transition-colors hover:bg-slate-200/70 hover:text-slate-700 dark:hover:bg-slate-700 dark:hover:text-slate-200"
                                                    >
                                                        <ChevronRight className="size-4" aria-hidden="true" />
                                                    </Link>
                                                </td>
                                            </tr>
                                        );
                                    })}
                                </tbody>
                            </table>
                        </div>

                        {/* --- Mobile cards --- */}
                        <ul className={cx("divide-y divide-slate-100 transition-opacity md:hidden dark:divide-slate-800", fetching && "opacity-60")}>
                            {deals.map((deal) => {
                                const owner = memberName(team.data, deal.owner_id);
                                return (
                                    <li key={deal.id}>
                                        <Link to={`/leads/${deal.id}`} className="flex items-start gap-3 px-4 py-3.5">
                                            <div className="min-w-0 flex-1">
                                                <div className="flex items-start justify-between gap-3">
                                                    <div className="min-w-0">
                                                        <p className="truncate text-sm font-medium text-slate-900 dark:text-slate-100">
                                                            {deal.company}
                                                        </p>
                                                        <p className="truncate text-xs text-slate-500 dark:text-slate-400">{deal.name}</p>
                                                    </div>
                                                    <DealValue value={deal.deal_value} />
                                                </div>
                                                <div className="mt-2 flex flex-wrap items-center gap-2">
                                                    <StageBadge stage={deal.stage} />
                                                    {!isClosedStage(deal.stage) && <HealthBadge lead={deal} />}
                                                    <StuckBadge lead={deal} />
                                                    <span className="text-[11px] text-slate-400 dark:text-slate-500">
                                                        {owner ?? "Unassigned"} · {formatRelative(deal.updated_at)}
                                                    </span>
                                                </div>
                                                {isClosedStage(deal.stage) && (
                                                    <div className="mt-1.5">
                                                        <CloseReasonText deal={deal} meta={meta} />
                                                    </div>
                                                )}
                                            </div>
                                            <ChevronRight className="mt-0.5 size-4 shrink-0 text-slate-400" aria-hidden="true" />
                                        </Link>
                                    </li>
                                );
                            })}
                        </ul>

                        {data && (
                            <div className="border-t border-slate-200 dark:border-slate-800">
                                <Pagination
                                    pagination={data.pagination}
                                    disabled={fetching}
                                    onPageChange={(nextPage) => updateParams({ page: String(nextPage) }, false)}
                                />
                            </div>
                        )}
                    </>
                )}
            </Card>
        </>
    );
}

// ---------------------------------------------------------------------------
// Summary maths
// ---------------------------------------------------------------------------

interface DealSummary {
    openCount: number;
    openValue: number;
    forecast: number;
    wonCount: number;
    wonValue: number;
    lostCount: number;
    /** Won as a share of closed deals; null until something has closed. */
    winRate: number | null;
    counts: Record<DealTab, number>;
    probability: Partial<Record<Stage, number>>;
}

function summarise(columns: PipelineColumn[] | null): DealSummary | null {
    if (!columns) return null;

    const open = columns.filter((column) => !isClosedStage(column.stage));
    const won = columns.find((column) => column.stage === "won");
    const lost = columns.find((column) => column.stage === "lost");
    const wonCount = won?.count ?? 0;
    const lostCount = lost?.count ?? 0;
    const openCount = open.reduce((sum, column) => sum + column.count, 0);
    const closed = wonCount + lostCount;

    return {
        openCount,
        openValue: open.reduce((sum, column) => sum + column.value, 0),
        forecast: open.reduce((sum, column) => sum + column.weighted, 0),
        wonCount,
        wonValue: won?.value ?? 0,
        lostCount,
        winRate: closed === 0 ? null : Math.round((wonCount / closed) * 100),
        counts: { open: openCount, won: wonCount, lost: lostCount },
        probability: Object.fromEntries(columns.map((column) => [column.stage, column.probability])),
    };
}

const EMPTY_TITLES: Record<DealTab, string> = {
    open: "No open deals",
    won: "No deals won yet",
    lost: "No lost deals",
};

const EMPTY_DESCRIPTIONS: Record<DealTab, string> = {
    open: "Every lead that is not Won or Lost shows up here as an open deal.",
    won: "Move a lead to Won and it will be counted here.",
    lost: "Leads marked Lost show up here, so you can see what slipped away.",
};

// ---------------------------------------------------------------------------
// Pieces
// ---------------------------------------------------------------------------

function SummaryTile({
    icon: Icon,
    label,
    value,
    caption,
    accent,
}: {
    icon: LucideIcon;
    label: string;
    /** null while loading. */
    value: string | null;
    caption: string;
    accent: string;
}) {
    return (
        <Card className="p-4 sm:p-5">
            <div className="flex items-start justify-between gap-3">
                <div className="min-w-0">
                    <p className="text-xs font-medium text-slate-500 dark:text-slate-400">{label}</p>
                    {value === null ? (
                        <Skeleton className="mt-2 h-8 w-24" />
                    ) : (
                        <p className="tabular mt-2 truncate text-2xl font-semibold tracking-tight text-slate-900 sm:text-3xl dark:text-slate-50">
                            {value}
                        </p>
                    )}
                    <p className="mt-1 truncate text-xs text-slate-400 dark:text-slate-500">{caption}</p>
                </div>
                <div className={cx("hidden size-9 shrink-0 items-center justify-center rounded-xl sm:flex", accent)}>
                    <Icon className="size-4.5" aria-hidden="true" />
                </div>
            </div>
        </Card>
    );
}

function DealName({ deal }: { deal: Lead }) {
    return (
        <Link to={`/leads/${deal.id}`} className="flex items-center gap-3">
            <span
                className={cx(
                    "flex size-8 shrink-0 items-center justify-center rounded-full text-[11px] font-semibold",
                    avatarColor(deal.company),
                )}
                aria-hidden="true"
            >
                {initials(deal.company)}
            </span>
            <span className="min-w-0">
                <span className="block truncate font-medium text-slate-900 group-hover:text-brand-700 dark:text-slate-100 dark:group-hover:text-brand-400">
                    {deal.company}
                </span>
                <span className="block truncate text-xs text-slate-500 dark:text-slate-400">{deal.name}</span>
            </span>
        </Link>
    );
}

function DealValue({ value }: { value: number | null }) {
    if (value === null) {
        return <span className="text-xs whitespace-nowrap text-slate-400 dark:text-slate-500">Not estimated</span>;
    }
    return (
        <span className="tabular font-semibold whitespace-nowrap text-slate-900 dark:text-slate-100">
            {formatMoney(value)}
        </span>
    );
}

function OwnerLabel({ name }: { name: string | null }) {
    if (!name) {
        return <span className="text-xs text-slate-400 dark:text-slate-500">Unassigned</span>;
    }
    return (
        <span className="inline-flex items-center gap-2 text-xs text-slate-600 dark:text-slate-300">
            <span
                className={cx("flex size-6 shrink-0 items-center justify-center rounded-full text-[10px] font-semibold", avatarColor(name))}
                aria-hidden="true"
            >
                {initials(name)}
            </span>
            <span className="max-w-[8rem] truncate">{name}</span>
        </span>
    );
}

function SortableHeader({
    field,
    label,
    align = "left",
    sortBy,
    sortDir,
    onSort,
}: {
    field: SortField;
    label: string;
    align?: "left" | "right";
    sortBy: SortField;
    sortDir: "asc" | "desc";
    onSort: (field: SortField) => void;
}) {
    const active = sortBy === field;
    return (
        <th
            scope="col"
            className={cx("px-4 py-2.5 font-medium", align === "right" && "text-right")}
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
                {active && (sortDir === "asc" ? <ArrowUp className="size-3" aria-hidden="true" /> : <ArrowDown className="size-3" aria-hidden="true" />)}
            </button>
        </th>
    );
}

function ListSkeleton() {
    return (
        <div className="divide-y divide-slate-100 dark:divide-slate-800">
            {Array.from({ length: 6 }).map((_, index) => (
                <div key={index} className="flex items-center gap-4 px-4 py-3.5">
                    <Skeleton className="size-8 shrink-0 rounded-full" />
                    <div className="flex-1 space-y-1.5">
                        <Skeleton className="h-3.5 w-40" />
                        <Skeleton className="h-3 w-28" />
                    </div>
                    <Skeleton className="h-4 w-20" />
                    <Skeleton className="hidden h-6 w-24 rounded-full sm:block" />
                </div>
            ))}
        </div>
    );
}

/** Why a closed deal closed: the reason's label and note, or a quiet dash. */
function CloseReasonText({ deal, meta }: { deal: Lead; meta: Meta | null | undefined }) {
    if (!deal.close_reason) {
        return <span className="text-xs text-slate-400 dark:text-slate-500">No reason recorded</span>;
    }
    return (
        <span className="block truncate text-xs text-slate-600 dark:text-slate-300" title={deal.close_note ?? undefined}>
            {reasonLabel(meta, deal.stage, deal.close_reason)}
            {deal.close_note && <span className="text-slate-400 dark:text-slate-500"> · {deal.close_note}</span>}
        </span>
    );
}
