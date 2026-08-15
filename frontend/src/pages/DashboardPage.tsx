import { Link } from "react-router-dom";
import {
    ArrowUpRight,
    Clock,
    Percent,
    TrendingUp,
    Users,
} from "lucide-react";
import type { LucideIcon } from "lucide-react";

import { api } from "../lib/api";
import {
    STAGE_STYLES,
    avatarColor,
    cx,
    formatRelative,
    initials,
} from "../lib/display";
import type { DashboardSummary, StageCount } from "../lib/types";
import { useAsync } from "../hooks/useAsync";
import { StageBadge } from "../components/StageBadge";
import {
    Card,
    CardHeader,
    EmptyState,
    ErrorState,
    PageHeader,
    Skeleton,
} from "../components/ui";

/**
 * The landing screen: four headline numbers, the pipeline as a bar chart, and the
 * most recent arrivals.
 *
 * It is one request. The backend does the aggregation in SQL and returns exactly
 * this shape, so the page has no counting logic of its own to get out of step
 * with the database.
 */
export function DashboardPage() {
    const { data, error, loading, refetch } = useAsync(
        (signal) => api.dashboard(signal),
        [],
    );

    if (loading) return <DashboardSkeleton />;

    if (error) {
        return (
            <>
                <PageHeader title="Dashboard" />
                <ErrorState message={error.message} onRetry={refetch} />
            </>
        );
    }

    if (!data) return null;

    return (
        <>
            <PageHeader
                title="Dashboard"
                subtitle="Where every lead stands right now."
            />

            <div className="space-y-6">
                <StatGrid summary={data} />

                <div className="grid gap-6 lg:grid-cols-5">
                    <Card className="lg:col-span-3">
                        <CardHeader
                            title="Pipeline by stage"
                            subtitle="Every lead, grouped by where it sits in the lifecycle."
                        />
                        <StageChart stages={data.by_stage} total={data.total_leads} />
                    </Card>

                    <Card className="lg:col-span-2">
                        <CardHeader
                            title="Recently added"
                            subtitle="The five newest leads."
                            action={
                                <Link
                                    to="/leads"
                                    className="inline-flex items-center gap-1 text-xs font-medium text-brand-600 hover:underline dark:text-brand-400"
                                >
                                    View all
                                    <ArrowUpRight className="size-3.5" aria-hidden="true" />
                                </Link>
                            }
                        />
                        <RecentLeads leads={data.recent_leads} />
                    </Card>
                </div>
            </div>
        </>
    );
}

// ---------------------------------------------------------------------------
// Headline numbers
// ---------------------------------------------------------------------------

function StatGrid({ summary }: { summary: DashboardSummary }) {
    const closed = summary.won_count + summary.lost_count;

    return (
        <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
            <StatCard
                icon={Users}
                label="Total leads"
                value={summary.total_leads}
                caption={`${summary.open_count} still open`}
                accent="text-brand-600 dark:text-brand-400"
                accentBg="bg-brand-50 dark:bg-brand-950/60"
            />
            <StatCard
                icon={Percent}
                label="Conversion rate"
                value={`${summary.conversion_rate}%`}
                // Naming the denominator turns a bare percentage into something
                // you can actually judge: 50% of 4 closed deals is not 50% of 400.
                caption={
                    closed === 0
                        ? "No deals closed yet"
                        : `${summary.won_count} won of ${closed} closed`
                }
                accent="text-emerald-600 dark:text-emerald-400"
                accentBg="bg-emerald-50 dark:bg-emerald-950/60"
            />
            <StatCard
                icon={TrendingUp}
                label="Won"
                value={summary.won_count}
                caption={`${summary.lost_count} lost`}
                accent="text-violet-600 dark:text-violet-400"
                accentBg="bg-violet-50 dark:bg-violet-950/60"
            />
            <StatCard
                icon={Clock}
                label="Added this week"
                value={summary.added_last_7_days}
                caption="In the last 7 days"
                accent="text-amber-600 dark:text-amber-400"
                accentBg="bg-amber-50 dark:bg-amber-950/60"
            />
        </div>
    );
}

function StatCard({
    icon: Icon,
    label,
    value,
    caption,
    accent,
    accentBg,
}: {
    icon: LucideIcon;
    label: string;
    value: string | number;
    caption: string;
    accent: string;
    accentBg: string;
}) {
    return (
        <Card className="p-5">
            <div className="flex items-start justify-between gap-3">
                <div className="min-w-0">
                    <p className="text-xs font-medium text-slate-500 dark:text-slate-400">
                        {label}
                    </p>
                    {/* .tabular keeps digits monospaced so the numbers do not
                        shuffle sideways when they change. */}
                    <p className="tabular mt-2 text-3xl font-semibold tracking-tight text-slate-900 dark:text-slate-50">
                        {value}
                    </p>
                    <p className="mt-1 truncate text-xs text-slate-400 dark:text-slate-500">
                        {caption}
                    </p>
                </div>
                <div
                    className={cx(
                        "flex size-9 shrink-0 items-center justify-center rounded-xl",
                        accentBg,
                        accent,
                    )}
                >
                    <Icon className="size-4.5" aria-hidden="true" />
                </div>
            </div>
        </Card>
    );
}

// ---------------------------------------------------------------------------
// Bar chart
// ---------------------------------------------------------------------------

/**
 * The stage breakdown, drawn as horizontal bars.
 *
 * Hand-built from divs rather than pulled in as a charting library: it is seven
 * bars, and a dependency for that would add tens of kilobytes to the bundle while
 * making the stage colours harder to keep consistent with the rest of the app.
 *
 * Bars are scaled against the largest count, not the total, so the shape stays
 * readable when one stage dominates.
 */
function StageChart({ stages, total }: { stages: StageCount[]; total: number }) {
    if (total === 0) {
        return (
            <EmptyState
                icon={Users}
                title="No leads yet"
                description="Add your first lead and this chart will fill in."
            />
        );
    }

    const largest = Math.max(...stages.map((entry) => entry.count), 1);

    return (
        <div className="space-y-3 px-5 py-5">
            {stages.map((entry) => {
                const style = STAGE_STYLES[entry.stage];
                const widthPercent = (entry.count / largest) * 100;
                const shareOfTotal = total > 0 ? Math.round((entry.count / total) * 100) : 0;

                return (
                    <Link
                        key={entry.stage}
                        // Each bar doubles as a filter link into the leads table,
                        // which is the obvious next question after "12 are in
                        // Negotiation": which twelve?
                        to={`/leads?stage=${entry.stage}`}
                        className="group flex items-center gap-3 rounded-lg py-0.5 transition-colors"
                        title={`View the ${entry.count} ${style.label} leads`}
                    >
                        <span className="w-24 shrink-0 text-right text-xs font-medium text-slate-600 group-hover:text-slate-900 dark:text-slate-400 dark:group-hover:text-slate-200">
                            {entry.label}
                        </span>

                        <span className="relative h-6 flex-1 overflow-hidden rounded-md bg-slate-100 dark:bg-slate-800">
                            <span
                                className={cx(
                                    "absolute inset-y-0 left-0 rounded-md transition-all duration-500 ease-out group-hover:brightness-110",
                                    style.solid,
                                )}
                                // An inline style is the right tool here: the width
                                // is data-driven, and Tailwind can only generate
                                // classes it can see at build time.
                                style={{ width: `${Math.max(widthPercent, entry.count > 0 ? 3 : 0)}%` }}
                            />
                        </span>

                        <span className="tabular w-14 shrink-0 text-xs text-slate-500 dark:text-slate-400">
                            <span className="font-semibold text-slate-900 dark:text-slate-100">
                                {entry.count}
                            </span>
                            <span className="ml-1 text-slate-400 dark:text-slate-500">
                                {shareOfTotal}%
                            </span>
                        </span>
                    </Link>
                );
            })}
        </div>
    );
}

// ---------------------------------------------------------------------------
// Recent leads
// ---------------------------------------------------------------------------

function RecentLeads({ leads }: { leads: DashboardSummary["recent_leads"] }) {
    if (leads.length === 0) {
        return (
            <EmptyState
                icon={Users}
                title="Nothing yet"
                description="New leads will appear here as they come in."
            />
        );
    }

    return (
        <ul className="divide-y divide-slate-100 dark:divide-slate-800">
            {leads.map((lead) => (
                <li key={lead.id}>
                    <Link
                        to={`/leads/${lead.id}`}
                        className="flex items-center gap-3 px-5 py-3 transition-colors hover:bg-slate-50 dark:hover:bg-slate-800/50"
                    >
                        <span
                            className={cx(
                                "flex size-9 shrink-0 items-center justify-center rounded-full text-xs font-semibold",
                                avatarColor(lead.name),
                            )}
                            aria-hidden="true"
                        >
                            {initials(lead.name)}
                        </span>

                        <span className="min-w-0 flex-1">
                            <span className="block truncate text-sm font-medium text-slate-900 dark:text-slate-100">
                                {lead.name}
                            </span>
                            <span className="block truncate text-xs text-slate-500 dark:text-slate-400">
                                {lead.company}
                            </span>
                        </span>

                        <span className="flex shrink-0 flex-col items-end gap-1">
                            <StageBadge stage={lead.stage} showIcon={false} />
                            <span className="text-[11px] text-slate-400 dark:text-slate-500">
                                {formatRelative(lead.created_at)}
                            </span>
                        </span>
                    </Link>
                </li>
            ))}
        </ul>
    );
}

// ---------------------------------------------------------------------------
// Loading state
// ---------------------------------------------------------------------------

/**
 * A skeleton shaped like the real dashboard.
 *
 * Mirroring the final layout means nothing jumps when the data lands -- the
 * cards are already the right size in the right places.
 */
function DashboardSkeleton() {
    return (
        <>
            <PageHeader title="Dashboard" subtitle="Where every lead stands right now." />

            <div className="space-y-6">
                <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
                    {Array.from({ length: 4 }).map((_, index) => (
                        <Card key={index} className="p-5">
                            <Skeleton className="h-3 w-20" />
                            <Skeleton className="mt-3 h-8 w-16" />
                            <Skeleton className="mt-2 h-3 w-24" />
                        </Card>
                    ))}
                </div>

                <div className="grid gap-6 lg:grid-cols-5">
                    <Card className="lg:col-span-3">
                        <div className="border-b border-slate-200 px-5 py-4 dark:border-slate-800">
                            <Skeleton className="h-4 w-32" />
                        </div>
                        <div className="space-y-3 px-5 py-5">
                            {Array.from({ length: 7 }).map((_, index) => (
                                <div key={index} className="flex items-center gap-3">
                                    <Skeleton className="h-3 w-24 shrink-0" />
                                    <Skeleton className="h-6 flex-1" />
                                </div>
                            ))}
                        </div>
                    </Card>

                    <Card className="lg:col-span-2">
                        <div className="border-b border-slate-200 px-5 py-4 dark:border-slate-800">
                            <Skeleton className="h-4 w-28" />
                        </div>
                        <div className="divide-y divide-slate-100 dark:divide-slate-800">
                            {Array.from({ length: 5 }).map((_, index) => (
                                <div key={index} className="flex items-center gap-3 px-5 py-3">
                                    <Skeleton className="size-9 shrink-0 rounded-full" />
                                    <div className="flex-1 space-y-1.5">
                                        <Skeleton className="h-3.5 w-32" />
                                        <Skeleton className="h-3 w-24" />
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
