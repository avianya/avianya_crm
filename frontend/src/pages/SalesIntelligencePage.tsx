import { useState } from "react";
import { Link } from "react-router-dom";
import {
    AlertTriangle,
    ArrowDownRight,
    ArrowRight,
    ArrowUpRight,
    Award,
    BarChart3,
    BrainCircuit,
    Calculator,
    CheckCircle2,
    ChevronRight,
    Clock,
    DollarSign,
    Flame,
    Gauge,
    Lightbulb,
    RefreshCw,
    ShieldAlert,
    Target,
    TrendingUp,
    Zap,
} from "lucide-react";

import { api } from "../lib/api";
import {
    BAND_STYLES,
    STAGE_STYLES,
    cx,
    formatMoney,
    formatMoneyCompact,
} from "../lib/display";
import type {
    Lead,
    SalesIntelligenceReport,
    ScoreBand,
    Source,
    Stage,
} from "../lib/types";
import { useAsync } from "../hooks/useAsync";
import { useToast } from "../context/toast";
import {
    Badge,
    Button,
    Card,
    CardHeader,
    ErrorState,
    PageHeader,
} from "../components/ui";

type IntelligenceTab = "scoring" | "funnel" | "roi" | "simulator";

const SOURCE_BADGES: Record<Source, { label: string; badge: string }> = {
    website: {
        label: "Website",
        badge: "bg-blue-50 text-blue-700 ring-blue-200 dark:bg-blue-950 dark:text-blue-300 dark:ring-blue-800",
    },
    referral: {
        label: "Referral",
        badge: "bg-emerald-50 text-emerald-700 ring-emerald-200 dark:bg-emerald-950 dark:text-emerald-300 dark:ring-emerald-800",
    },
    linkedin: {
        label: "LinkedIn",
        badge: "bg-sky-50 text-sky-700 ring-sky-200 dark:bg-sky-950 dark:text-sky-300 dark:ring-sky-800",
    },
    cold_call: {
        label: "Cold Call",
        badge: "bg-purple-50 text-purple-700 ring-purple-200 dark:bg-purple-950 dark:text-purple-300 dark:ring-purple-800",
    },
    meta_ads: {
        label: "Meta Ads",
        badge: "bg-fuchsia-50 text-fuchsia-700 ring-fuchsia-200 dark:bg-fuchsia-950 dark:text-fuchsia-300 dark:ring-fuchsia-800",
    },
    google_ads: {
        label: "Google Ads",
        badge: "bg-amber-50 text-amber-700 ring-amber-200 dark:bg-amber-950 dark:text-amber-300 dark:ring-amber-800",
    },
    other: {
        label: "Other",
        badge: "bg-slate-100 text-slate-700 ring-slate-200 dark:bg-slate-800 dark:text-slate-300 dark:ring-slate-700",
    },
};

export function SalesIntelligencePage() {
    const toast = useToast();
    const [activeTab, setActiveTab] = useState<IntelligenceTab>("scoring");
    const [isSeeding, setIsSeeding] = useState(false);

    const { data, error, loading, refetch } = useAsync(
        (signal) => api.salesIntelligence(signal),
        [],
    );

    const handleSeedData = async () => {
        try {
            setIsSeeding(true);
            const res = await api.seedIntelligence();
            toast.success(res.message || "Benchmark intelligence dataset successfully seeded!");
            await refetch();
        } catch (err: any) {
            toast.error(err?.message ?? "Failed to seed demo data.");
        } finally {
            setIsSeeding(false);
        }
    };

    if (loading) return <IntelligenceSkeleton />;

    if (error || !data) {
        return (
            <div className="space-y-6">
                <PageHeader title="Sales Intelligence" />
                <ErrorState
                    message={error?.message ?? "Could not load sales intelligence report."}
                    onRetry={refetch}
                />
            </div>
        );
    }

    const { summary, scoring, funnel, source_roi } = data;

    return (
        <div className="space-y-6 animate-fade-in">
            {/* Header with Quick Action */}
            <div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
                <div>
                    <div className="flex items-center gap-2">
                        <div className="flex size-9 items-center justify-center rounded-xl bg-gradient-to-tr from-brand-600 to-indigo-500 text-white shadow-md shadow-brand-500/20">
                            <BrainCircuit className="size-5" />
                        </div>
                        <h1 className="text-xl font-bold tracking-tight text-slate-900 sm:text-2xl dark:text-white">
                            Sales Intelligence & Predictive Analytics
                        </h1>
                    </div>
                    <p className="mt-1 text-sm text-slate-500 dark:text-slate-400">
                        Multi-dimensional lead scoring, funnel velocity conversion tracking, and channel ROI unit economics.
                    </p>
                </div>
                <div className="flex items-center gap-2.5">
                    <Button
                        variant="secondary"
                        onClick={handleSeedData}
                        loading={isSeeding}
                        className="border-slate-300 shadow-sm hover:border-brand-500 dark:border-slate-700"
                        title="Re-populate CRM with 28 rich benchmark leads, multi-touch activities, and funnel history"
                    >
                        <RefreshCw className="size-4 text-brand-600 dark:text-brand-400" />
                        <span>Reset Sample Data</span>
                    </Button>
                </div>
            </div>

            {/* Executive KPI Ribbon */}
            <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-6">
                <KpiCard
                    label="Funnel Win Rate"
                    value={`${summary.overall_conversion_rate}%`}
                    subtext="Won ÷ Closed Deals"
                    tone="emerald"
                    icon={TrendingUp}
                />
                <KpiCard
                    label="Avg Lead Score"
                    value={`${summary.average_lead_score}`}
                    subtext="Scale of 0 – 100"
                    tone="amber"
                    icon={Gauge}
                />
                <KpiCard
                    label="Active Pipeline"
                    value={formatMoneyCompact(summary.total_pipeline_value)}
                    subtext={`${summary.open_leads} open prospect${summary.open_leads === 1 ? "" : "s"}`}
                    tone="brand"
                    icon={Target}
                />
                <KpiCard
                    label="Closed Won Revenue"
                    value={formatMoneyCompact(summary.total_won_revenue)}
                    subtext={`${summary.won_leads} won contract${summary.won_leads === 1 ? "" : "s"}`}
                    tone="emerald"
                    icon={Award}
                />
                <KpiCard
                    label="Overall Channel ROI"
                    value={`${summary.overall_channel_roi > 0 ? "+" : ""}${Math.round(summary.overall_channel_roi)}%`}
                    subtext={`${summary.overall_revenue_multiple}x Return on Spend`}
                    tone="violet"
                    icon={DollarSign}
                />
                <KpiCard
                    label="Avg Sales Cycle"
                    value={`${summary.average_sales_cycle_days > 0 ? summary.average_sales_cycle_days : 28}d`}
                    subtext="Creation to Won"
                    tone="sky"
                    icon={Clock}
                />
            </div>

            {/* Navigation Tabs */}
            <div className="flex overflow-x-auto border-b border-slate-200 no-scrollbar dark:border-slate-800">
                <nav className="flex space-x-2" aria-label="Tabs">
                    <TabButton
                        active={activeTab === "scoring"}
                        onClick={() => setActiveTab("scoring")}
                        icon={Flame}
                        label="Lead Scoring"
                        badge={`${scoring.top_leads.length} Hot`}
                        badgeColor="bg-orange-100 text-orange-700 dark:bg-orange-950/60 dark:text-orange-300"
                    />
                    <TabButton
                        active={activeTab === "funnel"}
                        onClick={() => setActiveTab("funnel")}
                        icon={BarChart3}
                        label="Sales Funnel Analytics"
                        badge={`${funnel.stages.length} Stages`}
                        badgeColor="bg-indigo-100 text-indigo-700 dark:bg-indigo-950/60 dark:text-indigo-300"
                    />
                    <TabButton
                        active={activeTab === "roi"}
                        onClick={() => setActiveTab("roi")}
                        icon={DollarSign}
                        label="Lead Source ROI"
                        badge={`${source_roi.sources.length} Channels`}
                        badgeColor="bg-emerald-100 text-emerald-700 dark:bg-emerald-950/60 dark:text-emerald-300"
                    />
                    <TabButton
                        active={activeTab === "simulator"}
                        onClick={() => setActiveTab("simulator")}
                        icon={Calculator}
                        label="Interactive Simulator"
                        badge="Modeler"
                        badgeColor="bg-brand-100 text-brand-700 dark:bg-brand-950/60 dark:text-brand-300"
                    />
                </nav>
            </div>

            {/* Tab Contents */}
            {activeTab === "scoring" && <LeadScoringTab scoring={scoring} />}
            {activeTab === "funnel" && <SalesFunnelTab funnel={funnel} />}
            {activeTab === "roi" && <LeadSourceRoiTab sourceRoi={source_roi} />}
            {activeTab === "simulator" && <SimulatorTab />}
        </div>
    );
}

// ===========================================================================
// TAB 1: LEAD SCORING & INTENT
// ===========================================================================

function LeadScoringTab({ scoring }: { scoring: SalesIntelligenceReport["scoring"] }) {
    const [filterBand, setFilterBand] = useState<string>("all");

    const filteredLeads =
        filterBand === "all"
            ? scoring.top_leads
            : scoring.top_leads.filter((l) => l.score?.band === filterBand);

    return (
        <div className="space-y-6">
            {/* Score Distribution & Engine Weights */}
            <div className="grid gap-6 lg:grid-cols-3">
                {/* Score Bands Overview */}
                <Card className="lg:col-span-1">
                    <CardHeader
                        title="Score Distribution"
                        subtitle="Proportion of open pipeline by predictive propensity."
                    />
                    <div className="space-y-4 p-5">
                        {scoring.band_breakdown.map((item) => (
                            <div key={item.band} className="space-y-1.5">
                                <div className="flex items-center justify-between text-xs">
                                    <span className="flex items-center gap-1.5 font-medium text-slate-800 dark:text-slate-200">
                                        <span
                                            className={cx(
                                                "size-2 rounded-full",
                                                item.band === "hot"
                                                    ? "bg-orange-500"
                                                    : item.band === "warm"
                                                      ? "bg-amber-500"
                                                      : "bg-slate-400",
                                            )}
                                        />
                                        {item.label}
                                    </span>
                                    <span className="font-semibold text-slate-900 dark:text-white">
                                        {item.count} leads ({item.percentage}%)
                                    </span>
                                </div>
                                <div className="h-2 w-full overflow-hidden rounded-full bg-slate-100 dark:bg-slate-800">
                                    <div
                                        className={cx(
                                            "h-full rounded-full transition-all duration-500",
                                            item.band === "hot"
                                                ? "bg-gradient-to-r from-orange-500 to-amber-500"
                                                : item.band === "warm"
                                                  ? "bg-gradient-to-r from-amber-400 to-yellow-400"
                                                  : "bg-slate-400",
                                        )}
                                        style={{ width: `${Math.max(item.percentage, 4)}%` }}
                                    />
                                </div>
                                <div className="flex justify-between text-[11px] text-slate-500 dark:text-slate-400">
                                    <span>Pipeline: {formatMoney(item.total_value)}</span>
                                    <span>Win Propensity: ~{item.average_probability}%</span>
                                </div>
                            </div>
                        ))}

                        <div className="mt-4 rounded-xl border border-slate-200 bg-slate-50 p-3.5 text-xs text-slate-600 dark:border-slate-800 dark:bg-slate-950/60 dark:text-slate-400">
                            <div className="flex items-start gap-2">
                                <Lightbulb className="mt-0.5 size-4 shrink-0 text-amber-500" />
                                <div>
                                    <span className="font-semibold text-slate-800 dark:text-slate-200">
                                        Sales Strategy:
                                    </span>{" "}
                                    Hot leads ({scoring.hot_count}) represent{" "}
                                    <span className="font-medium text-slate-900 dark:text-white">
                                        {formatMoney(scoring.hot_value)}
                                    </span>{" "}
                                    in imminent deal volume. Prioritize same-day contact on all Hot accounts.
                                </div>
                            </div>
                        </div>
                    </div>
                </Card>

                {/* Scoring Engine Model Architecture */}
                <Card className="lg:col-span-2">
                    <CardHeader
                        title="Predictive Scoring Engine Weights"
                        subtitle="Calculated live on every request across 8 objective momentum signals and penalty deductions."
                    />
                    <div className="grid gap-3 p-5 sm:grid-cols-2">
                        {scoring.factor_weights.map((rule) => (
                            <div
                                key={rule.key}
                                className="group rounded-xl border border-slate-200/80 bg-white p-3.5 shadow-xs transition-all hover:border-brand-300 hover:shadow-sm dark:border-slate-800 dark:bg-slate-900/60 dark:hover:border-slate-700"
                            >
                                <div className="flex items-center justify-between">
                                    <span className="font-medium text-slate-900 text-xs dark:text-white">
                                        {rule.label}
                                    </span>
                                    <span className="rounded-md bg-brand-50 px-2 py-0.5 font-semibold text-brand-700 text-[11px] ring-1 ring-brand-200/50 ring-inset dark:bg-brand-950/60 dark:text-brand-300 dark:ring-brand-800/40">
                                        Up to {rule.max_points} pts
                                    </span>
                                </div>
                                <p className="mt-1 text-[11px] leading-relaxed text-slate-500 dark:text-slate-400">
                                    {rule.description}
                                </p>
                            </div>
                        ))}
                    </div>
                    <div className="flex flex-wrap items-center justify-between gap-2 border-t border-slate-200 px-5 py-3 text-xs text-slate-500 dark:border-slate-800 dark:text-slate-400">
                        <span className="flex items-center gap-1.5">
                            <AlertTriangle className="size-3.5 text-rose-500" />
                            <span>
                                Active Deductions: Overdue tasks (-5 pts/task), Stagnant stages (-10 pts)
                            </span>
                        </span>
                        <span className="text-[11px] text-slate-400">Engine v2.4 Active</span>
                    </div>
                </Card>
            </div>

            {/* Top High-Intent Leads Leaderboard */}
            <Card>
                <div className="flex flex-col gap-3 border-b border-slate-200 px-5 py-4 sm:flex-row sm:items-center sm:justify-between dark:border-slate-800">
                    <div>
                        <div className="flex items-center gap-2">
                            <h2 className="text-sm font-semibold text-slate-900 dark:text-slate-100">
                                High-Intent Leads Leaderboard
                            </h2>
                            <span className="rounded-full bg-brand-100 px-2 py-0.5 font-semibold text-brand-800 text-[10px] dark:bg-brand-950 dark:text-brand-300">
                                Top Prospects
                            </span>
                        </div>
                        <p className="mt-0.5 text-xs text-slate-500 dark:text-slate-400">
                            Ranked by total propensity score, engagement velocity, and estimated deal value.
                        </p>
                    </div>

                    <div className="flex items-center gap-2">
                        <span className="text-xs text-slate-500 dark:text-slate-400">Filter band:</span>
                        <select
                            value={filterBand}
                            onChange={(e) => setFilterBand(e.target.value)}
                            className="select-chevron h-8 rounded-lg border border-slate-300 bg-white pr-8 pl-2.5 text-xs font-medium text-slate-700 shadow-xs dark:border-slate-700 dark:bg-slate-900 dark:text-slate-200"
                        >
                            <option value="all">All Bands</option>
                            <option value="hot">Hot (70+)</option>
                            <option value="warm">Warm (40-69)</option>
                            <option value="cold">Cold (&lt;40)</option>
                        </select>
                    </div>
                </div>

                <div className="divide-y divide-slate-100 overflow-x-auto dark:divide-slate-800/80">
                    {filteredLeads.map((lead, idx) => {
                        const score = lead.score?.total ?? 0;
                        const band = lead.score?.band ?? "cold";
                        const stageInfo = STAGE_STYLES[lead.stage];
                        const sourceInfo = SOURCE_BADGES[lead.source] || SOURCE_BADGES.other;

                        return (
                            <div
                                key={lead.id}
                                className="group flex flex-col items-start justify-between gap-4 p-4 transition-colors hover:bg-slate-50/80 sm:flex-row sm:items-center dark:hover:bg-slate-800/40"
                            >
                                <div className="flex items-center gap-3.5">
                                    <div className="flex size-7 shrink-0 items-center justify-center rounded-full bg-slate-100 font-mono text-xs font-bold text-slate-600 dark:bg-slate-800 dark:text-slate-300">
                                        #{idx + 1}
                                    </div>

                                    {/* Circular Score Gauge */}
                                    <div className="relative flex size-12 shrink-0 items-center justify-center">
                                        <svg className="size-full -rotate-90" viewBox="0 0 36 36">
                                            <path
                                                className="text-slate-200 dark:text-slate-700"
                                                strokeWidth="3.5"
                                                stroke="currentColor"
                                                fill="none"
                                                d="M18 2.0845 a 15.9155 15.9155 0 0 1 0 31.831 a 15.9155 15.9155 0 0 1 0 -31.831"
                                            />
                                            <path
                                                className={
                                                    band === "hot"
                                                        ? "text-orange-500"
                                                        : band === "warm"
                                                          ? "text-amber-500"
                                                          : "text-slate-400"
                                                }
                                                strokeDasharray={`${score}, 100`}
                                                strokeWidth="3.5"
                                                strokeLinecap="round"
                                                stroke="currentColor"
                                                fill="none"
                                                d="M18 2.0845 a 15.9155 15.9155 0 0 1 0 31.831 a 15.9155 15.9155 0 0 1 0 -31.831"
                                            />
                                        </svg>
                                        <div className="absolute text-xs font-bold text-slate-900 dark:text-white">
                                            {score}
                                        </div>
                                    </div>

                                    <div className="min-w-0">
                                        <div className="flex flex-wrap items-center gap-2">
                                            <Link
                                                to={`/leads/${lead.id}`}
                                                className="font-semibold text-slate-900 hover:text-brand-600 hover:underline dark:text-white dark:hover:text-brand-400"
                                            >
                                                {lead.name}
                                            </Link>
                                            <span className="text-xs text-slate-400">•</span>
                                            <span className="text-xs text-slate-600 dark:text-slate-300">
                                                {lead.company}
                                            </span>
                                            <Badge tone={stageInfo.badge}>{stageInfo.label}</Badge>
                                            <Badge tone={sourceInfo.badge}>{sourceInfo.label}</Badge>
                                        </div>
                                        <div className="mt-1 flex flex-wrap items-center gap-3 text-xs text-slate-500 dark:text-slate-400">
                                            <span>
                                                Value:{" "}
                                                <strong className="text-slate-900 dark:text-slate-100">
                                                    {lead.deal_value ? formatMoney(lead.deal_value) : "Unestimated"}
                                                </strong>
                                            </span>
                                            <span>•</span>
                                            <span>
                                                Contact:{" "}
                                                <span
                                                    className={cx(
                                                        "font-medium",
                                                        lead.health === "active"
                                                            ? "text-emerald-600 dark:text-emerald-400"
                                                            : lead.health === "cooling"
                                                              ? "text-amber-600 dark:text-amber-400"
                                                              : "text-rose-600 dark:text-rose-400",
                                                    )}
                                                >
                                                    {lead.days_since_touch === 0
                                                        ? "Today"
                                                        : `${lead.days_since_touch}d ago`}
                                                </span>
                                            </span>
                                            {lead.has_open_task && (
                                                <>
                                                    <span>•</span>
                                                    <span className="flex items-center gap-1 text-emerald-600 dark:text-emerald-400">
                                                        <CheckCircle2 className="size-3" />
                                                        Next step planned
                                                    </span>
                                                </>
                                            )}
                                        </div>
                                    </div>
                                </div>

                                <div className="flex shrink-0 items-center gap-3 self-end sm:self-center">
                                    {lead.score?.penalties && lead.score.penalties.length > 0 && (
                                        <div
                                            className="flex items-center gap-1 rounded-md bg-rose-50 px-2 py-1 text-[11px] font-medium text-rose-700 dark:bg-rose-950/60 dark:text-rose-300"
                                            title={lead.score.penalties.map((p) => p.label).join(", ")}
                                        >
                                            <AlertTriangle className="size-3" />
                                            <span>{lead.score.penalties[0].label}</span>
                                        </div>
                                    )}

                                    <Link
                                        to={`/leads/${lead.id}`}
                                        className="inline-flex items-center gap-1.5 rounded-lg border border-slate-200 bg-white px-3 py-1.5 text-xs font-medium text-slate-700 shadow-xs hover:border-brand-500 hover:text-brand-600 dark:border-slate-700 dark:bg-slate-900 dark:text-slate-200 dark:hover:text-brand-400"
                                    >
                                        <span>View Detail</span>
                                        <ChevronRight className="size-3.5" />
                                    </Link>
                                </div>
                            </div>
                        );
                    })}
                </div>
            </Card>

            {/* At-Risk High-Value Deals Alert Section */}
            {scoring.at_risk_deals.length > 0 && (
                <Card className="border-amber-200/80 bg-amber-50/20 dark:border-amber-900/40 dark:bg-amber-950/10">
                    <CardHeader
                        title={
                            <div className="flex items-center gap-2 text-amber-900 dark:text-amber-200">
                                <ShieldAlert className="size-4.5 text-amber-600 dark:text-amber-400" />
                                <span>At-Risk High-Value Pipeline Alerts</span>
                            </div>
                        }
                        subtitle="Advanced deals with high value (>₹4 Lakhs) that have gone cooling, cold, or accumulated penalties."
                    />
                    <div className="divide-y divide-amber-100 p-2 dark:divide-amber-950/40">
                        {scoring.at_risk_deals.map((deal: Lead) => (
                            <div
                                key={deal.id}
                                className="flex flex-col justify-between gap-3 p-3 text-xs sm:flex-row sm:items-center"
                            >
                                <div className="space-y-0.5">
                                    <div className="flex items-center gap-2">
                                        <Link
                                            to={`/leads/${deal.id}`}
                                            className="font-semibold text-slate-900 hover:underline dark:text-white"
                                        >
                                            {deal.name} ({deal.company})
                                        </Link>
                                        <Badge tone={STAGE_STYLES[deal.stage].badge}>
                                            {STAGE_STYLES[deal.stage].label}
                                        </Badge>
                                        <span className="font-bold text-amber-800 dark:text-amber-300">
                                            {deal.deal_value ? formatMoney(deal.deal_value) : ""}
                                        </span>
                                    </div>
                                    <p className="text-[11px] text-slate-500 dark:text-slate-400">
                                        Last contacted {deal.days_since_touch} days ago. Health:{" "}
                                        <span className="font-medium text-amber-700 dark:text-amber-300">
                                            {deal.health}
                                        </span>
                                        {deal.stuck && ` • Stuck in ${STAGE_STYLES[deal.stage].label}`}
                                    </p>
                                </div>

                                <Link
                                    to={`/leads/${deal.id}`}
                                    className="inline-flex items-center gap-1 rounded-md bg-amber-600 px-2.5 py-1 text-xs font-medium text-white shadow-xs hover:bg-amber-700"
                                >
                                    <span>Log Follow-up Call</span>
                                    <ArrowRight className="size-3" />
                                </Link>
                            </div>
                        ))}
                    </div>
                </Card>
            )}
        </div>
    );
}

// ===========================================================================
// TAB 2: SALES FUNNEL ANALYTICS
// ===========================================================================

function SalesFunnelTab({ funnel }: { funnel: SalesIntelligenceReport["funnel"] }) {
    return (
        <div className="space-y-6">
            {/* Bottleneck Diagnostic Banner */}
            <div className="rounded-2xl border border-indigo-200/80 bg-gradient-to-r from-indigo-50/80 via-white to-brand-50/60 p-5 shadow-xs dark:border-indigo-900/50 dark:from-indigo-950/30 dark:via-slate-900 dark:to-brand-950/20">
                <div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
                    <div className="flex items-start gap-3.5">
                        <div className="flex size-10 shrink-0 items-center justify-center rounded-xl bg-indigo-600 text-white shadow-md shadow-indigo-600/20">
                            <Zap className="size-5" />
                        </div>
                        <div>
                            <div className="flex items-center gap-2">
                                <span className="font-semibold text-indigo-950 text-sm dark:text-indigo-200">
                                    Funnel Bottleneck Detected: {funnel.bottleneck_stage}
                                </span>
                                <span className="rounded-full bg-indigo-100 px-2 py-0.5 font-medium text-indigo-800 text-[10px] dark:bg-indigo-950 dark:text-indigo-300">
                                    Action Required
                                </span>
                            </div>
                            <p className="mt-1 text-xs text-slate-600 dark:text-slate-300">
                                {funnel.bottleneck_reason}
                            </p>
                            <p className="mt-1.5 text-xs font-medium text-indigo-700 dark:text-indigo-300">
                                💡 Recommendation: {funnel.bottleneck_recommendation}
                            </p>
                        </div>
                    </div>

                    <div className="flex items-center gap-3 shrink-0 rounded-xl bg-white/80 p-3 shadow-xs dark:bg-slate-800/80">
                        <div className="text-right">
                            <p className="text-[11px] font-medium text-slate-500 dark:text-slate-400">
                                Total Inbound Throughput
                            </p>
                            <p className="text-base font-bold text-slate-900 dark:text-white">
                                {funnel.total_funnel_entries} Prospects
                            </p>
                        </div>
                    </div>
                </div>
            </div>

            {/* Stepped Visual Funnel & Stage Transition Cards */}
            <Card>
                <CardHeader
                    title="Interactive Stage-by-Stage Sales Funnel"
                    subtitle="Throughput volume, deal value, stage-to-stage conversion rate, and drop-off attrition."
                />

                <div className="space-y-4 p-5">
                    {funnel.stages.map((st, index) => {
                        const isWon = st.stage === "won";
                        const stageStyle = STAGE_STYLES[st.stage];

                        return (
                            <div key={st.stage} className="space-y-2">
                                <div className="flex flex-col justify-between gap-2 text-xs sm:flex-row sm:items-center">
                                    <div className="flex items-center gap-2 font-semibold text-slate-900 dark:text-white">
                                        <span className="flex size-5 items-center justify-center rounded-full bg-slate-100 text-[10px] text-slate-600 dark:bg-slate-800 dark:text-slate-300">
                                            {index + 1}
                                        </span>
                                        <Badge tone={stageStyle.badge}>{st.label}</Badge>
                                        <span className="text-slate-400">|</span>
                                        <span className="text-slate-600 dark:text-slate-300">
                                            {st.entered_count} Entered ({st.cumulative_conversion}% from top)
                                        </span>
                                        {st.current_count > 0 && (
                                            <span className="text-slate-400">
                                                • {st.current_count} active ({formatMoney(st.current_value)})
                                            </span>
                                        )}
                                    </div>

                                    <div className="flex items-center gap-3 font-medium">
                                        {index > 0 && (
                                            <span className="flex items-center gap-1 text-emerald-600 dark:text-emerald-400">
                                                <ArrowUpRight className="size-3.5" />
                                                {st.conversion_rate}% Conversion
                                            </span>
                                        )}
                                        {st.drop_off_count > 0 && (
                                            <span className="flex items-center gap-1 text-rose-500">
                                                <ArrowDownRight className="size-3.5" />
                                                {st.drop_off_count} Lost ({st.drop_off_rate}%)
                                            </span>
                                        )}
                                        {st.avg_duration_days > 0 && (
                                            <span className="text-slate-400">
                                                Avg {st.avg_duration_days} days
                                            </span>
                                        )}
                                    </div>
                                </div>

                                {/* Stepped Funnel Bar */}
                                <div className="relative h-9 w-full overflow-hidden rounded-xl bg-slate-100 dark:bg-slate-800">
                                    <div
                                        className={cx(
                                            "flex h-full items-center justify-between px-3 text-xs font-semibold text-white transition-all duration-700",
                                            isWon
                                                ? "bg-gradient-to-r from-emerald-500 to-teal-500"
                                                : index === 0
                                                  ? "bg-gradient-to-r from-slate-600 to-indigo-600"
                                                  : index === 1
                                                    ? "bg-gradient-to-r from-sky-500 to-brand-600"
                                                    : index === 2
                                                      ? "bg-gradient-to-r from-brand-600 to-violet-600"
                                                      : index === 3
                                                        ? "bg-gradient-to-r from-amber-500 to-orange-500"
                                                        : "bg-gradient-to-r from-orange-500 to-emerald-600",
                                        )}
                                        style={{ width: `${Math.max(st.cumulative_conversion, 12)}%` }}
                                    >
                                        <span className="truncate">{st.label}</span>
                                        <span>{st.entered_count} deals</span>
                                    </div>
                                </div>
                            </div>
                        );
                    })}
                </div>
            </Card>

            {/* Cycle Time & Stage Velocity */}
            <div className="grid gap-6 lg:grid-cols-2">
                <Card>
                    <CardHeader
                        title="Stage Duration & Velocity"
                        subtitle="Average calendar days spent in each stage before deal progression."
                    />
                    <div className="space-y-3.5 p-5">
                        {funnel.stages
                            .filter((s) => s.stage !== "won" && s.avg_duration_days > 0)
                            .map((st) => (
                                <div key={st.stage} className="space-y-1">
                                    <div className="flex justify-between text-xs">
                                        <span className="font-medium text-slate-800 dark:text-slate-200">
                                            {st.label}
                                        </span>
                                        <span className="font-bold text-slate-900 dark:text-white">
                                            {st.avg_duration_days} days
                                        </span>
                                    </div>
                                    <div className="h-2 w-full overflow-hidden rounded-full bg-slate-100 dark:bg-slate-800">
                                        <div
                                            className="h-full rounded-full bg-brand-500 transition-all duration-500"
                                            style={{
                                                width: `${Math.min((st.avg_duration_days / 14) * 100, 100)}%`,
                                            }}
                                        />
                                    </div>
                                </div>
                            ))}
                    </div>
                </Card>

                <Card>
                    <CardHeader
                        title="Funnel Conversion Economics"
                        subtitle="Key milestone conversion benchmarks."
                    />
                    <div className="space-y-4 p-5 text-xs">
                        <div className="flex items-center justify-between rounded-xl border border-slate-100 bg-slate-50 p-3.5 dark:border-slate-800 dark:bg-slate-950">
                            <div>
                                <p className="font-semibold text-slate-900 dark:text-white">
                                    Top-of-Funnel Qualification
                                </p>
                                <p className="text-slate-500">New → Qualified</p>
                            </div>
                            <span className="font-bold text-emerald-600 text-sm dark:text-emerald-400">
                                {funnel.stages.length >= 3
                                    ? `${funnel.stages[2].cumulative_conversion}%`
                                    : "71.4%"}
                            </span>
                        </div>

                        <div className="flex items-center justify-between rounded-xl border border-slate-100 bg-slate-50 p-3.5 dark:border-slate-800 dark:bg-slate-950">
                            <div>
                                <p className="font-semibold text-slate-900 dark:text-white">
                                    Proposal-to-Close Efficiency
                                </p>
                                <p className="text-slate-500">Proposal Sent → Won</p>
                            </div>
                            <span className="font-bold text-brand-600 text-sm dark:text-brand-400">
                                {funnel.stages.length >= 4 && funnel.stages[3].entered_count > 0
                                    ? `${Math.round(((funnel.stages[funnel.stages.length - 1].entered_count || 1) / funnel.stages[3].entered_count) * 100)}%`
                                    : "40%"}
                            </span>
                        </div>

                        <div className="flex items-center justify-between rounded-xl border border-slate-100 bg-slate-50 p-3.5 dark:border-slate-800 dark:bg-slate-950">
                            <div>
                                <p className="font-semibold text-slate-900 dark:text-white">
                                    End-to-End Win Rate
                                </p>
                                <p className="text-slate-500">All Inbound → Closed Won</p>
                            </div>
                            <span className="font-bold text-indigo-600 text-sm dark:text-indigo-400">
                                {funnel.overall_conversion_rate}%
                            </span>
                        </div>
                    </div>
                </Card>
            </div>
        </div>
    );
}

// ===========================================================================
// TAB 3: LEAD SOURCE ROI & UNIT ECONOMICS
// ===========================================================================

function LeadSourceRoiTab({ sourceRoi }: { sourceRoi: SalesIntelligenceReport["source_roi"] }) {
    return (
        <div className="space-y-6">
            {/* Source Highlights */}
            <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
                <div className="rounded-2xl border border-emerald-200 bg-gradient-to-br from-emerald-50 to-white p-4 shadow-xs dark:border-emerald-900/50 dark:from-emerald-950/20 dark:to-slate-900">
                    <p className="text-xs font-medium text-emerald-800 dark:text-emerald-300">
                        Top Performing Channel
                    </p>
                    <p className="mt-1 font-bold text-emerald-950 text-lg dark:text-emerald-100">
                        {sourceRoi.top_performing_source}
                    </p>
                    <p className="mt-0.5 text-[11px] text-emerald-700/80 dark:text-emerald-400">
                        Highest Net Margin & Multiple
                    </p>
                </div>

                <div className="rounded-2xl border border-brand-200 bg-gradient-to-br from-brand-50 to-white p-4 shadow-xs dark:border-brand-900/50 dark:from-brand-950/20 dark:to-slate-900">
                    <p className="text-xs font-medium text-brand-800 dark:text-brand-300">
                        Total Marketing & Acq Cost
                    </p>
                    <p className="mt-1 font-bold text-brand-950 text-lg dark:text-brand-100">
                        {formatMoney(sourceRoi.total_acquisition_cost)}
                    </p>
                    <p className="mt-0.5 text-[11px] text-brand-700/80 dark:text-brand-400">
                        Estimated Channel Spend
                    </p>
                </div>

                <div className="rounded-2xl border border-violet-200 bg-gradient-to-br from-violet-50 to-white p-4 shadow-xs dark:border-violet-900/50 dark:from-violet-950/20 dark:to-slate-900">
                    <p className="text-xs font-medium text-violet-800 dark:text-violet-300">
                        Net Profit Generated
                    </p>
                    <p className="mt-1 font-bold text-violet-950 text-lg dark:text-violet-100">
                        {formatMoney(sourceRoi.total_net_profit)}
                    </p>
                    <p className="mt-0.5 text-[11px] text-violet-700/80 dark:text-violet-400">
                        Won Revenue Minus Acq Cost
                    </p>
                </div>

                <div className="rounded-2xl border border-amber-200 bg-gradient-to-br from-amber-50 to-white p-4 shadow-xs dark:border-amber-900/50 dark:from-amber-950/20 dark:to-slate-900">
                    <p className="text-xs font-medium text-amber-800 dark:text-amber-300">
                        Overall Revenue Multiple (ROAS)
                    </p>
                    <p className="mt-1 font-bold text-amber-950 text-lg dark:text-amber-100">
                        {sourceRoi.overall_revenue_multiple}x Multiple
                    </p>
                    <p className="mt-0.5 text-[11px] text-amber-700/80 dark:text-amber-400">
                        ROI: +{Math.round(sourceRoi.overall_roi)}%
                    </p>
                </div>
            </div>

            {/* Granular Source Table */}
            <Card>
                <CardHeader
                    title="Lead Source ROI & Unit Economics Matrix"
                    subtitle="Detailed acquisition costs, conversion rates, customer acquisition cost (CAC), and return on investment per channel."
                />

                <div className="overflow-x-auto">
                    <table className="w-full text-left text-xs">
                        <thead className="border-b border-slate-200 bg-slate-50 font-semibold text-slate-600 dark:border-slate-800 dark:bg-slate-950 dark:text-slate-400">
                            <tr>
                                <th className="px-5 py-3">Channel / Source</th>
                                <th className="px-4 py-3 text-right">Leads</th>
                                <th className="px-4 py-3 text-right">Win Rate</th>
                                <th className="px-4 py-3 text-right">Closed Revenue</th>
                                <th className="px-4 py-3 text-right">Channel Cost</th>
                                <th className="px-4 py-3 text-right">CAC</th>
                                <th className="px-4 py-3 text-right">Net Profit</th>
                                <th className="px-4 py-3 text-right">ROI %</th>
                                <th className="px-5 py-3 text-center">Efficiency Tier</th>
                            </tr>
                        </thead>
                        <tbody className="divide-y divide-slate-100 dark:divide-slate-800/80">
                            {sourceRoi.sources.map((s) => {
                                const style = SOURCE_BADGES[s.source] || SOURCE_BADGES.other;

                                return (
                                    <tr
                                        key={s.source}
                                        className="transition-colors hover:bg-slate-50/70 dark:hover:bg-slate-800/40"
                                    >
                                        <td className="px-5 py-3.5">
                                            <div className="flex items-center gap-2">
                                                <Badge tone={style.badge}>{s.label}</Badge>
                                                <span className="text-[11px] text-slate-400">
                                                    ({s.share_of_leads}%)
                                                </span>
                                            </div>
                                        </td>
                                        <td className="px-4 py-3.5 text-right font-medium text-slate-900 dark:text-white">
                                            {s.leads}{" "}
                                            <span className="text-[10px] text-slate-400">
                                                ({s.won} won, {s.open} open)
                                            </span>
                                        </td>
                                        <td className="px-4 py-3.5 text-right font-semibold text-emerald-600 dark:text-emerald-400">
                                            {s.win_rate}%
                                        </td>
                                        <td className="px-4 py-3.5 text-right font-bold text-slate-900 dark:text-white">
                                            {formatMoney(s.won_value)}
                                        </td>
                                        <td className="px-4 py-3.5 text-right text-slate-600 dark:text-slate-300">
                                            {formatMoney(s.total_acquisition_cost)}
                                        </td>
                                        <td className="px-4 py-3.5 text-right font-mono font-medium text-slate-800 dark:text-slate-200">
                                            {s.cac > 0 ? formatMoney(s.cac) : "—"}
                                        </td>
                                        <td
                                            className={cx(
                                                "px-4 py-3.5 text-right font-semibold",
                                                s.net_profit >= 0
                                                    ? "text-emerald-600 dark:text-emerald-400"
                                                    : "text-rose-600 dark:text-rose-400",
                                            )}
                                        >
                                            {formatMoney(s.net_profit)}
                                        </td>
                                        <td className="px-4 py-3.5 text-right">
                                            <span
                                                className={cx(
                                                    "font-bold",
                                                    s.roi >= 0
                                                        ? "text-emerald-600 dark:text-emerald-400"
                                                        : "text-rose-600 dark:text-rose-400",
                                                )}
                                            >
                                                {s.roi > 0 ? "+" : ""}
                                                {Math.round(s.roi)}%
                                            </span>
                                        </td>
                                        <td className="px-5 py-3.5 text-center">
                                            <span
                                                className={cx(
                                                    "inline-flex items-center rounded-full px-2.5 py-0.5 text-[10px] font-semibold",
                                                    s.efficiency_tier.includes("Tier 1")
                                                        ? "bg-emerald-100 text-emerald-800 dark:bg-emerald-950 dark:text-emerald-300"
                                                        : s.efficiency_tier.includes("Tier 2")
                                                          ? "bg-brand-100 text-brand-800 dark:bg-brand-950 dark:text-brand-300"
                                                          : s.efficiency_tier.includes("Tier 3")
                                                            ? "bg-amber-100 text-amber-800 dark:bg-amber-950 dark:text-amber-300"
                                                            : "bg-slate-100 text-slate-700 dark:bg-slate-800 dark:text-slate-300",
                                                )}
                                            >
                                                {s.efficiency_tier}
                                            </span>
                                        </td>
                                    </tr>
                                );
                            })}
                        </tbody>
                    </table>
                </div>
            </Card>
        </div>
    );
}

// ===========================================================================
// TAB 4: INTERACTIVE SIMULATORS (LEAD SCORE & MARKETING ROI)
// ===========================================================================

function SimulatorTab() {
    // Simulator 1: Lead Score Calculator state
    const [simStage, setSimStage] = useState<Stage>("qualified");
    const [simSource, setSimSource] = useState<Source>("referral");
    const [simRecentContact, setSimRecentContact] = useState<number>(20); // 20: active, 8: cooling, 0: cold
    const [simConversations, setSimConversations] = useState<number>(3); // 0, 1, 2, 4+
    const [simHasTask, setSimHasTask] = useState<boolean>(true);
    const [simHasDealValue, setSimHasDealValue] = useState<boolean>(true);
    const [simHasPhone, setSimHasPhone] = useState<boolean>(true);
    const [simHasNotes, setSimHasNotes] = useState<boolean>(true);
    const [simOverdueTasks, setSimOverdueTasks] = useState<number>(0);
    const [simIsStuck, setSimIsStuck] = useState<boolean>(false);

    // Calculate simulated score
    const stagePointsMap: Record<Stage, number> = {
        new: 0,
        contacted: 8,
        qualified: 15,
        proposal_sent: 23,
        negotiation: 30,
        won: 0,
        lost: 0,
    };

    const sourcePointsMap: Record<Source, number> = {
        referral: 10,
        website: 7,
        google_ads: 7,
        linkedin: 5,
        meta_ads: 5,
        other: 3,
        cold_call: 2,
    };

    const engagementPoints = (c: number) => {
        if (c >= 4) return 15;
        if (c >= 2) return 10;
        if (c === 1) return 5;
        return 0;
    };

    const calcFactors = [
        { label: "Pipeline Stage", points: stagePointsMap[simStage], max: 30 },
        { label: "Recent Contact", points: simRecentContact, max: 20 },
        { label: "30-Day Conversations", points: engagementPoints(simConversations), max: 15 },
        { label: "Lead Source", points: sourcePointsMap[simSource], max: 10 },
        { label: "Next Step Planned", points: simHasTask ? 10 : 0, max: 10 },
        { label: "Deal Value Estimated", points: simHasDealValue ? 10 : 0, max: 10 },
        { label: "Phone on File", points: simHasPhone ? 3 : 0, max: 3 },
        { label: "Notes Written", points: simHasNotes ? 2 : 0, max: 2 },
    ];

    const penaltyPoints =
        Math.min(simOverdueTasks * 5, 10) + (simIsStuck ? 10 : 0);

    const grossPoints = calcFactors.reduce((acc, f) => acc + f.points, 0);
    const simTotalScore = Math.max(grossPoints - penaltyPoints, 0);

    const simBand: ScoreBand =
        simTotalScore >= 70 ? "hot" : simTotalScore >= 40 ? "warm" : "cold";

    // Simulator 2: Budget ROI Modeler state
    const [budgetWebsite, setBudgetWebsite] = useState(25000);
    const [budgetReferral, setBudgetReferral] = useState(10000);
    const [budgetLinkedin, setBudgetLinkedin] = useState(30000);
    const [budgetColdCall, setBudgetColdCall] = useState(15000);

    const totalSimBudget =
        budgetWebsite + budgetReferral + budgetLinkedin + budgetColdCall;
    const projectedSimRevenue =
        (budgetWebsite / 2500) * 0.25 * 950000 +
        (budgetReferral / 1000) * 0.45 * 1100000 +
        (budgetLinkedin / 3500) * 0.2 * 1400000 +
        (budgetColdCall / 1800) * 0.15 * 650000;

    const projectedProfit = projectedSimRevenue - totalSimBudget;
    const projectedRoi =
        totalSimBudget > 0 ? (projectedProfit / totalSimBudget) * 100 : 0;

    return (
        <div className="grid gap-6 lg:grid-cols-2">
            {/* Lead Scoring Simulator */}
            <Card>
                <CardHeader
                    title="Live Lead Score Modeler & Simulator"
                    subtitle="Adjust deal parameters to preview real-time predictive score calculation and tier classification."
                />
                <div className="space-y-4 p-5">
                    {/* Live Score Display Result */}
                    <div className="flex items-center justify-between rounded-2xl border border-slate-200 bg-slate-50/80 p-4 dark:border-slate-800 dark:bg-slate-950">
                        <div>
                            <p className="text-xs font-semibold tracking-wide text-slate-500 uppercase dark:text-slate-400">
                                Simulated Lead Score
                            </p>
                            <div className="mt-1 flex items-baseline gap-2">
                                <span className="font-extrabold text-3xl text-slate-900 dark:text-white">
                                    {simTotalScore}
                                </span>
                                <span className="text-xs text-slate-400">/ 100 pts</span>
                            </div>
                        </div>

                        <Badge
                            tone={
                                simBand === "hot"
                                    ? BAND_STYLES.hot.badge
                                    : simBand === "warm"
                                      ? BAND_STYLES.warm.badge
                                      : BAND_STYLES.cold.badge
                            }
                            className="px-3 py-1 text-sm font-bold"
                        >
                            {simBand.toUpperCase()} PROPENSITY
                        </Badge>
                    </div>

                    {/* Inputs */}
                    <div className="grid grid-cols-2 gap-3 text-xs">
                        <div>
                            <label className="block font-medium text-slate-700 dark:text-slate-300">
                                Pipeline Stage
                            </label>
                            <select
                                value={simStage}
                                onChange={(e) => setSimStage(e.target.value as Stage)}
                                className="select-chevron mt-1 w-full rounded-lg border border-slate-300 bg-white p-2 text-xs dark:border-slate-700 dark:bg-slate-900 dark:text-white"
                            >
                                <option value="new">New (0 pts)</option>
                                <option value="contacted">Contacted (+8 pts)</option>
                                <option value="qualified">Qualified (+15 pts)</option>
                                <option value="proposal_sent">Proposal Sent (+23 pts)</option>
                                <option value="negotiation">Negotiation (+30 pts)</option>
                            </select>
                        </div>

                        <div>
                            <label className="block font-medium text-slate-700 dark:text-slate-300">
                                Lead Source
                            </label>
                            <select
                                value={simSource}
                                onChange={(e) => setSimSource(e.target.value as Source)}
                                className="select-chevron mt-1 w-full rounded-lg border border-slate-300 bg-white p-2 text-xs dark:border-slate-700 dark:bg-slate-900 dark:text-white"
                            >
                                <option value="referral">Referral (+10 pts)</option>
                                <option value="website">Website (+7 pts)</option>
                                <option value="linkedin">LinkedIn (+5 pts)</option>
                                <option value="cold_call">Cold Call (+2 pts)</option>
                                <option value="other">Other (+3 pts)</option>
                            </select>
                        </div>
                    </div>

                    <div className="grid grid-cols-2 gap-3 text-xs">
                        <div>
                            <label className="block font-medium text-slate-700 dark:text-slate-300">
                                Contact Recency
                            </label>
                            <select
                                value={simRecentContact}
                                onChange={(e) => setSimRecentContact(Number(e.target.value))}
                                className="select-chevron mt-1 w-full rounded-lg border border-slate-300 bg-white p-2 text-xs dark:border-slate-700 dark:bg-slate-900 dark:text-white"
                            >
                                <option value={20}>Active (&lt;3-7d): +20 pts</option>
                                <option value={8}>Cooling: +8 pts</option>
                                <option value={0}>Cold: 0 pts</option>
                            </select>
                        </div>

                        <div>
                            <label className="block font-medium text-slate-700 dark:text-slate-300">
                                30-Day Conversations
                            </label>
                            <select
                                value={simConversations}
                                onChange={(e) => setSimConversations(Number(e.target.value))}
                                className="select-chevron mt-1 w-full rounded-lg border border-slate-300 bg-white p-2 text-xs dark:border-slate-700 dark:bg-slate-900 dark:text-white"
                            >
                                <option value={4}>4+ calls/meetings (+15 pts)</option>
                                <option value={2}>2-3 calls/meetings (+10 pts)</option>
                                <option value={1}>1 conversation (+5 pts)</option>
                                <option value={0}>0 conversations (0 pts)</option>
                            </select>
                        </div>
                    </div>

                    {/* Toggle Checks */}
                    <div className="grid grid-cols-2 gap-2 text-xs">
                        <label className="flex items-center gap-2 rounded-lg border border-slate-200 p-2 dark:border-slate-800">
                            <input
                                type="checkbox"
                                checked={simHasTask}
                                onChange={(e) => setSimHasTask(e.target.checked)}
                                className="rounded text-brand-600"
                            />
                            <span>Next Step Planned (+10)</span>
                        </label>
                        <label className="flex items-center gap-2 rounded-lg border border-slate-200 p-2 dark:border-slate-800">
                            <input
                                type="checkbox"
                                checked={simHasDealValue}
                                onChange={(e) => setSimHasDealValue(e.target.checked)}
                                className="rounded text-brand-600"
                            />
                            <span>Deal Estimated (+10)</span>
                        </label>
                        <label className="flex items-center gap-2 rounded-lg border border-slate-200 p-2 dark:border-slate-800">
                            <input
                                type="checkbox"
                                checked={simHasPhone}
                                onChange={(e) => setSimHasPhone(e.target.checked)}
                                className="rounded text-brand-600"
                            />
                            <span>Phone Verified (+3)</span>
                        </label>
                        <label className="flex items-center gap-2 rounded-lg border border-slate-200 p-2 dark:border-slate-800">
                            <input
                                type="checkbox"
                                checked={simHasNotes}
                                onChange={(e) => setSimHasNotes(e.target.checked)}
                                className="rounded text-brand-600"
                            />
                            <span>Context Notes (+2)</span>
                        </label>
                    </div>

                    {/* Penalty checks */}
                    <div className="rounded-xl border border-rose-200 bg-rose-50/50 p-3 text-xs dark:border-rose-900/40 dark:bg-rose-950/20">
                        <span className="font-semibold text-rose-800 dark:text-rose-300">
                            Simulate Risk Deductions:
                        </span>
                        <div className="mt-2 flex items-center gap-4">
                            <label className="flex items-center gap-1.5">
                                <span>Overdue Tasks:</span>
                                <input
                                    type="number"
                                    min={0}
                                    max={4}
                                    value={simOverdueTasks}
                                    onChange={(e) => setSimOverdueTasks(Number(e.target.value))}
                                    className="w-12 rounded border border-slate-300 p-1 text-center font-bold text-slate-800 dark:border-slate-700 dark:bg-slate-900 dark:text-white"
                                />
                            </label>
                            <label className="flex items-center gap-2">
                                <input
                                    type="checkbox"
                                    checked={simIsStuck}
                                    onChange={(e) => setSimIsStuck(e.target.checked)}
                                    className="rounded text-rose-600"
                                />
                                <span className="text-rose-700 dark:text-rose-300">
                                    Stage Stagnant (-10 pts)
                                </span>
                            </label>
                        </div>
                    </div>
                </div>
            </Card>

            {/* Marketing Budget & ROI Modeler */}
            <Card>
                <CardHeader
                    title="Marketing Budget & Channel ROI Modeler"
                    subtitle="Test different monthly channel spend allocations to forecast pipeline ROI, deal yield, and net revenue."
                />
                <div className="space-y-4 p-5 text-xs">
                    <div className="rounded-2xl border border-brand-200 bg-gradient-to-tr from-brand-50 via-white to-indigo-50 p-4 dark:border-brand-900/50 dark:from-brand-950/20 dark:via-slate-900 dark:to-indigo-950/20">
                        <div className="grid grid-cols-2 gap-4">
                            <div>
                                <p className="text-[11px] font-medium text-slate-500">
                                    Total Projected Revenue
                                </p>
                                <p className="font-extrabold text-2xl text-emerald-600 dark:text-emerald-400">
                                    {formatMoney(Math.round(projectedSimRevenue))}
                                </p>
                            </div>
                            <div className="text-right">
                                <p className="text-[11px] font-medium text-slate-500">
                                    Projected Channel ROI
                                </p>
                                <p className="font-extrabold text-2xl text-brand-600 dark:text-brand-400">
                                    +{Math.round(projectedRoi)}%
                                </p>
                            </div>
                        </div>
                    </div>

                    <div className="space-y-3">
                        <div>
                            <div className="flex justify-between font-medium">
                                <span>Website (SEO / Ads) Budget</span>
                                <span className="font-bold">{formatMoney(budgetWebsite)}</span>
                            </div>
                            <input
                                type="range"
                                min={5000}
                                max={100000}
                                step={5000}
                                value={budgetWebsite}
                                onChange={(e) => setBudgetWebsite(Number(e.target.value))}
                                className="mt-1 w-full"
                            />
                        </div>

                        <div>
                            <div className="flex justify-between font-medium">
                                <span>Referral / Partner Program Budget</span>
                                <span className="font-bold">{formatMoney(budgetReferral)}</span>
                            </div>
                            <input
                                type="range"
                                min={2000}
                                max={50000}
                                step={2000}
                                value={budgetReferral}
                                onChange={(e) => setBudgetReferral(Number(e.target.value))}
                                className="mt-1 w-full"
                            />
                        </div>

                        <div>
                            <div className="flex justify-between font-medium">
                                <span>LinkedIn ABM Sponsored InMail Budget</span>
                                <span className="font-bold">{formatMoney(budgetLinkedin)}</span>
                            </div>
                            <input
                                type="range"
                                min={5000}
                                max={100000}
                                step={5000}
                                value={budgetLinkedin}
                                onChange={(e) => setBudgetLinkedin(Number(e.target.value))}
                                className="mt-1 w-full"
                            />
                        </div>

                        <div>
                            <div className="flex justify-between font-medium">
                                <span>Outbound SDR & Data Tools Budget</span>
                                <span className="font-bold">{formatMoney(budgetColdCall)}</span>
                            </div>
                            <input
                                type="range"
                                min={3000}
                                max={60000}
                                step={3000}
                                value={budgetColdCall}
                                onChange={(e) => setBudgetColdCall(Number(e.target.value))}
                                className="mt-1 w-full"
                            />
                        </div>
                    </div>

                    <div className="rounded-xl border border-slate-200 bg-slate-50 p-3 text-[11px] text-slate-600 dark:border-slate-800 dark:bg-slate-950 dark:text-slate-400">
                        Total Simulated Spend: <strong>{formatMoney(totalSimBudget)}</strong>. Net
                        Projected Profit:{" "}
                        <strong className="text-emerald-600 dark:text-emerald-400">
                            {formatMoney(Math.round(projectedProfit))}
                        </strong>
                        .
                    </div>
                </div>
            </Card>
        </div>
    );
}

// ===========================================================================
// SUBCOMPONENTS & HELPERS
// ===========================================================================

function KpiCard({
    label,
    value,
    subtext,
    tone,
    icon: Icon,
}: {
    label: string;
    value: string;
    subtext: string;
    tone: "brand" | "emerald" | "amber" | "violet" | "sky";
    icon: any;
}) {
    const toneStyles = {
        brand: "text-brand-600 dark:text-brand-400",
        emerald: "text-emerald-600 dark:text-emerald-400",
        amber: "text-amber-600 dark:text-amber-400",
        violet: "text-violet-600 dark:text-violet-400",
        sky: "text-sky-600 dark:text-sky-400",
    };

    return (
        <Card className="p-3.5 shadow-xs transition-transform hover:-translate-y-0.5">
            <div className="flex items-center justify-between">
                <span className="text-[11px] font-medium text-slate-500 dark:text-slate-400">
                    {label}
                </span>
                <Icon className={cx("size-4", toneStyles[tone])} />
            </div>
            <div className="mt-1.5 font-extrabold text-xl tracking-tight text-slate-900 sm:text-2xl dark:text-white">
                {value}
            </div>
            <p className="mt-0.5 truncate text-[10px] text-slate-400 dark:text-slate-500">{subtext}</p>
        </Card>
    );
}

function TabButton({
    active,
    onClick,
    icon: Icon,
    label,
    badge,
    badgeColor,
}: {
    active: boolean;
    onClick: () => void;
    icon: any;
    label: string;
    badge: string;
    badgeColor: string;
}) {
    return (
        <button
            type="button"
            onClick={onClick}
            className={cx(
                "group inline-flex items-center gap-2 border-b-2 px-3 py-3 text-xs font-semibold whitespace-nowrap transition-colors",
                active
                    ? "border-brand-600 text-brand-700 dark:border-brand-400 dark:text-brand-300"
                    : "border-transparent text-slate-600 hover:border-slate-300 hover:text-slate-900 dark:text-slate-400 dark:hover:border-slate-700 dark:hover:text-slate-200",
            )}
        >
            <Icon className="size-4 shrink-0" />
            <span>{label}</span>
            <span
                className={cx(
                    "rounded-full px-2 py-0.5 text-[10px] font-medium transition-colors",
                    badgeColor,
                )}
            >
                {badge}
            </span>
        </button>
    );
}

function IntelligenceSkeleton() {
    return (
        <div className="space-y-6">
            <div className="h-10 w-72 animate-pulse rounded-lg bg-slate-200 dark:bg-slate-800" />
            <div className="grid grid-cols-2 gap-3 lg:grid-cols-6">
                {[...Array(6)].map((_, i) => (
                    <div
                        key={i}
                        className="h-24 animate-pulse rounded-2xl bg-slate-200 dark:bg-slate-800"
                    />
                ))}
            </div>
            <div className="h-96 animate-pulse rounded-2xl bg-slate-200 dark:bg-slate-800" />
        </div>
    );
}
