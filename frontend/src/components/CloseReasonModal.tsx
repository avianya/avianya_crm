import { useState } from "react";
import type { FormEvent } from "react";

import { MAX_CLOSE_NOTE, reasonsFor } from "../lib/closeReasons";
import { cx } from "../lib/display";
import type { CloseInput, CloseReason, Meta, Stage } from "../lib/types";
import { Modal, ModalBody, ModalFooter } from "./Modal";
import { Button, Field, Textarea } from "./ui";

/** What the dialog is asking about. */
export interface CloseReasonRequest {
    leadName: string;
    stage: Stage; // "won" or "lost"
    /** The reason the lead has now, when changing an existing one. */
    current?: CloseInput;
}

/**
 * Asks why a deal was won or lost.
 *
 * Lost must have a reason: the dialog cannot be confirmed without one. Won can
 * be skipped, so closing a win stays quick. Cancelling means "don't move the
 * deal at all". "Other" needs a note to mean anything.
 */
export function CloseReasonModal({
    request,
    meta,
    onConfirm,
    onCancel,
}: {
    request: CloseReasonRequest | null;
    meta: Meta | null | undefined;
    onConfirm: (close: CloseInput) => void;
    onCancel: () => void;
}) {
    const lost = request?.stage === "lost";
    const changing = request?.current !== undefined;

    return (
        <Modal
            open={request !== null}
            onClose={onCancel}
            title={lost ? "Why was this deal lost?" : "Why was this deal won?"}
            description={
                request
                    ? lost
                        ? `${request.leadName}. The reason feeds the loss report, so you can see what costs the most deals.`
                        : `${request.leadName}. Optional: skip it if you'd rather not say.`
                    : undefined
            }
        >
            {request && (
                <ReasonForm
                    key={`${request.leadName}-${request.stage}`}
                    request={request}
                    meta={meta}
                    changing={changing}
                    onConfirm={onConfirm}
                    onCancel={onCancel}
                />
            )}
        </Modal>
    );
}

function ReasonForm({
    request,
    meta,
    changing,
    onConfirm,
    onCancel,
}: {
    request: CloseReasonRequest;
    meta: Meta | null | undefined;
    changing: boolean;
    onConfirm: (close: CloseInput) => void;
    onCancel: () => void;
}) {
    const lost = request.stage === "lost";
    const reasons = reasonsFor(meta, request.stage);
    const [reason, setReason] = useState<CloseReason | null>(request.current?.close_reason ?? null);
    const [note, setNote] = useState(request.current?.close_note ?? "");
    const [error, setError] = useState<Record<string, string>>({});

    const submit = (event: FormEvent) => {
        event.preventDefault();
        if (!reason) {
            setError({ reason: "Choose a reason." });
            return;
        }
        if (reason === "other" && !note.trim()) {
            setError({ note: 'Say a few words about what "Other" means here.' });
            return;
        }
        onConfirm({ close_reason: reason, close_note: note.trim() });
    };

    return (
        <form onSubmit={submit} noValidate>
            <ModalBody>
                <fieldset>
                    <legend className="mb-2 block text-xs font-medium text-slate-700 dark:text-slate-300">
                        Reason
                        {lost && (
                            <span className="ml-0.5 text-rose-500" aria-hidden="true">
                                *
                            </span>
                        )}
                    </legend>
                    {reasons.length === 0 ? (
                        <p className="text-sm text-slate-500 dark:text-slate-400">Loading reasons…</p>
                    ) : (
                        <div className="grid gap-2 sm:grid-cols-2">
                            {reasons.map((option) => (
                                <label
                                    key={option.value}
                                    className={cx(
                                        "flex cursor-pointer items-center gap-2.5 rounded-lg border px-3 py-2 text-sm transition-colors",
                                        reason === option.value
                                            ? "border-brand-500 bg-brand-50 text-brand-800 dark:border-brand-500 dark:bg-brand-950 dark:text-brand-200"
                                            : "border-slate-200 text-slate-700 hover:bg-slate-50 dark:border-slate-700 dark:text-slate-300 dark:hover:bg-slate-800/50",
                                    )}
                                >
                                    <input
                                        type="radio"
                                        name="close-reason"
                                        value={option.value}
                                        checked={reason === option.value}
                                        onChange={() => {
                                            setReason(option.value);
                                            setError({});
                                        }}
                                        className="size-4 border-slate-300 text-brand-600 dark:border-slate-600"
                                    />
                                    {option.label}
                                </label>
                            ))}
                        </div>
                    )}
                    {error.reason && (
                        <p className="mt-1.5 text-xs text-rose-600 dark:text-rose-400" role="alert">
                            {error.reason}
                        </p>
                    )}
                </fieldset>

                <Field
                    id="close-note"
                    label={reason === "other" ? "Note (required for Other)" : "Note (optional)"}
                    error={error.note}
                    hint={`What happened, in a line. ${note.length} / ${MAX_CLOSE_NOTE}`}
                >
                    <Textarea
                        id="close-note"
                        rows={2}
                        value={note}
                        maxLength={MAX_CLOSE_NOTE}
                        invalid={Boolean(error.note)}
                        placeholder={lost ? "e.g. Competitor offered 20% off" : "e.g. Loved the onboarding demo"}
                        onChange={(event) => {
                            setNote(event.target.value);
                            setError({});
                        }}
                    />
                </Field>
            </ModalBody>
            <ModalFooter>
                <Button onClick={onCancel}>Cancel</Button>
                {!lost && !changing && (
                    <Button onClick={() => onConfirm({ close_reason: null, close_note: "" })}>Skip</Button>
                )}
                <Button type="submit" variant="primary">
                    {changing ? "Save reason" : lost ? "Mark as Lost" : "Mark as Won"}
                </Button>
            </ModalFooter>
        </form>
    );
}
