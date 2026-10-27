import { useState } from "react";
import { Link } from "react-router-dom";
import { Megaphone, RefreshCw } from "lucide-react";

import { api } from "../lib/api";
import {
    CAPI_STATUS_ORDER,
    CAPI_STATUS_STYLES,
    QUALIFICATION_STYLES,
    QUALITY_ORDER,
    QUALITY_STYLES,
    STAGE_ORDER,
    STAGE_STYLES,
    cx,
    formatDateTime,
    formatRelative,
} from "../lib/display";
import type { MetaLeadParams, QualificationStatus } from "../lib/types";
import { useAsync } from "../hooks/useAsync";
import { memberName, useTeam } from "../hooks/useTeam";
import { CapiStatusBadge, QualificationBadge, QualityBadge } from "../components/QualityBadges";
import { Pagination } from "../components/Pagination";
import { StageBadge } from "../components/StageBadge";
import { Button, Card, EmptyState, ErrorState, PageHeader, Select, Skeleton, TextInput } from "../components/ui";

const QUALIFICATIONS: QualificationStatus[] = ["pending", "qualified", "not_qualified"];

/**
 * Leads from Meta lead forms: how many came in, how many qualified and
 * converted, and what has been reported back to Meta. Every number comes from
 * the stored leads, over everything the filters match.
 */
export function MetaLeadsPage() {
    const team = useTeam();
    const [params, setParams] = useState<MetaLeadParams>({ page: 1, page_size: 25 });
    const report = useAsync((signal) => api.metaLeads(params, signal), [JSON.stringify(params)]);

    const set = (patch: Partial<MetaLeadParams>) => setParams((p) => ({ ...p, ...patch, page: patch.page ?? 1 }));
    const data = report.data?.data;
    const summary = data?.summary;

    return (
        <>
            <PageHeader
                title="Meta leads"
                subtitle="Leads from Facebook and Instagram lead forms, their quality, and what Meta has been told."
                action={
                    <Button size="sm" onClick={report.refetch} loading={report.fetching}>
                        {!report.fetching && <RefreshCw className="size-3.5" aria-hidden="true" />}
                        Refresh
                    </Button>
                }
            />

            <Card className="mb-6">
                <div className="grid gap-3 p-4 sm:grid-cols-2 lg:grid-cols-4">
                    <FilterField label="From">
                        <TextInput type="date" value={params.from ?? ""} onChange={(e) => set({ from: e.target.value })} />
                    </FilterField>
                    <FilterField label="To">
                        <TextInput type="date" value={params.to ?? ""} onChange={(e) => set({ to: e.target.value })} />
                    </FilterField>
                    <FilterField label="Quality">
                        <Select value={params.quality ?? ""} onChange={(e) => set({ quality: e.target.value as MetaLeadParams["quality"] })}>
                            <option value="">Any quality</option>
                            {QUALITY_ORDER.map((q) => (
                                <option key={q} value={q}>
                                    {QUALITY_STYLES[q].label}
                                </option>
                            ))}
                        </Select>
                    </FilterField>
                    <FilterField label="Qualification">
                        <Select
                            value={params.qualification ?? ""}
                            onChange={(e) => set({ qualification: e.target.value as MetaLeadParams["qualification"] })}
                        >
                            <option value="">Any</option>
                            {QUALIFICATIONS.map((q) => (
                                <option key={q} value={q}>
                                    {QUALIFICATION_STYLES[q].label}
                                </option>
                            ))}
                        </Select>
                    </FilterField>
                    <FilterField label="Meta event status">
                        <Select
                            value={params.capi_status ?? ""}
                            onChange={(e) => set({ capi_status: e.target.value as MetaLeadParams["capi_status"] })}
                        >
                            <option value="">Any</option>
                            <option value="none">Nothing sent</option>
                            {CAPI_STATUS_ORDER.map((s) => (
                                <option key={s} value={s}>
                                    {CAPI_STATUS_STYLES[s].label}
                                </option>
                            ))}
                        </Select>
                    </FilterField>
                    <FilterField label="Stage">
                        <Select value={params.stage ?? ""} onChange={(e) => set({ stage: e.target.value as MetaLeadParams["stage"] })}>
                            <option value="">Any stage</option>
                            {STAGE_ORDER.map((s) => (
                                <option key={s} value={s}>
                                    {STAGE_STYLES[s].label}
                                </option>
                            ))}
                        </Select>
                    </FilterField>
                    <FilterField label="Campaign">
                        <Select value={params.campaign_id ?? ""} onChange={(e) => set({ campaign_id: e.target.value })}>
                            <option value="">Any campaign</option>
                            {data?.options.campaigns.map((o) => (
                                <option key={o.value} value={o.value}>
                                    {o.label} ({o.count})
                                </option>
                            ))}
                        </Select>
                    </FilterField>
                    <FilterField label="Salesperson">
                        <Select value={params.owner ?? ""} onChange={(e) => set({ owner: e.target.value })}>
                            <option value="">Anyone</option>
                            <option value="none">Unassigned</option>
                            {(team.data ?? []).map((m) => (
                                <option key={m.id} value={String(m.id)}>
                                    {m.name}
                                </option>
                            ))}
                        </Select>
                    </FilterField>
                </div>
            </Card>

            {report.loading ? (
                <Skeleton className="h-72 w-full rounded-2xl" />
            ) : report.error ? (
                <ErrorState message={report.error.message} onRetry={report.refetch} />
            ) : data && summary ? (
                <div className="space-y-6">
                    <div className="grid grid-cols-2 gap-3 lg:grid-cols-5">
                        <Stat label="Imported" value={String(summary.total)} />
                        <Stat label="Qualified" value={String(summary.qualified)} tone="text-emerald-600 dark:text-emerald-400" />
                        <Stat label="Converted" value={String(summary.converted)} tone="text-brand-600 dark:text-brand-400" />
                        <Stat label="Unqualified" value={String(summary.unqualified)} tone="text-rose-600 dark:text-rose-400" />
                        <Stat
                            label="Conversion rate"
                            hint="Converted ÷ imported"
                            value={summary.conversion_rate === null ? "—" : `${summary.conversion_rate.toFixed(1)}%`}
                        />
                    </div>

                    <Card>
                        {data.leads.length === 0 ? (
                            <EmptyState
                                icon={Megaphone}
                                title="No Meta leads"
                                description="Leads from Meta lead forms appear here as they arrive, or after Sync Now."
                            />
                        ) : (
                            <>
                                <div className="overflow-x-auto">
                                    <table className="w-full text-left text-sm">
                                        <thead className="text-xs text-slate-500 dark:text-slate-400">
                                            <tr className="border-b border-slate-200 dark:border-slate-800">
                                                <th className="px-5 py-2.5 font-medium">Lead</th>
                                                <th className="px-3 py-2.5 font-medium">Campaign / ad</th>
                                                <th className="px-3 py-2.5 font-medium">Quality</th>
                                                <th className="px-3 py-2.5 font-medium">Qualification</th>
                                                <th className="px-3 py-2.5 font-medium">Stage</th>
                                                <th className="px-3 py-2.5 font-medium">Salesperson</th>
                                                <th className="px-3 py-2.5 font-medium">Meta</th>
                                                <th className="px-5 py-2.5 font-medium">Submitted</th>
                                            </tr>
                                        </thead>
                                        <tbody className="divide-y divide-slate-100 dark:divide-slate-800">
                                            {data.leads.map(({ lead, attribution, effective_score, capi_status }) => {
                                                const submitted = attribution.meta_created_at ?? attribution.created_at;
                                                return (
                                                    <tr key={lead.id} className="hover:bg-slate-50 dark:hover:bg-slate-800/40">
                                                        <td className="px-5 py-3">
                                                            <Link
                                                                to={`/leads/${lead.id}`}
                                                                className="font-medium text-slate-900 hover:underline dark:text-slate-100"
                                                            >
                                                                {lead.name}
                                                            </Link>
                                                            {lead.spam_confirmed && <span className="ml-1 text-xs text-rose-500">spam</span>}
                                                        </td>
                                                        <td className="max-w-56 px-3 py-3 text-xs text-slate-600 dark:text-slate-300">
                                                            <span className="block truncate">{attribution.campaign_name || attribution.campaign_id || "—"}</span>
                                                            <span className="block truncate text-slate-400">{attribution.ad_name || attribution.ad_id}</span>
                                                        </td>
                                                        <td className="px-3 py-3">
                                                            <QualityBadge quality={lead.quality} score={effective_score} title={lead.quality_reason} />
                                                        </td>
                                                        <td className="px-3 py-3">
                                                            <QualificationBadge status={lead.qualification_status} />
                                                        </td>
                                                        <td className="px-3 py-3">
                                                            <StageBadge stage={lead.stage} />
                                                        </td>
                                                        <td className="px-3 py-3 text-xs text-slate-600 dark:text-slate-300">
                                                            {memberName(team.data, lead.owner_id) ?? "—"}
                                                        </td>
                                                        <td className="px-3 py-3">
                                                            <CapiStatusBadge
                                                                status={capi_status}
                                                                eventName={lead.meta_last_event_name}
                                                                at={lead.meta_last_event_at}
                                                            />
                                                        </td>
                                                        <td
                                                            className="px-5 py-3 text-xs whitespace-nowrap text-slate-500 dark:text-slate-400"
                                                            title={formatDateTime(submitted)}
                                                        >
                                                            {formatRelative(submitted)}
                                                        </td>
                                                    </tr>
                                                );
                                            })}
                                        </tbody>
                                    </table>
                                </div>
                                {report.data && (
                                    <div className="border-t border-slate-200 dark:border-slate-800">
                                        <Pagination
                                            pagination={report.data.pagination}
                                            onPageChange={(page) => set({ page })}
                                            disabled={report.fetching}
                                        />
                                    </div>
                                )}
                            </>
                        )}
                    </Card>
                </div>
            ) : null}
        </>
    );
}

function FilterField({ label, children }: { label: string; children: React.ReactNode }) {
    return (
        <label className="block">
            <span className="mb-1 block text-xs font-medium text-slate-600 dark:text-slate-400">{label}</span>
            {children}
        </label>
    );
}

function Stat({ label, value, tone, hint }: { label: string; value: string; tone?: string; hint?: string }) {
    return (
        <div className="rounded-xl border border-slate-200 bg-white px-4 py-3 dark:border-slate-800 dark:bg-slate-900" title={hint}>
            <p className={cx("text-xl font-semibold tabular-nums", tone ?? "text-slate-900 dark:text-slate-100")}>{value}</p>
            <p className="text-xs text-slate-500 dark:text-slate-400">{label}</p>
        </div>
    );
}
