import { ChartColumn, Lightbulb } from "lucide-react";

import { api } from "../lib/api";
import { STAGE_STYLES, SOURCE_STYLES, cx, formatMoney, formatMoneyCompact } from "../lib/display";
import type { ReasonCount, SourceReport, StageDuration, WeekCount } from "../lib/types";
import { useAsync } from "../hooks/useAsync";
import { Card, CardHeader, EmptyState, ErrorState, PageHeader, Skeleton } from "../components/ui";

/**
 * Trends over time: where leads come from, which sources close, and where
 * deals slow down.
 *
 * Every chart is a single series, so each uses the one brand colour -- the
 * labels say what a bar is, colour does not have to. Each panel ends with a
 * plain-English takeaway, worked out from the same numbers the chart shows,
 * because a chart people have to interpret is a chart most people skip.
 */
export function ReportsPage() {
    const { data, error, loading, refetch } = useAsync((signal) => api.reports(signal), []);

    if (loading) return <ReportsSkeleton />;

    if (error || !data) {
        return (
            <>
                <PageHeader title="Reports" />
                <ErrorState message={error?.message ?? "Could not load reports."} onRetry={refetch} />
            </>
        );
    }

    return (
        <>
            <PageHeader title="Reports" subtitle="Trends across every lead, worked out from the timeline." />

            <div className="space-y-6">
                <Card>
                    <CardHeader title="New leads per week" subtitle="The last 12 weeks, Monday to Sunday." />
                    <WeeklyChart weeks={data.leads_per_week} />
                    <Takeaway text={weeklyTakeaway(data.leads_per_week)} />
                </Card>

                <div className="grid gap-6 lg:grid-cols-2">
                    <Card>
                        <CardHeader
                            title="Win rate by source"
                            subtitle="Won ÷ (won + lost). Open leads are not counted until they close."
                        />
                        <SourceTable sources={data.by_source} />
                        <Takeaway text={sourceTakeaway(data.by_source)} />
                    </Card>

                    <Card>
                        <CardHeader
                            title="Time in each stage"
                            subtitle="Average days before a lead moves on. Leads still in a stage are not counted yet."
                        />
                        <DurationChart durations={data.stage_durations} />
                        <Takeaway text={durationTakeaway(data.stage_durations)} />
                    </Card>
                </div>

                <div className="grid gap-6 lg:grid-cols-2">
                    <Card>
                        <CardHeader
                            title="Why deals are lost"
                            subtitle="Every lead now in Lost, by the reason given. Bars count deals."
                        />
                        <ReasonChart reasons={data.loss_reasons} outcome="lost" />
                        <Takeaway text={reasonTakeaway(data.loss_reasons, "lost")} />
                    </Card>

                    <Card>
                        <CardHeader
                            title="Why deals are won"
                            subtitle="Every lead now in Won, by the reason given. Giving one is optional."
                        />
                        <ReasonChart reasons={data.win_reasons} outcome="won" />
                        <Takeaway text={reasonTakeaway(data.win_reasons, "won")} />
                    </Card>
                </div>
            </div>
        </>
    );
}

// ---------------------------------------------------------------------------
// Shared pieces
// ---------------------------------------------------------------------------

/** The one-line conclusion at the foot of each panel. */
function Takeaway({ text }: { text: string }) {
    return (
        <p className="flex items-start gap-2 border-t border-slate-200 px-5 py-3 text-sm text-slate-700 dark:border-slate-800 dark:text-slate-300">
            <Lightbulb className="mt-0.5 size-4 shrink-0 text-amber-500" aria-hidden="true" />
            {text}
        </p>
    );
}

/**
 * A hover/focus tooltip for one bar. Shown on the bar's group hover, and on
 * keyboard focus, so it is not mouse-only.
 */
function BarTooltip({ children, align = "center" }: { children: React.ReactNode; align?: "center" | "end" }) {
    return (
        <span
            role="tooltip"
            className={cx(
                "pointer-events-none absolute bottom-full z-10 mb-2 rounded-md bg-slate-900 px-2 py-1 text-xs whitespace-nowrap text-white opacity-0 shadow-lg transition-opacity group-hover:opacity-100 group-focus-visible:opacity-100 dark:bg-slate-100 dark:text-slate-900",
                align === "center" ? "left-1/2 -translate-x-1/2" : "right-0",
            )}
        >
            {children}
        </span>
    );
}

/** "20 Jul" from "2026-07-20", read as a local calendar date. */
function shortDate(isoDate: string): string {
    const [year, month, day] = isoDate.split("-").map(Number);
    return new Date(year, month - 1, day).toLocaleDateString(undefined, { day: "numeric", month: "short" });
}

const plural = (n: number, one: string, many: string) => `${n} ${n === 1 ? one : many}`;

// ---------------------------------------------------------------------------
// New leads per week
// ---------------------------------------------------------------------------

function WeeklyChart({ weeks }: { weeks: WeekCount[] }) {
    const largest = Math.max(...weeks.map((week) => week.count), 1);

    return (
        <div className="px-5 pt-6 pb-4">
            <div className="flex">
                {/* A single scale reference instead of a full gridded axis. */}
                <div className="tabular flex h-40 w-6 shrink-0 flex-col justify-between pr-1 text-right text-[10px] text-slate-400 dark:text-slate-500">
                    <span>{largest}</span>
                    <span>0</span>
                </div>

                <ol
                    className="flex h-40 flex-1 items-end gap-0.5 border-b border-slate-200 dark:border-slate-700"
                    aria-label="New leads per week"
                >
                    {weeks.map((week, index) => {
                        const isThisWeek = index === weeks.length - 1;
                        const height = (week.count / largest) * 100;
                        return (
                            <li key={week.week_start} className="flex h-full flex-1 items-end justify-center">
                                <span
                                    tabIndex={0}
                                    className="group relative flex h-full w-full max-w-10 items-end justify-center outline-none"
                                    aria-label={`Week of ${shortDate(week.week_start)}: ${plural(week.count, "new lead", "new leads")}`}
                                >
                                    <BarTooltip>
                                        Week of {shortDate(week.week_start)}: {plural(week.count, "lead", "leads")}
                                    </BarTooltip>
                                    {/* Only this week's value is printed; the rest are on hover. */}
                                    {isThisWeek && week.count > 0 && (
                                        <span
                                            className="tabular absolute text-xs font-semibold text-slate-900 dark:text-slate-100"
                                            style={{ bottom: `calc(${height}% + 4px)` }}
                                        >
                                            {week.count}
                                        </span>
                                    )}
                                    <span
                                        className={cx(
                                            "w-full rounded-t-[4px] transition-all duration-500",
                                            week.count > 0
                                                ? "bg-brand-500 group-hover:bg-brand-600 dark:bg-brand-400 dark:group-hover:bg-brand-300"
                                                : "",
                                        )}
                                        style={{ height: week.count > 0 ? `max(${height}%, 3px)` : 0 }}
                                    />
                                </span>
                            </li>
                        );
                    })}
                </ol>
            </div>

            <div className="mt-1.5 ml-6 flex gap-0.5 text-[10px] text-slate-400 dark:text-slate-500" aria-hidden="true">
                {weeks.map((week, index) => (
                    <span key={week.week_start} className="flex-1 truncate text-center">
                        {index === weeks.length - 1 ? "Now" : index % 3 === 0 ? shortDate(week.week_start) : ""}
                    </span>
                ))}
            </div>

            <details className="mt-3 text-xs text-slate-500 dark:text-slate-400">
                <summary className="cursor-pointer select-none hover:text-slate-700 dark:hover:text-slate-200">
                    Show as table
                </summary>
                <table className="mt-2 w-full max-w-xs">
                    <thead>
                        <tr className="text-left">
                            <th className="py-1 font-medium">Week of</th>
                            <th className="py-1 text-right font-medium">New leads</th>
                        </tr>
                    </thead>
                    <tbody className="tabular">
                        {weeks.map((week) => (
                            <tr key={week.week_start}>
                                <td className="py-0.5">{shortDate(week.week_start)}</td>
                                <td className="py-0.5 text-right text-slate-900 dark:text-slate-100">{week.count}</td>
                            </tr>
                        ))}
                    </tbody>
                </table>
            </details>
        </div>
    );
}

function weeklyTakeaway(weeks: WeekCount[]): string {
    const total = weeks.reduce((sum, week) => sum + week.count, 0);
    if (total === 0) return "No new leads in the last 12 weeks.";

    const thisWeek = weeks[weeks.length - 1]?.count ?? 0;
    const lastWeek = weeks[weeks.length - 2]?.count ?? 0;
    const average = total / weeks.length;

    const trend =
        thisWeek > lastWeek
            ? `up from ${lastWeek} last week`
            : thisWeek < lastWeek
              ? `down from ${lastWeek} last week`
              : "the same as last week";

    return `${plural(thisWeek, "new lead", "new leads")} this week, ${trend}. That is ${
        thisWeek >= average ? "at or above" : "below"
    } the 12-week average of ${average.toFixed(1)} a week.`;
}

// ---------------------------------------------------------------------------
// Win rate by source
// ---------------------------------------------------------------------------

function SourceTable({ sources }: { sources: SourceReport[] }) {
    if (sources.every((source) => source.leads === 0)) {
        return <EmptyState icon={ChartColumn} title="No leads yet" description="Sources appear once leads arrive." />;
    }

    return (
        <table className="w-full text-sm">
            <thead>
                <tr className="text-left text-xs text-slate-500 dark:text-slate-400">
                    <th className="px-5 py-2.5 font-medium">Source</th>
                    <th className="px-2 py-2.5 text-right font-medium">Leads</th>
                    <th className="px-2 py-2.5 text-right font-medium">Won / lost</th>
                    <th className="w-2/5 px-5 py-2.5 font-medium">Win rate</th>
                </tr>
            </thead>
            <tbody className="tabular divide-y divide-slate-100 dark:divide-slate-800">
                {sources.map((source) => {
                    const Icon = SOURCE_STYLES[source.source].icon;
                    const closed = source.won + source.lost;
                    return (
                        <tr key={source.source}>
                            <td className="px-5 py-2.5">
                                <span className="inline-flex items-center gap-2 text-slate-700 dark:text-slate-300">
                                    <Icon className="size-3.5 opacity-60" aria-hidden="true" />
                                    {source.label}
                                </span>
                            </td>
                            <td className="px-2 py-2.5 text-right text-slate-900 dark:text-slate-100">{source.leads}</td>
                            <td className="px-2 py-2.5 text-right text-slate-500 dark:text-slate-400">
                                {source.won} / {source.lost}
                            </td>
                            <td className="px-5 py-2.5">
                                {source.win_rate === null ? (
                                    <span className="text-xs text-slate-400 dark:text-slate-500">
                                        {source.leads === 0 ? "—" : "None closed yet"}
                                    </span>
                                ) : (
                                    <span
                                        tabIndex={0}
                                        className="group relative flex items-center gap-2 outline-none"
                                        aria-label={`${source.label}: ${source.win_rate}% win rate over ${plural(closed, "closed deal", "closed deals")}`}
                                    >
                                        <BarTooltip align="end">
                                            {source.won} won of {closed} closed
                                            {source.won_value > 0 && ` · ${formatMoney(source.won_value)} won`}
                                        </BarTooltip>
                                        <span className="h-2 flex-1 overflow-hidden rounded-r-[4px] bg-slate-100 dark:bg-slate-800">
                                            <span
                                                className="block h-full rounded-r-[4px] bg-brand-500 group-hover:bg-brand-600 dark:bg-brand-400"
                                                style={{ width: `${Math.max(source.win_rate, 2)}%` }}
                                            />
                                        </span>
                                        <span className="w-12 text-right text-xs font-semibold text-slate-900 dark:text-slate-100">
                                            {source.win_rate}%
                                        </span>
                                    </span>
                                )}
                            </td>
                        </tr>
                    );
                })}
            </tbody>
        </table>
    );
}

function sourceTakeaway(sources: SourceReport[]): string {
    const withLeads = sources.filter((source) => source.leads > 0);
    if (withLeads.length === 0) return "No leads yet, so there is nothing to compare.";

    const biggest = [...withLeads].sort((a, b) => b.leads - a.leads)[0];
    const total = withLeads.reduce((sum, source) => sum + source.leads, 0);
    const volume = `${biggest.label} brings in the most leads (${biggest.leads} of ${total})`;

    const rated = sources.filter((source) => source.win_rate !== null);
    if (rated.length === 0) return `${volume}. Nothing has closed yet, so there is no win rate to compare.`;

    // Best rate first; more closed deals breaks a tie, because 1 of 1 is
    // weaker evidence than 5 of 5.
    const best = [...rated].sort(
        (a, b) => b.win_rate! - a.win_rate! || b.won + b.lost - (a.won + a.lost),
    )[0];
    const closed = best.won + best.lost;
    const caveat = closed < 5 ? ` — only ${plural(closed, "deal", "deals")} so far, so treat it as early` : "";

    return best.source === biggest.source
        ? `${volume}, and it also closes best: ${best.win_rate}% won${caveat}.`
        : `${volume}, but ${best.label} closes best: ${best.win_rate}% won${caveat}.`;
}

// ---------------------------------------------------------------------------
// Time in each stage
// ---------------------------------------------------------------------------

/** "4.2 days", "1 day", "< 1 day". */
function formatDays(days: number): string {
    if (days < 1) return "< 1 day";
    return days === 1 ? "1 day" : `${days} days`;
}

function DurationChart({ durations }: { durations: StageDuration[] }) {
    const measured = durations.filter((d) => d.avg_days !== null);
    if (measured.length === 0) {
        return (
            <EmptyState
                icon={ChartColumn}
                title="No stage moves yet"
                description="Once leads move between stages, the time they spent in each shows up here."
            />
        );
    }

    const largest = Math.max(...measured.map((d) => d.avg_days!), 0.1);

    return (
        <ul className="space-y-3 px-5 py-5">
            {durations.map((d) => (
                <li key={d.stage} className="flex items-center gap-3">
                    <span className="flex w-28 shrink-0 items-center gap-2 text-xs font-medium text-slate-600 dark:text-slate-400">
                        {/* The stage dot ties the row to the stage colour used
                            everywhere else; the bar itself stays one colour. */}
                        <span className={cx("size-2 rounded-full", STAGE_STYLES[d.stage].solid)} aria-hidden="true" />
                        {d.label}
                    </span>

                    {d.avg_days === null ? (
                        <span className="flex-1 text-xs text-slate-400 dark:text-slate-500">No lead has moved on yet</span>
                    ) : (
                        <span
                            tabIndex={0}
                            className="group relative flex flex-1 items-center gap-2 outline-none"
                            aria-label={`${d.label}: average ${formatDays(d.avg_days)} over ${plural(d.samples, "move", "moves")}`}
                        >
                            <BarTooltip>
                                Average {formatDays(d.avg_days)} · {plural(d.samples, "lead moved on", "leads moved on")}
                            </BarTooltip>
                            <span className="h-5 flex-1">
                                <span
                                    className="block h-full rounded-r-[4px] bg-brand-500 transition-all duration-500 group-hover:bg-brand-600 dark:bg-brand-400"
                                    style={{ width: `max(${(d.avg_days / largest) * 100}%, 3px)` }}
                                />
                            </span>
                            <span className="tabular w-24 shrink-0 text-right text-xs">
                                <span className="font-semibold text-slate-900 dark:text-slate-100">
                                    {formatDays(d.avg_days)}
                                </span>
                                <span className="text-slate-400 dark:text-slate-500"> · {d.samples}</span>
                            </span>
                        </span>
                    )}
                </li>
            ))}
        </ul>
    );
}

function durationTakeaway(durations: StageDuration[]): string {
    const measured = durations.filter((d) => d.avg_days !== null);
    if (measured.length === 0) return "No lead has moved between stages yet.";

    const slowest = [...measured].sort((a, b) => b.avg_days! - a.avg_days!)[0];
    if (slowest.avg_days! < 1) {
        return "Leads are moving through every stage in under a day so far.";
    }

    const caveat = slowest.samples < 3 ? ` (from only ${plural(slowest.samples, "move", "moves")} so far)` : "";
    return `Deals slow down most in ${slowest.label}: ${formatDays(slowest.avg_days!)} on average${caveat}. That is the stage to look at first.`;
}

// ---------------------------------------------------------------------------
// Win and loss reasons
// ---------------------------------------------------------------------------

function ReasonChart({ reasons, outcome }: { reasons: ReasonCount[]; outcome: "won" | "lost" }) {
    if (reasons.length === 0) {
        return (
            <EmptyState
                icon={ChartColumn}
                title={outcome === "lost" ? "No lost deals yet" : "No won deals yet"}
                description={`When a deal is marked ${outcome === "lost" ? "Lost" : "Won"}, its reason shows up here.`}
            />
        );
    }

    const largest = Math.max(...reasons.map((r) => r.count), 1);

    return (
        <ul className="space-y-3 px-5 py-5">
            {reasons.map((r) => (
                <li key={r.reason ?? "none"} className="flex items-center gap-3">
                    <span
                        className={cx(
                            "w-40 shrink-0 truncate text-xs font-medium",
                            r.reason === null
                                ? "text-slate-400 italic dark:text-slate-500"
                                : "text-slate-600 dark:text-slate-400",
                        )}
                        title={r.label}
                    >
                        {r.label}
                    </span>
                    <span
                        tabIndex={0}
                        className="group relative flex flex-1 items-center gap-2 outline-none"
                        aria-label={`${r.label}: ${plural(r.count, "deal", "deals")}${r.value > 0 ? `, ${formatMoney(r.value)}` : ""}`}
                    >
                        <BarTooltip>
                            {plural(r.count, "deal", "deals")}
                            {r.value > 0 && ` · ${formatMoney(r.value)} ${outcome}`}
                        </BarTooltip>
                        <span className="h-5 flex-1">
                            <span
                                className={cx(
                                    "block h-full rounded-r-[4px] transition-all duration-500",
                                    // Unrecorded is not a reason, so it does not get
                                    // the reason colour.
                                    r.reason === null
                                        ? "bg-slate-300 dark:bg-slate-600"
                                        : "bg-brand-500 group-hover:bg-brand-600 dark:bg-brand-400",
                                )}
                                style={{ width: `max(${(r.count / largest) * 100}%, 3px)` }}
                            />
                        </span>
                        <span className="tabular w-24 shrink-0 text-right text-xs">
                            <span className="font-semibold text-slate-900 dark:text-slate-100">{r.count}</span>
                            {r.value > 0 && (
                                <span className="text-slate-400 dark:text-slate-500"> · {formatMoneyCompact(r.value)}</span>
                            )}
                        </span>
                    </span>
                </li>
            ))}
        </ul>
    );
}

function reasonTakeaway(reasons: ReasonCount[], outcome: "won" | "lost"): string {
    const total = reasons.reduce((sum, r) => sum + r.count, 0);
    if (total === 0) return outcome === "lost" ? "No deals lost yet." : "No deals won yet.";

    const given = reasons.filter((r) => r.reason !== null);
    const unrecorded = total - given.reduce((sum, r) => sum + r.count, 0);
    if (given.length === 0) {
        return `None of the ${plural(total, "deal", "deals")} ${outcome} so far has a reason recorded.`;
    }

    // The server sorts by count, so the first given reason is the top one.
    const top = given[0];
    const share = Math.round((top.count / total) * 100);
    const gap = unrecorded > 0 ? ` (${plural(unrecorded, "deal has", "deals have")} no reason recorded.)` : "";
    return outcome === "lost"
        ? `"${top.label}" costs the most deals: ${top.count} of ${total} lost (${share}%). Worth tackling first.${gap}`
        : `"${top.label}" wins the most deals: ${top.count} of ${total} won (${share}%). Lean on it in your pitch.${gap}`;
}

// ---------------------------------------------------------------------------
// Loading
// ---------------------------------------------------------------------------

function ReportsSkeleton() {
    return (
        <>
            <PageHeader title="Reports" subtitle="Trends across every lead, worked out from the timeline." />
            <div className="space-y-6">
                <Card className="p-5">
                    <Skeleton className="h-4 w-40" />
                    <Skeleton className="mt-6 h-40 w-full" />
                </Card>
                <div className="grid gap-6 lg:grid-cols-2">
                    {Array.from({ length: 2 }).map((_, index) => (
                        <Card key={index} className="p-5">
                            <Skeleton className="h-4 w-32" />
                            <div className="mt-5 space-y-3">
                                {Array.from({ length: 5 }).map((__, row) => (
                                    <Skeleton key={row} className="h-5 w-full" />
                                ))}
                            </div>
                        </Card>
                    ))}
                </div>
            </div>
        </>
    );
}
