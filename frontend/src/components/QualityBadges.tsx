import { CAPI_STATUS_STYLES, QUALIFICATION_STYLES, QUALITY_STYLES, formatDateTime } from "../lib/display";
import type { CapiStatus, QualificationStatus, Quality } from "../lib/types";
import { Badge } from "./ui";

/** A lead's quality, with its score when it has one. */
export function QualityBadge({ quality, score, title }: { quality: Quality; score: number | null; title?: string }) {
    const style = QUALITY_STYLES[quality] ?? QUALITY_STYLES.not_evaluated;
    return (
        <Badge tone={style.badge} className="whitespace-nowrap">
            <span title={title}>
                {style.label}
                {score !== null && <span className="ml-1 tabular-nums opacity-75">{score}</span>}
            </span>
        </Badge>
    );
}

export function QualificationBadge({ status }: { status: QualificationStatus }) {
    const style = QUALIFICATION_STYLES[status] ?? QUALIFICATION_STYLES.pending;
    return <Badge tone={style.badge}>{style.label}</Badge>;
}

/** A conversion event's delivery, or a dash when the lead has sent none. */
export function CapiStatusBadge({
    status,
    eventName,
    at,
}: {
    status: CapiStatus | "";
    eventName?: string;
    at?: string | null;
}) {
    if (!status) return <span className="text-xs text-slate-400 dark:text-slate-500">—</span>;
    const style = CAPI_STATUS_STYLES[status];
    const title = [eventName, at ? formatDateTime(at) : ""].filter(Boolean).join(" · ");
    return (
        <Badge tone={style.badge} className="whitespace-nowrap">
            <span title={title || undefined}>{style.label}</span>
        </Badge>
    );
}
