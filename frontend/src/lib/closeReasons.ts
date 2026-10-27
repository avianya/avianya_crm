/**
 * Win/loss reasons. The lists come from GET /api/meta (model.LostReasons and
 * model.WonReasons), so the backend is the one place they are defined.
 */

import type { CloseReason, Meta, MetaOption, Stage } from "./types";

/** Notes on a close reason are capped at this, as on the server. */
export const MAX_CLOSE_NOTE = 500;

/** The reasons offered for a closed stage; none for an open one. */
export function reasonsFor(meta: Meta | null | undefined, stage: Stage): MetaOption<CloseReason>[] {
    if (!meta) return [];
    if (stage === "lost") return meta.lost_reasons;
    if (stage === "won") return meta.won_reasons;
    return [];
}

/** "Price too high" for ("lost", "price"); the code itself if it is unknown. */
export function reasonLabel(meta: Meta | null | undefined, stage: Stage, reason: CloseReason): string {
    return reasonsFor(meta, stage).find((r) => r.value === reason)?.label ?? reason;
}
