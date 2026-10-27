import { useState } from "react";
import { History, Pencil, Send, Trash2 } from "lucide-react";

import { ACTIVITY_STYLES, cx, formatDateTime, formatRelative, isEditableActivity } from "../lib/display";
import type { Activity, ActivityInput, UserActivityType } from "../lib/types";
import { ConfirmDialog } from "./ConfirmDialog";
import { Button, EmptyState, Field, Select, Textarea } from "./ui";

/**
 * The lead's history: a vertical rail with one marker per event, newest first.
 *
 * Entries come from two places and are not visually separated, on purpose --
 * "Stage changed from Qualified to Proposal Sent" (written by the server) reads
 * alongside "Called about pricing" (written by a user), because what someone
 * needs when they open a lead is the single combined story of what happened.
 *
 * Notes, calls, emails and meetings can be corrected or removed when onEdit
 * and onDelete are given. Entries the server wrote cannot.
 */
export function ActivityTimeline({
    activities,
    onEdit,
    onDelete,
}: {
    activities: Activity[];
    /** Resolves once saved; rejects (with a message) to keep the editor open. */
    onEdit?: (activity: Activity, note: string) => Promise<void>;
    /** Resolves once deleted; rejects to keep the confirmation open. */
    onDelete?: (activity: Activity) => Promise<void>;
}) {
    const [editingId, setEditingId] = useState<number | null>(null);
    const [deleting, setDeleting] = useState<Activity | null>(null);

    if (activities.length === 0) {
        return (
            <EmptyState
                icon={History}
                title="No activity yet"
                description="Log a call, an email or a note and it will show up here."
            />
        );
    }

    return (
        <>
        <ol className="relative space-y-1 px-5 py-4" role="list">
            {activities.map((activity, index) => {
                const style = ACTIVITY_STYLES[activity.type] ?? ACTIVITY_STYLES.note;
                const Icon = style.icon;
                const isLast = index === activities.length - 1;
                const editable = isEditableActivity(activity.type) && Boolean(onEdit || onDelete);
                const editing = editingId === activity.id;

                return (
                    <li key={activity.id} className="group relative flex gap-3 pb-4">
                        {/* The connecting rail. Omitted on the last item so the
                            line stops at the final marker instead of dangling. */}
                        {!isLast && (
                            <span
                                className="absolute top-8 left-[15px] h-full w-0.5 -translate-x-1/2 bg-slate-200 dark:bg-slate-800"
                                aria-hidden="true"
                            />
                        )}

                        <span
                            className={cx(
                                "relative z-10 flex size-8 shrink-0 items-center justify-center rounded-full",
                                style.marker,
                            )}
                        >
                            <Icon className="size-4" aria-hidden="true" />
                        </span>

                        <div className="min-w-0 flex-1 pt-1">
                            <div className="flex flex-wrap items-baseline gap-x-2 gap-y-0.5">
                                <span className="text-xs font-semibold text-slate-900 dark:text-slate-100">
                                    {style.label}
                                </span>
                                {/* <time> with a machine-readable datetime, and
                                    the exact timestamp on hover: relative labels
                                    are easier to scan, absolute ones are what you
                                    need when you actually have to be precise. */}
                                <time
                                    dateTime={activity.created_at}
                                    title={formatDateTime(activity.created_at)}
                                    className="text-xs text-slate-400 dark:text-slate-500"
                                >
                                    {formatRelative(activity.created_at)}
                                </time>
                                {activity.edited_at && (
                                    <span
                                        className="text-xs text-slate-400 italic dark:text-slate-500"
                                        title={`Edited ${formatDateTime(activity.edited_at)}`}
                                    >
                                        edited
                                    </span>
                                )}

                                {/* Faded in on hover to keep the timeline calm;
                                    focus-within keeps them reachable by keyboard,
                                    and on phones they always show. */}
                                {editable && !editing && (
                                    <span className="ml-auto flex gap-0.5 self-center transition-opacity sm:opacity-0 sm:group-hover:opacity-100 sm:focus-within:opacity-100">
                                        {onEdit && (
                                            <button
                                                type="button"
                                                onClick={() => setEditingId(activity.id)}
                                                aria-label={`Edit this ${style.label.toLowerCase()}`}
                                                title="Edit"
                                                className="rounded-md p-1 text-slate-400 transition-colors hover:bg-slate-100 hover:text-slate-700 dark:hover:bg-slate-800 dark:hover:text-slate-200"
                                            >
                                                <Pencil className="size-3.5" aria-hidden="true" />
                                            </button>
                                        )}
                                        {onDelete && (
                                            <button
                                                type="button"
                                                onClick={() => setDeleting(activity)}
                                                aria-label={`Delete this ${style.label.toLowerCase()}`}
                                                title="Delete"
                                                className="rounded-md p-1 text-slate-400 transition-colors hover:bg-rose-50 hover:text-rose-600 dark:hover:bg-rose-950/60 dark:hover:text-rose-400"
                                            >
                                                <Trash2 className="size-3.5" aria-hidden="true" />
                                            </button>
                                        )}
                                    </span>
                                )}
                            </div>

                            {editing && onEdit ? (
                                <EntryEditor
                                    activity={activity}
                                    onCancel={() => setEditingId(null)}
                                    onSave={async (note) => {
                                        await onEdit(activity, note);
                                        setEditingId(null);
                                    }}
                                />
                            ) : (
                                <p className="mt-0.5 text-sm break-words whitespace-pre-wrap text-slate-600 dark:text-slate-300">
                                    {activity.note}
                                </p>
                            )}
                        </div>
                    </li>
                );
            })}
        </ol>

        {onDelete && (
            <ConfirmDialog
                open={deleting !== null}
                onClose={() => setDeleting(null)}
                onConfirm={async () => {
                    if (deleting) await onDelete(deleting);
                }}
                title="Delete entry"
                message={
                    deleting
                        ? `Delete this ${ACTIVITY_STYLES[deleting.type].label.toLowerCase()} from the timeline? This cannot be undone.`
                        : ""
                }
            />
        )}
        </>
    );
}

/** Edits one entry's text in place. Esc cancels, Ctrl+Enter saves. */
function EntryEditor({
    activity,
    onCancel,
    onSave,
}: {
    activity: Activity;
    onCancel: () => void;
    /** Rejects with a message to keep the editor open. */
    onSave: (note: string) => Promise<void>;
}) {
    const [note, setNote] = useState(activity.note);
    const [saving, setSaving] = useState(false);
    const [error, setError] = useState<string | null>(null);

    const trimmed = note.trim();
    const id = `edit-activity-${activity.id}`;

    const save = async () => {
        if (!trimmed || saving) return;
        // Nothing changed: just close, rather than mark it "edited".
        if (trimmed === activity.note) {
            onCancel();
            return;
        }
        setSaving(true);
        setError(null);
        try {
            await onSave(trimmed);
        } catch (cause) {
            setError(cause instanceof Error ? cause.message : "Could not save the change.");
            setSaving(false);
        }
    };

    return (
        <div className="mt-1.5 space-y-2">
            <Textarea
                id={id}
                rows={3}
                value={note}
                maxLength={2000}
                autoFocus
                invalid={Boolean(error)}
                aria-label="Edit entry"
                onChange={(event) => setNote(event.target.value)}
                onKeyDown={(event) => {
                    if (event.key === "Escape") {
                        event.preventDefault();
                        onCancel();
                    } else if (event.key === "Enter" && (event.ctrlKey || event.metaKey)) {
                        event.preventDefault();
                        void save();
                    }
                }}
            />
            {error && (
                <p id={`${id}-error`} className="text-xs text-rose-600 dark:text-rose-400">
                    {error}
                </p>
            )}
            <div className="flex items-center justify-end gap-2">
                <span className="mr-auto text-xs text-slate-400 dark:text-slate-500">
                    {note.length} / 2000 · Ctrl+Enter to save, Esc to cancel
                </span>
                <Button size="sm" variant="ghost" onClick={onCancel} disabled={saving}>
                    Cancel
                </Button>
                <Button size="sm" variant="primary" loading={saving} disabled={!trimmed} onClick={() => void save()}>
                    Save
                </Button>
            </div>
        </div>
    );
}

/**
 * The "log an activity" composer that sits above the timeline.
 *
 * Kept inline on the page rather than behind a modal: logging a call is the most
 * frequent thing anyone does on this screen, and it should not cost a dialog.
 */
export function ActivityComposer({
    onSubmit,
    types,
}: {
    /** Resolves once the activity has been saved; rejects to keep the draft. */
    onSubmit: (input: ActivityInput) => Promise<void>;
    types: { value: UserActivityType; label: string }[];
}) {
    const [type, setType] = useState<UserActivityType>("note");
    const [note, setNote] = useState("");
    const [saving, setSaving] = useState(false);
    const [error, setError] = useState<string | null>(null);

    const trimmed = note.trim();

    // Split from the submit handler so the Ctrl+Enter shortcut can call it
    // directly, without having to fabricate a form event.
    const save = async () => {
        if (!trimmed || saving) return;

        setSaving(true);
        setError(null);

        try {
            await onSubmit({ type, note: trimmed });
            // Only clear the textarea once the save succeeded. Clearing
            // optimistically would throw away what someone typed if the request
            // failed.
            setNote("");
            setType("note");
        } catch (cause) {
            setError(cause instanceof Error ? cause.message : "Could not save the activity.");
        } finally {
            setSaving(false);
        }
    };

    const handleSubmit = (event: React.FormEvent) => {
        event.preventDefault();
        void save();
    };

    return (
        <form onSubmit={handleSubmit} className="space-y-3 px-5 py-4">
            <div className="grid gap-3 sm:grid-cols-[10rem_1fr]">
                <Field id="activity-type" label="Type">
                    <Select
                        id="activity-type"
                        value={type}
                        onChange={(event) =>
                            setType(event.target.value as UserActivityType)
                        }
                    >
                        {types.map((option) => (
                            <option key={option.value} value={option.value}>
                                {option.label}
                            </option>
                        ))}
                    </Select>
                </Field>

                <Field
                    id="activity-note"
                    label="What happened?"
                    error={error ?? undefined}
                    hint="Ctrl+Enter to save"
                >
                    <Textarea
                        id="activity-note"
                        rows={2}
                        value={note}
                        maxLength={2000}
                        invalid={Boolean(error)}
                        placeholder="e.g. Called to walk through the proposal - they want a revised quote by Friday."
                        onChange={(event) => setNote(event.target.value)}
                        onKeyDown={(event) => {
                            // A textarea swallows Enter for newlines, so the
                            // keyboard shortcut for "send" is Ctrl/Cmd+Enter --
                            // the same convention as most chat and issue trackers.
                            if (event.key === "Enter" && (event.ctrlKey || event.metaKey)) {
                                event.preventDefault();
                                void save();
                            }
                        }}
                    />
                </Field>
            </div>

            <div className="flex items-center justify-between gap-3">
                <span className="text-xs text-slate-400 dark:text-slate-500">
                    {note.length > 0 && `${note.length} / 2000`}
                </span>
                <Button
                    type="submit"
                    variant="primary"
                    size="sm"
                    loading={saving}
                    disabled={!trimmed}
                >
                    {!saving && <Send className="size-3.5" aria-hidden="true" />}
                    Log activity
                </Button>
            </div>
        </form>
    );
}
