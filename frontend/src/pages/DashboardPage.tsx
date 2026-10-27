import { useState } from "react";
import { Link } from "react-router-dom";
import {
    AlertTriangle,
    ArrowUpRight,
    CalendarDays,
    Check,
    CircleCheck,
    Flame,
    Clock,
    Hourglass,
    IndianRupee,
    Percent,
    Snowflake,
    TrendingUp,
    Users,
} from "lucide-react";
import type { LucideIcon } from "lucide-react";

import { ApiError, api } from "../lib/api";
import {
    BAND_STYLES,
    DEFAULT_TASK_DURATION,
    STAGE_STYLES,
    avatarColor,
    cx,
    daysUntil,
    formatClock,
    formatDue,
    formatDuration,
    minutesOfDay,
    formatMoney,
    formatMoneyCompact,
    formatRelative,
    formatSilence,
    initials,
    isClosedStage,
} from "../lib/display";
import type { DashboardSummary, StageCount, Task } from "../lib/types";
import { useAsync } from "../hooks/useAsync";
import { useToast } from "../context/toast";
import { StageBadge } from "../components/StageBadge";
import { TaskTypeIcon } from "../components/TaskTypePicker";
import { HealthBadge, ScorePill, StuckBadge } from "../components/LeadInsights";
import {
    Card,
    CardHeader,
    EmptyState,
    ErrorState,
    PageHeader,
    RefreshError,
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

    // Only a failed first load replaces the page; a failed refresh (say, after
    // ticking off a task) keeps the numbers already shown, with a notice.
    if (error && !data) {
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
                {error && <RefreshError message={error.message} onRetry={refetch} />}

                <StatGrid summary={data} />

                <Card>
                    <CardHeader
                        title="Today's agenda"
                        subtitle={agendaSubtitle(data.today_tasks)}
                        action={
                            <Link
                                to="/schedule"
                                className="inline-flex items-center gap-1 text-xs font-medium text-brand-600 hover:underline dark:text-brand-400"
                            >
                                Open schedule
                                <ArrowUpRight className="size-3.5" aria-hidden="true" />
                            </Link>
                        }
                    />
                    <TodayAgenda tasks={data.today_tasks} onChanged={refetch} />
                </Card>

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

                <Card>
                    <CardHeader
                        title="Hottest leads"
                        subtitle="The five highest-scoring open leads: call these first."
                        action={
                            <Link
                                to="/leads?band=hot&sort_by=score&sort_dir=desc"
                                className={cx(
                                    "inline-flex shrink-0 items-center gap-1 rounded-full px-2.5 py-1 text-xs font-medium hover:underline",
                                    BAND_STYLES.hot.badge,
                                )}
                            >
                                <Flame className="size-3.5" aria-hidden="true" />
                                {data.hot_count} hot
                                <ArrowUpRight className="size-3.5" aria-hidden="true" />
                            </Link>
                        }
                    />
                    <HottestLeads leads={data.hottest_leads} />
                </Card>

                <div className="grid gap-6 xl:grid-cols-2">
                    <Card>
                        <CardHeader
                            title="Going cold"
                            subtitle="Open leads nobody has contacted for longer than their stage allows."
                            action={<HealthCounts summary={data} />}
                        />
                        <GoingCold leads={data.going_cold} />
                    </Card>

                    <Card>
                        <CardHeader
                            title="Stuck deals"
                            subtitle="Open deals sitting in one stage for longer than it allows."
                            action={
                                <Link
                                    to="/follow-ups?tab=stuck"
                                    className="inline-flex shrink-0 items-center gap-1 rounded-full bg-orange-50 px-2.5 py-1 text-xs font-medium text-orange-700 hover:underline dark:bg-orange-950/60 dark:text-orange-300"
                                >
                                    {data.stuck_count} stuck
                                    <ArrowUpRight className="size-3.5" aria-hidden="true" />
                                </Link>
                            }
                        />
                        <StuckDeals leads={data.stuck_deals} />
                    </Card>
                </div>

                <div className="grid gap-6 lg:grid-cols-5">
                    <Card className="lg:col-span-3">
                        <CardHeader
                            title="Forecast"
                            subtitle="Open deal value × the win probability of its stage."
                        />
                        <Forecast summary={data} />
                    </Card>

                    <Card className="lg:col-span-2">
                        <CardHeader
                            title="Follow-ups"
                            subtitle="Open tasks due today or earlier."
                        />
                        <FollowUps summary={data} onChanged={refetch} />
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
// Going cold
// ---------------------------------------------------------------------------

function HealthCounts({ summary }: { summary: DashboardSummary }) {
    return (
        <div className="flex shrink-0 gap-2 text-xs font-medium">
            <span className="rounded-full bg-amber-50 px-2.5 py-1 text-amber-700 dark:bg-amber-950/60 dark:text-amber-300">
                {summary.cooling_leads} cooling
            </span>
            <span className="rounded-full bg-sky-50 px-2.5 py-1 text-sky-700 dark:bg-sky-950/60 dark:text-sky-300">
                {summary.cold_leads} cold
            </span>
        </div>
    );
}

/**
 * The best bets: open leads by score, highest first. Where Going cold is who
 * to rescue, this is who to call while they are keen.
 */
function HottestLeads({ leads }: { leads: DashboardSummary["hottest_leads"] }) {
    if (leads.length === 0) {
        return (
            <EmptyState
                icon={Flame}
                title="No open leads to score"
                description="Add a lead, or reopen one, and the most promising ones will show up here."
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
                                {lead.company} · {formatSilence(lead.days_since_touch)}
                            </span>
                        </span>

                        <span className="hidden shrink-0 sm:block">
                            <StageBadge stage={lead.stage} showIcon={false} />
                        </span>
                        <span className="w-20 shrink-0 text-right">
                            <ScorePill score={lead.score} />
                        </span>
                    </Link>
                </li>
            ))}
        </ul>
    );
}

/**
 * The call list: open leads ranked by how far past their stage's limit they
 * are, not by raw days. Six silent days in Negotiation (cold at 7) is more
 * urgent than ten in Qualified (cold at 14), and this list says so.
 */
function GoingCold({ leads }: { leads: DashboardSummary["going_cold"] }) {
    if (leads.length === 0) {
        return (
            <EmptyState
                icon={Snowflake}
                title="Nothing going cold"
                description="Every open lead has been contacted recently enough for its stage."
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
                                {lead.company} · {formatSilence(lead.days_since_touch)}
                            </span>
                        </span>

                        <span className="hidden shrink-0 sm:block">
                            <StageBadge stage={lead.stage} showIcon={false} />
                        </span>
                        <HealthBadge lead={lead} />
                        <span className="w-20 shrink-0 text-right">
                            <ScorePill score={lead.score} />
                        </span>
                    </Link>
                </li>
            ))}
        </ul>
    );
}

/**
 * Deals that are not moving: ranked by how far past their stage's limit they
 * are, so 20 days in Negotiation (limit 14) comes before 12 in Proposal Sent
 * (limit 10).
 */
function StuckDeals({ leads }: { leads: DashboardSummary["stuck_deals"] }) {
    if (leads.length === 0) {
        return (
            <EmptyState
                icon={Hourglass}
                title="No stuck deals"
                description="Every open deal has moved stage recently enough."
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
                                {lead.company} · {lead.days_in_stage} days in {STAGE_STYLES[lead.stage].label}{" "}
                                (limit {lead.stuck_limit_days})
                            </span>
                        </span>

                        <span className="hidden shrink-0 sm:block">
                            <StageBadge stage={lead.stage} showIcon={false} />
                        </span>
                        <StuckBadge lead={lead} />
                    </Link>
                </li>
            ))}
        </ul>
    );
}

// ---------------------------------------------------------------------------
// Forecast
// ---------------------------------------------------------------------------

/**
 * The weighted forecast, with its working shown.
 *
 * Each open stage gets a row reading "value × probability = weighted", and the
 * rows add up to the headline. Nothing is hidden behind a model: anyone can check
 * the number with a calculator, and argue with a stage's probability if they
 * think it is wrong.
 */
function Forecast({ summary }: { summary: DashboardSummary }) {
    const openStages = summary.by_stage.filter((entry) => !isClosedStage(entry.stage));
    const unvalued = openStages.reduce((sum, entry) => sum + entry.unvalued_count, 0);

    if (summary.open_count === 0 && summary.won_value === 0) {
        return (
            <EmptyState
                icon={IndianRupee}
                title="No open deals"
                description="Give a lead a deal value and it will be forecast here."
            />
        );
    }

    return (
        <div className="px-5 py-5">
            <dl className="grid grid-cols-3 gap-4">
                <ForecastFigure label="Open pipeline" value={summary.pipeline_value} />
                <ForecastFigure
                    label="Weighted forecast"
                    value={summary.weighted_forecast}
                    emphasis
                />
                <ForecastFigure label="Won so far" value={summary.won_value} />
            </dl>

            <table className="mt-5 w-full text-xs">
                <thead>
                    <tr className="text-left text-slate-400 dark:text-slate-500">
                        <th className="pb-2 font-medium">Stage</th>
                        <th className="pb-2 text-right font-medium">Value</th>
                        <th className="pb-2 text-right font-medium">Chance</th>
                        <th className="pb-2 text-right font-medium">Weighted</th>
                    </tr>
                </thead>
                <tbody className="tabular divide-y divide-slate-100 dark:divide-slate-800">
                    {openStages.map((entry) => (
                        <tr key={entry.stage}>
                            <td className="py-2">
                                <span className="inline-flex items-center gap-2 text-slate-700 dark:text-slate-300">
                                    <span
                                        className={cx("size-2 rounded-full", STAGE_STYLES[entry.stage].solid)}
                                        aria-hidden="true"
                                    />
                                    {entry.label}
                                </span>
                            </td>
                            <td className="py-2 text-right text-slate-600 dark:text-slate-300">
                                {formatMoney(entry.value)}
                            </td>
                            <td className="py-2 text-right text-slate-400 dark:text-slate-500">
                                × {entry.probability}%
                            </td>
                            <td className="py-2 text-right font-semibold text-slate-900 dark:text-slate-100">
                                {formatMoney(entry.weighted)}
                            </td>
                        </tr>
                    ))}
                </tbody>
            </table>

            {/* Unvalued leads count for nothing in the forecast. Saying so beats
                a forecast that silently looks smaller than the real pipeline. */}
            {unvalued > 0 && (
                <p className="mt-4 flex items-center gap-1.5 text-xs text-amber-700 dark:text-amber-400">
                    <AlertTriangle className="size-3.5 shrink-0" aria-hidden="true" />
                    {unvalued} open {unvalued === 1 ? "lead has" : "leads have"} no deal value
                    and {unvalued === 1 ? "is" : "are"} not in the forecast.
                </p>
            )}
        </div>
    );
}

function ForecastFigure({
    label,
    value,
    emphasis = false,
}: {
    label: string;
    value: number;
    emphasis?: boolean;
}) {
    return (
        <div className="min-w-0">
            <dt className="text-xs font-medium text-slate-500 dark:text-slate-400">{label}</dt>
            <dd
                className={cx(
                    "tabular mt-1 truncate text-xl font-semibold tracking-tight",
                    emphasis
                        ? "text-brand-600 dark:text-brand-400"
                        : "text-slate-900 dark:text-slate-50",
                )}
                title={formatMoney(value)}
            >
                {formatMoneyCompact(value)}
            </dd>
        </div>
    );
}

// ---------------------------------------------------------------------------
// Follow-ups
// ---------------------------------------------------------------------------

/**
 * Today's to-do list across every lead, plus the leads nobody has planned a
 * next step for -- the gap a task list alone cannot show.
 */
function FollowUps({
    summary,
    onChanged,
}: {
    summary: DashboardSummary;
    onChanged: () => void;
}) {
    const toast = useToast();
    const [busyId, setBusyId] = useState<number | null>(null);

    const complete = async (task: Task) => {
        setBusyId(task.id);
        try {
            await api.setTaskCompleted(task.id, true);
            toast.success(`Done: ${task.title}`);
            // The counts, the list and "leads with no next step" can all change,
            // so the summary is re-read rather than patched by hand.
            onChanged();
        } catch (cause) {
            toast.error(cause instanceof ApiError ? cause.message : "Could not complete the task.");
        } finally {
            setBusyId(null);
        }
    };

    const hidden =
        summary.overdue_tasks + summary.due_today_tasks - summary.due_tasks.length;

    return (
        <div>
            <div className="flex gap-2 px-5 pt-4 text-xs font-medium">
                <span
                    className={cx(
                        "rounded-full px-2.5 py-1",
                        summary.overdue_tasks > 0
                            ? "bg-rose-50 text-rose-700 dark:bg-rose-950/60 dark:text-rose-300"
                            : "bg-slate-100 text-slate-500 dark:bg-slate-800 dark:text-slate-400",
                    )}
                >
                    {summary.overdue_tasks} overdue
                </span>
                <span
                    className={cx(
                        "rounded-full px-2.5 py-1",
                        summary.due_today_tasks > 0
                            ? "bg-amber-50 text-amber-700 dark:bg-amber-950/60 dark:text-amber-300"
                            : "bg-slate-100 text-slate-500 dark:bg-slate-800 dark:text-slate-400",
                    )}
                >
                    {summary.due_today_tasks} due today
                </span>
            </div>

            {summary.due_tasks.length === 0 ? (
                <EmptyState
                    icon={CircleCheck}
                    title="All caught up"
                    description="Nothing is due today or overdue."
                />
            ) : (
                <ul className="mt-2 divide-y divide-slate-100 dark:divide-slate-800">
                    {summary.due_tasks.map((task) => (
                        <li
                            key={task.id}
                            className={cx(
                                "flex items-start gap-3 px-5 py-3",
                                busyId === task.id && "opacity-60",
                            )}
                        >
                            <button
                                type="button"
                                role="checkbox"
                                aria-checked={false}
                                aria-label={`Complete "${task.title}"`}
                                disabled={busyId !== null}
                                onClick={() => complete(task)}
                                className="group mt-0.5 flex size-5 shrink-0 items-center justify-center rounded-md border border-slate-300 transition-colors hover:border-teal-500 dark:border-slate-600"
                            >
                                <Check
                                    className="size-3.5 text-teal-500 opacity-0 group-hover:opacity-100"
                                    aria-hidden="true"
                                />
                            </button>
                            <Link to={`/leads/${task.lead_id}`} className="group/link min-w-0 flex-1">
                                <span className="block truncate text-sm text-slate-900 group-hover/link:underline dark:text-slate-100">
                                    <TaskTypeIcon type={task.type} className="mr-1 size-3.5 align-[-2px]" />
                                    {task.title}
                                </span>
                                <span className="block truncate text-xs text-slate-500 dark:text-slate-400">
                                    {task.lead_name} · {task.lead_company}
                                </span>
                            </Link>
                            <span
                                className={cx(
                                    "shrink-0 text-[11px] font-medium",
                                    daysUntil(task.due_date) < 0
                                        ? "text-rose-600 dark:text-rose-400"
                                        : "text-amber-600 dark:text-amber-400",
                                )}
                            >
                                {formatDue(task.due_date)}
                                {task.start_time && ` · ${formatClock(task.start_time)}`}
                            </span>
                        </li>
                    ))}
                </ul>
            )}

            {hidden > 0 && (
                <p className="px-5 pb-3 text-xs text-slate-400 dark:text-slate-500">
                    +{hidden} more - open the leads to see them.
                </p>
            )}

            {summary.open_leads_without_task > 0 && (
                <Link
                    to="/leads"
                    className="mx-5 mt-2 mb-4 flex items-start gap-2 rounded-lg bg-slate-50 px-3 py-2.5 text-xs text-slate-600 transition-colors hover:bg-slate-100 dark:bg-slate-800/50 dark:text-slate-300 dark:hover:bg-slate-800"
                >
                    <AlertTriangle
                        className="mt-0.5 size-3.5 shrink-0 text-amber-500"
                        aria-hidden="true"
                    />
                    <span>
                        <span className="font-semibold">{summary.open_leads_without_task}</span>{" "}
                        open {summary.open_leads_without_task === 1 ? "lead has" : "leads have"} no
                        next step planned.
                    </span>
                </Link>
            )}
        </div>
    );
}

// ---------------------------------------------------------------------------
// Today's agenda
// ---------------------------------------------------------------------------

function agendaSubtitle(tasks: Task[]): string {
    if (tasks.length === 0) return "Nothing due today.";
    const done = tasks.filter((task) => task.completed_at).length;
    return `${done} of ${tasks.length} done`;
}

function minutesNow(): number {
    const now = new Date();
    return now.getHours() * 60 + now.getMinutes();
}

/**
 * The open timed task happening now, or else the next one to start -- the one
 * the agenda points out. All-day tasks have no "now", so they are never it.
 */
function currentOrNext(tasks: Task[], now: number): { id: number; label: "Now" | "Next" } | null {
    for (const task of tasks) {
        if (task.completed_at || !task.start_time) continue;
        const start = minutesOfDay(task.start_time);
        const end = start + (task.duration_minutes ?? DEFAULT_TASK_DURATION);
        if (now < end) return { id: task.id, label: now >= start ? "Now" : "Next" };
    }
    return null;
}

/**
 * The day in order: timed tasks by start time, then the all-day ones, with the
 * one happening now (or next) called out. Done tasks stay, struck through, so
 * the list shows progress through the day; ticking one reopens it.
 */
function TodayAgenda({ tasks, onChanged }: { tasks: Task[]; onChanged: () => void }) {
    const toast = useToast();
    const [busyId, setBusyId] = useState<number | null>(null);

    const toggle = async (task: Task) => {
        const completing = !task.completed_at;
        setBusyId(task.id);
        try {
            await api.setTaskCompleted(task.id, completing);
            toast.success(completing ? `Done: ${task.title}` : `Reopened "${task.title}".`);
            // Follow-ups, counts and lead health can all change too.
            onChanged();
        } catch (cause) {
            toast.error(cause instanceof ApiError ? cause.message : "Could not update the task.");
        } finally {
            setBusyId(null);
        }
    };

    if (tasks.length === 0) {
        return (
            <EmptyState
                icon={CalendarDays}
                title="A clear day"
                description="Nothing is scheduled for today. Book follow-ups from the Schedule page or any lead."
            />
        );
    }

    const highlight = currentOrNext(tasks, minutesNow());

    return (
        <ul className="divide-y divide-slate-100 dark:divide-slate-800">
            {tasks.map((task) => {
                const done = Boolean(task.completed_at);
                const flagged = highlight?.id === task.id;

                return (
                    <li
                        key={task.id}
                        className={cx(
                            "flex items-start gap-3 px-5 py-3",
                            flagged && "bg-brand-50/60 dark:bg-brand-950/30",
                            busyId === task.id && "opacity-60",
                        )}
                    >
                        {/* Time column: start and length, or "All day". */}
                        <div className="w-20 shrink-0 text-xs">
                            {task.start_time ? (
                                <>
                                    <p className={cx("font-semibold", done ? "text-slate-400 dark:text-slate-500" : "text-slate-900 dark:text-slate-100")}>
                                        {formatClock(task.start_time)}
                                    </p>
                                    <p className="text-slate-400 dark:text-slate-500">
                                        {formatDuration(task.duration_minutes ?? DEFAULT_TASK_DURATION)}
                                    </p>
                                </>
                            ) : (
                                <p className="font-medium text-slate-500 dark:text-slate-400">All day</p>
                            )}
                        </div>

                        <button
                            type="button"
                            role="checkbox"
                            aria-checked={done}
                            aria-label={done ? `Reopen "${task.title}"` : `Complete "${task.title}"`}
                            disabled={busyId !== null}
                            onClick={() => toggle(task)}
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

                        <Link to={`/leads/${task.lead_id}`} className="group/link min-w-0 flex-1">
                            <span
                                className={cx(
                                    "block truncate text-sm group-hover/link:underline",
                                    done
                                        ? "text-slate-400 line-through dark:text-slate-500"
                                        : "text-slate-900 dark:text-slate-100",
                                )}
                            >
                                <TaskTypeIcon type={task.type} className="mr-1 size-3.5 align-[-2px]" />
                                {task.title}
                            </span>
                            <span className="block truncate text-xs text-slate-500 dark:text-slate-400">
                                {task.lead_name} · {task.lead_company}
                            </span>
                        </Link>

                        {flagged && highlight && (
                            <span className="shrink-0 rounded-full bg-brand-600 px-2 py-0.5 text-[11px] font-semibold text-white dark:bg-brand-500">
                                {highlight.label}
                            </span>
                        )}
                    </li>
                );
            })}
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
