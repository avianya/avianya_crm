import { useState } from "react";
import { CircleCheck, CircleHelp, CircleX, Megaphone, ShieldOff } from "lucide-react";

import { ApiError, api } from "../lib/api";
import { useAuth } from "../context/auth";
import { QUALITY_CATEGORY_LABELS, cx, formatDateTime, formatRelative } from "../lib/display";
import type { Lead, QualityHit } from "../lib/types";
import { useToast } from "../context/toast";
import { CapiStatusBadge, QualificationBadge, QualityBadge } from "./QualityBadges";
import { Button, Card, CardHeader, TextInput, Textarea } from "./ui";

const OUTCOME_ICONS: Record<QualityHit["outcome"], { icon: typeof CircleCheck; tone: string; label: string }> = {
    matched: { icon: CircleCheck, tone: "text-emerald-600 dark:text-emerald-400", label: "Yes" },
    failed: { icon: CircleX, tone: "text-rose-600 dark:text-rose-400", label: "No" },
    unknown: { icon: CircleHelp, tone: "text-slate-400 dark:text-slate-500", label: "Not known yet" },
};

/**
 * The lead's quality: score and band, how each rule came out, the reason in
 * words, qualification, and the spam / sharing flags. Quality is kept apart
 * from the stage and from what is reported to Meta.
 */
export function LeadQualityCard({ lead, onChange }: { lead: Lead; onChange: (lead: Lead) => void }) {
    const toast = useToast();
    const { is } = useAuth();
    const [reason, setReason] = useState(lead.qualification_reason);
    const [busy, setBusy] = useState("");
    const [overriding, setOverriding] = useState(false);
    const [overrideScore, setOverrideScore] = useState("");
    const [overrideReason, setOverrideReason] = useState("");

    const act = async (kind: string, fn: () => Promise<Lead>, done: string) => {
        setBusy(kind);
        try {
            onChange(await fn());
            toast.success(done);
        } catch (cause) {
            toast.error(cause instanceof ApiError ? cause.message : "That did not work.");
        } finally {
            setBusy("");
        }
    };

    const effective = lead.quality_override ?? lead.quality_score;
    // Overriding a score is for admins and managers (the server checks too).
    const isAdmin = is("admin", "manager");

    return (
        <Card>
            <CardHeader
                title="Lead quality"
                subtitle={lead.quality_updated_at ? `Worked out ${formatRelative(lead.quality_updated_at)}` : undefined}
                action={<QualityBadge quality={lead.quality} score={effective} />}
            />
            <div className="space-y-4 p-5">
                <p className="text-sm leading-relaxed text-slate-700 dark:text-slate-300">{lead.quality_reason || "Not worked out yet."}</p>

                {lead.quality_breakdown.length > 0 && (
                    <ul className="space-y-1.5">
                        {lead.quality_breakdown.map((hit) => {
                            const o = OUTCOME_ICONS[hit.outcome] ?? OUTCOME_ICONS.unknown;
                            return (
                                <li key={hit.rule_id} className="flex items-center gap-2 text-xs">
                                    <o.icon className={cx("size-3.5 shrink-0", o.tone)} aria-label={o.label} />
                                    <span className="flex-1 text-slate-700 dark:text-slate-300">
                                        {hit.label}
                                        {hit.required && <span className="ml-1 text-slate-400">(needed for Good)</span>}
                                        <span className="block text-[11px] text-slate-400">{QUALITY_CATEGORY_LABELS[hit.category] ?? hit.category}</span>
                                    </span>
                                    <span className="tabular-nums text-slate-500 dark:text-slate-400">
                                        {hit.points}/{hit.max}
                                    </span>
                                </li>
                            );
                        })}
                    </ul>
                )}

                {lead.quality_override !== null && (
                    <p className="rounded-lg bg-slate-50 px-3 py-2 text-xs text-slate-600 dark:bg-slate-950/50 dark:text-slate-300">
                        Set by hand to {lead.quality_override}
                        {lead.quality_override_at && ` (${formatDateTime(lead.quality_override_at)})`}: {lead.quality_override_reason}
                    </p>
                )}

                <div className="flex items-center gap-2 border-t border-slate-100 pt-4 dark:border-slate-800">
                    <span className="text-xs font-medium text-slate-600 dark:text-slate-400">Qualification</span>
                    <QualificationBadge status={lead.qualification_status} />
                    <span className="text-[11px] text-slate-400">from the stage</span>
                </div>
                <div className="space-y-2">
                    <Textarea
                        rows={2}
                        placeholder="Why is this lead (not) qualified?"
                        value={reason}
                        onChange={(e) => setReason(e.target.value)}
                        aria-label="Qualification reason"
                    />
                    {reason !== lead.qualification_reason && (
                        <Button
                            size="sm"
                            loading={busy === "reason"}
                            onClick={() => act("reason", () => api.updateQualification(lead.id, reason), "Reason saved.")}
                        >
                            Save reason
                        </Button>
                    )}
                </div>

                <div className="flex flex-wrap gap-2 border-t border-slate-100 pt-4 dark:border-slate-800">
                    <Button
                        size="sm"
                        variant={lead.spam_confirmed ? "secondary" : "ghost"}
                        loading={busy === "spam"}
                        onClick={() =>
                            act(
                                "spam",
                                () => api.updateLeadFlags(lead.id, { spam_confirmed: !lead.spam_confirmed }),
                                lead.spam_confirmed ? "No longer marked as spam." : "Marked as invalid / spam.",
                            )
                        }
                    >
                        <ShieldOff className="size-3.5" aria-hidden="true" />
                        {lead.spam_confirmed ? "Not spam after all" : "Confirm invalid / spam"}
                    </Button>
                    {isAdmin && !overriding && (
                        <Button size="sm" variant="ghost" onClick={() => setOverriding(true)}>
                            Override score
                        </Button>
                    )}
                    {isAdmin && lead.quality_override !== null && (
                        <Button
                            size="sm"
                            variant="ghost"
                            loading={busy === "clear"}
                            onClick={() => act("clear", () => api.clearQualityOverride(lead.id), "Override removed.")}
                        >
                            Remove override
                        </Button>
                    )}
                </div>

                {overriding && (
                    <div className="space-y-2 rounded-lg border border-slate-200 p-3 dark:border-slate-800">
                        <TextInput
                            type="number"
                            min={0}
                            max={100}
                            placeholder="Score 0-100"
                            value={overrideScore}
                            onChange={(e) => setOverrideScore(e.target.value)}
                            aria-label="Override score"
                        />
                        <TextInput
                            placeholder="Reason (required)"
                            value={overrideReason}
                            onChange={(e) => setOverrideReason(e.target.value)}
                            aria-label="Override reason"
                        />
                        <div className="flex gap-2">
                            <Button
                                size="sm"
                                variant="primary"
                                loading={busy === "override"}
                                onClick={() =>
                                    act(
                                        "override",
                                        async () => {
                                            const updated = await api.overrideQuality(lead.id, Number(overrideScore), overrideReason);
                                            setOverriding(false);
                                            return updated;
                                        },
                                        "Score overridden.",
                                    )
                                }
                            >
                                Save override
                            </Button>
                            <Button size="sm" variant="ghost" onClick={() => setOverriding(false)}>
                                Cancel
                            </Button>
                        </div>
                    </div>
                )}
            </div>
        </Card>
    );
}

/** Where a Meta lead came from, and what the CRM has told Meta about it. */
export function LeadMetaCard({ lead, onChange }: { lead: Lead; onChange: (lead: Lead) => void }) {
    const toast = useToast();
    const [busy, setBusy] = useState(false);
    const a = lead.meta_attribution;
    if (!a) return null;
    const events = lead.capi_events ?? [];

    const toggleSharing = async () => {
        setBusy(true);
        try {
            onChange(await api.updateLeadFlags(lead.id, { meta_share_opt_out: !lead.meta_share_opt_out }));
            toast.success(lead.meta_share_opt_out ? "Sharing with Meta allowed again." : "This lead will no longer be shared with Meta.");
        } catch (cause) {
            toast.error(cause instanceof ApiError ? cause.message : "That did not work.");
        } finally {
            setBusy(false);
        }
    };

    const rows: [string, string][] = [
        ["Campaign", a.campaign_name || a.campaign_id],
        ["Ad set", a.adset_name || a.adset_id],
        ["Ad", a.ad_name || a.ad_id],
        ["Form", a.form_id],
        ["Submitted", a.meta_created_at ? formatDateTime(a.meta_created_at) : ""],
    ];

    return (
        <Card>
            <CardHeader
                title={
                    <span className="inline-flex items-center gap-1.5">
                        <Megaphone className="size-4 text-blue-600" aria-hidden="true" /> Meta lead form
                    </span>
                }
                action={<CapiStatusBadge status={lead.meta_capi_status} eventName={lead.meta_last_event_name} at={lead.meta_last_event_at} />}
            />
            <div className="space-y-4 p-5">
                <dl className="grid grid-cols-[6rem_1fr] gap-x-3 gap-y-1 text-xs">
                    {rows
                        .filter(([, v]) => v)
                        .map(([k, v]) => (
                            <div key={k} className="contents">
                                <dt className="text-slate-500 dark:text-slate-400">{k}</dt>
                                <dd className="break-words text-slate-800 dark:text-slate-200">{v}</dd>
                            </div>
                        ))}
                </dl>

                <div>
                    <p className="mb-1.5 text-xs font-medium text-slate-600 dark:text-slate-400">Sent to Meta</p>
                    {events.length === 0 ? (
                        <p className="text-xs text-slate-500 dark:text-slate-400">Nothing yet.</p>
                    ) : (
                        <ul className="space-y-1.5">
                            {events.map((e) => (
                                <li key={e.id} className="flex items-center gap-2 text-xs">
                                    <span className="flex-1 text-slate-700 dark:text-slate-300">
                                        {e.event_name}
                                        {e.test_mode && <span className="ml-1 text-slate-400">(test)</span>}
                                        {e.last_error && e.status !== "sent" && (
                                            <span className="block text-[11px] text-slate-400">
                                                {e.error_code !== null && `#${e.error_code} `}
                                                {e.last_error}
                                            </span>
                                        )}
                                    </span>
                                    <CapiStatusBadge status={e.status} />
                                    <span className="w-20 text-right text-slate-400" title={formatDateTime(e.sent_at ?? e.created_at)}>
                                        {formatRelative(e.sent_at ?? e.created_at)}
                                    </span>
                                </li>
                            ))}
                        </ul>
                    )}
                    <p className="mt-2 text-[11px] text-slate-400">Failed events can be retried from Settings › Meta CAPI.</p>
                </div>

                <label className="flex items-start gap-2 border-t border-slate-100 pt-4 text-xs dark:border-slate-800">
                    <input
                        type="checkbox"
                        className="mt-0.5 size-4 accent-brand-600"
                        checked={lead.meta_share_opt_out}
                        disabled={busy}
                        onChange={toggleSharing}
                    />
                    <span>
                        <span className="font-medium text-slate-800 dark:text-slate-200">Do not share with Meta</span>
                        <span className="block text-slate-500 dark:text-slate-400">
                            For a lead who asked not to be shared. Unsent events are cancelled and none are sent again.
                        </span>
                    </span>
                </label>
            </div>
        </Card>
    );
}
