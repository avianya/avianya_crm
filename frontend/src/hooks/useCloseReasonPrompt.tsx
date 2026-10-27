import { useCallback, useRef, useState } from "react";
import type { ReactNode } from "react";

import { api } from "../lib/api";
import type { CloseInput, Lead, Meta, Stage } from "../lib/types";
import { CloseReasonModal } from "../components/CloseReasonModal";
import type { CloseReasonRequest } from "../components/CloseReasonModal";
import { useAsync } from "./useAsync";

/**
 * Asks why a deal closed before it is moved to Won or Lost, from wherever the
 * move starts: a stage picker, the "Lost" button, a drag on the board.
 *
 *     const closeReason = useCloseReasonPrompt();
 *     const close = await closeReason.ask(lead, "lost");
 *     if (close === null) return; // cancelled: leave the deal where it is
 *     await api.updateLeadStage(lead.id, "lost", close);
 *     ...
 *     return <>{page}{closeReason.dialog}</>;
 *
 * ask resolves to undefined straight away for an open stage (nothing to ask),
 * to null when cancelled, and otherwise to the reason (empty for a skipped win).
 */
export function useCloseReasonPrompt(): {
    ask: (lead: Lead, stage: Stage, opts?: { changing?: boolean }) => Promise<CloseInput | null | undefined>;
    dialog: ReactNode;
    meta: Meta | null;
} {
    const { data: meta } = useAsync((signal) => api.meta(signal), []);
    const [request, setRequest] = useState<CloseReasonRequest | null>(null);
    const resolver = useRef<((close: CloseInput | null) => void) | null>(null);

    const ask = useCallback((lead: Lead, stage: Stage, opts?: { changing?: boolean }) => {
        if (stage !== "won" && stage !== "lost") return Promise.resolve(undefined);

        // Changing the reason on a deal already in this stage starts from the
        // one it has.
        const current =
            opts?.changing && lead.stage === stage
                ? { close_reason: lead.close_reason, close_note: lead.close_note ?? "" }
                : undefined;

        return new Promise<CloseInput | null>((resolve) => {
            resolver.current?.(null); // a newer question replaces an unanswered one
            resolver.current = resolve;
            setRequest({ leadName: lead.name, stage, current });
        });
    }, []);

    const finish = (close: CloseInput | null) => {
        resolver.current?.(close);
        resolver.current = null;
        setRequest(null);
    };

    return {
        ask,
        meta: meta ?? null,
        dialog: (
            <CloseReasonModal
                request={request}
                meta={meta}
                onConfirm={(close) => finish(close)}
                onCancel={() => finish(null)}
            />
        ),
    };
}
