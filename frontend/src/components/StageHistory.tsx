import { History } from "lucide-react";

import { STAGE_STYLES, cx, formatDateTime, formatSpan } from "../lib/display";
import type { StageHistoryEntry } from "../lib/types";
import { StageBadge } from "./StageBadge";
import { Card, CardHeader } from "./ui";

/**
 * Every stage the lead has been in, newest first, with how long it stayed.
 *
 * Read from lead_stage_history, so the dates are when the stage really
 * changed rather than anything inferred from the timeline.
 */
export function StageHistoryCard({ history }: { history: StageHistoryEntry[] }) {
    const now = Date.now();

    // Each stint runs until the next entry starts; the last one is still going.
    const stints = history.map((entry, index) => {
        const next = history[index + 1];
        const start = new Date(entry.changed_at).getTime();
        const end = next ? new Date(next.changed_at).getTime() : now;
        return { entry, current: !next, ms: Math.max(0, end - start) };
    });
    const newestFirst = [...stints].reverse();

    return (
        <Card>
            <CardHeader
                title="Stage history"
                subtitle={
                    history.length <= 1
                        ? "Moves between stages will be listed here."
                        : `${history.length - 1} ${history.length === 2 ? "move" : "moves"} · newest first`
                }
            />
            {history.length === 0 ? (
                <div className="flex items-center gap-2 px-5 py-4 text-sm text-slate-400 dark:text-slate-500">
                    <History className="size-4" aria-hidden="true" />
                    No stage history recorded.
                </div>
            ) : (
                <ol className="px-5 py-4">
                    {newestFirst.map(({ entry, current, ms }, index) => (
                        <li key={entry.id} className="relative flex gap-3 pb-4 last:pb-0">
                            {/* The rail joining the dots, drawn on every item but the last. */}
                            {index < newestFirst.length - 1 && (
                                <span
                                    className="absolute top-3 bottom-0 left-[5px] w-px bg-slate-200 dark:bg-slate-700"
                                    aria-hidden="true"
                                />
                            )}
                            <span
                                className={cx(
                                    "relative mt-1.5 size-[11px] shrink-0 rounded-full ring-2 ring-white dark:ring-slate-900",
                                    STAGE_STYLES[entry.to_stage].solid,
                                )}
                                aria-hidden="true"
                            />
                            <div className="min-w-0 flex-1">
                                <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
                                    <StageBadge stage={entry.to_stage} />
                                    <span className="text-xs text-slate-500 dark:text-slate-400">
                                        {current ? `${formatSpan(ms)} so far` : formatSpan(ms)}
                                    </span>
                                </div>
                                <p className="mt-1 text-xs text-slate-400 dark:text-slate-500">
                                    {entry.from_stage
                                        ? `From ${STAGE_STYLES[entry.from_stage].label} · `
                                        : "Created in this stage · "}
                                    <time dateTime={entry.changed_at}>{formatDateTime(entry.changed_at)}</time>
                                </p>
                            </div>
                        </li>
                    ))}
                </ol>
            )}
        </Card>
    );
}
