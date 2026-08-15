import { useState } from "react";
import { AlertTriangle } from "lucide-react";

import { Modal } from "./Modal";
import { Button } from "./ui";

/**
 * A confirmation step for destructive actions.
 *
 * Deleting a lead also deletes its whole timeline (the foreign key cascades), and
 * there is no undo, so it gets an explicit "are you sure" naming exactly what is
 * about to be destroyed.
 */
export function ConfirmDialog({
    open,
    onClose,
    onConfirm,
    title,
    message,
    confirmLabel = "Delete",
}: {
    open: boolean;
    onClose: () => void;
    /** Awaited, so the button can show a spinner until the request finishes. */
    onConfirm: () => Promise<void>;
    title: string;
    message: string;
    confirmLabel?: string;
}) {
    const [working, setWorking] = useState(false);

    const handleConfirm = async () => {
        setWorking(true);
        try {
            await onConfirm();
            onClose();
        } finally {
            // Runs whether or not the request succeeded: on failure the dialog
            // stays open with a usable button rather than a stuck spinner. The
            // caller surfaces the error as a toast.
            setWorking(false);
        }
    };

    return (
        <Modal open={open} onClose={onClose} title={title}>
            <div className="flex gap-4 px-5 py-5">
                <div className="flex size-10 shrink-0 items-center justify-center rounded-full bg-rose-100 text-rose-600 dark:bg-rose-950 dark:text-rose-400">
                    <AlertTriangle className="size-5" aria-hidden="true" />
                </div>
                <p className="pt-2 text-sm text-slate-600 dark:text-slate-300">{message}</p>
            </div>

            <div className="flex justify-end gap-2 border-t border-slate-200 bg-slate-50 px-5 py-3.5 dark:border-slate-800 dark:bg-slate-950/50">
                <Button variant="secondary" onClick={onClose} disabled={working}>
                    Cancel
                </Button>
                <Button variant="danger" loading={working} onClick={handleConfirm}>
                    {confirmLabel}
                </Button>
            </div>
        </Modal>
    );
}
