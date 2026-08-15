import { useState } from "react";
import { History, Send } from "lucide-react";

import { ACTIVITY_STYLES, cx, formatDateTime, formatRelative } from "../lib/display";
import type { Activity, ActivityInput, UserActivityType } from "../lib/types";
import { Button, EmptyState, Field, Select, Textarea } from "./ui";

/**
 * The lead's history: a vertical rail with one marker per event, newest first.
 *
 * Entries come from two places and are not visually separated, on purpose --
 * "Stage changed from Qualified to Proposal Sent" (written by the server) reads
 * alongside "Called about pricing" (written by a user), because what someone
 * needs when they open a lead is the single combined story of what happened.
 */
export function ActivityTimeline({ activities }: { activities: Activity[] }) {
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
        <ol className="relative space-y-1 px-5 py-4" role="list">
            {activities.map((activity, index) => {
                const style = ACTIVITY_STYLES[activity.type] ?? ACTIVITY_STYLES.note;
                const Icon = style.icon;
                const isLast = index === activities.length - 1;

                return (
                    <li key={activity.id} className="relative flex gap-3 pb-4">
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
                            </div>
                            <p className="mt-0.5 text-sm break-words text-slate-600 dark:text-slate-300">
                                {activity.note}
                            </p>
                        </div>
                    </li>
                );
            })}
        </ol>
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
