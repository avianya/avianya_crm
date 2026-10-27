import { useState } from "react";
import type { FormEvent } from "react";
import { Ban, Check, RotateCcw, Trash2 } from "lucide-react";

import { ApiError, api } from "../lib/api";
import { DEFAULT_TASK_DURATION, TASK_STATUS_STYLES, cx } from "../lib/display";
import type { Task, TaskType } from "../lib/types";
import { useTeam } from "../hooks/useTeam";
import { AssigneeSelect, ReminderSelect } from "./EventFields";
import { Modal, ModalBody, ModalFooter } from "./Modal";
import { TaskTypePicker } from "./TaskTypePicker";
import { TimeSlotPicker } from "./TimeSlotPicker";
import { Button, Field, TextInput, Textarea } from "./ui";

/**
 * Edits an event: title, type, date and time, agenda, who it is for and its
 * reminder -- and completes, cancels or restores it. Used wherever tasks are
 * listed, so a typo or a moved meeting is fixed in place rather than by
 * delete-and-re-add.
 *
 * `subtitle` names the lead, when the caller knows it. `onSaved` receives the
 * saved task and a short confirmation to show.
 */
export function TaskEditModal({
    task,
    subtitle,
    onClose,
    onSaved,
    onDeleted,
}: {
    task: Task | null;
    subtitle?: string;
    onClose: () => void;
    onSaved: (task: Task, message: string) => void;
    /** Called with the deleted task once the server has removed it. */
    onDeleted: (task: Task) => void;
}) {
    return (
        <Modal open={task !== null} onClose={onClose} title="Edit event" description={subtitle}>
            {/* Keyed by task, so opening another task starts from its own values. */}
            {task && (
                <TaskEditForm key={task.id} task={task} onCancel={onClose} onSaved={onSaved} onDeleted={onDeleted} />
            )}
        </Modal>
    );
}

/** Which in-place confirmation is showing, if any. */
type Confirming = "delete" | "cancel" | null;

function TaskEditForm({
    task,
    onCancel,
    onSaved,
    onDeleted,
}: {
    task: Task;
    onCancel: () => void;
    onSaved: (task: Task, message: string) => void;
    onDeleted: (task: Task) => void;
}) {
    const team = useTeam();
    const [title, setTitle] = useState(task.title);
    const [type, setType] = useState<TaskType>(task.type);
    const [dueDate, setDueDate] = useState(task.due_date);
    const [startTime, setStartTime] = useState(task.start_time ?? "");
    const [duration, setDuration] = useState(task.duration_minutes ?? DEFAULT_TASK_DURATION);
    const [notes, setNotes] = useState(task.notes);
    const [assigneeId, setAssigneeId] = useState(task.assignee_id);
    const [reminder, setReminder] = useState(task.reminder_minutes);
    const [errors, setErrors] = useState<Record<string, string>>({});
    const [saving, setSaving] = useState(false);

    // Delete and cancel ask once, in place: a second dialog stacked on this one
    // would fight it for focus and Escape.
    const [confirming, setConfirming] = useState<Confirming>(null);
    const [acting, setActing] = useState(false);

    const closed = task.status === "completed" || task.status === "cancelled";
    const busy = saving || acting;

    const failed = (cause: unknown, fallback: string) => {
        setErrors({ form: cause instanceof ApiError ? cause.message : fallback });
        setConfirming(null);
    };

    const remove = async () => {
        setActing(true);
        try {
            await api.deleteTask(task.id);
            onDeleted(task);
        } catch (cause) {
            failed(cause, "Could not delete the event.");
        } finally {
            setActing(false);
        }
    };

    /** Completes, reopens, cancels or restores, each a single PATCH. */
    const changeStatus = async (action: "complete" | "reopen" | "cancel" | "restore") => {
        setActing(true);
        try {
            const result =
                action === "complete" || action === "reopen"
                    ? await api.setTaskCompleted(task.id, action === "complete")
                    : await api.setTaskCancelled(task.id, action === "cancel");
            const messages = {
                complete: "Marked complete and logged on the timeline.",
                reopen: "Reopened.",
                cancel: "Event cancelled. Its reminder is off.",
                restore: "Event restored.",
            };
            onSaved(result.task, messages[action]);
        } catch (cause) {
            failed(cause, "Could not update the event.");
        } finally {
            setActing(false);
        }
    };

    const submit = async (event: FormEvent) => {
        event.preventDefault();
        const next: Record<string, string> = {};
        if (!title.trim()) next.title = "Enter what needs doing.";
        if (!dueDate) next.due_date = "Pick a date.";
        setErrors(next);
        if (Object.keys(next).length > 0) return;

        setSaving(true);
        try {
            const saved = await api.updateTask(task.id, {
                title: title.trim(),
                type,
                due_date: dueDate,
                ...(startTime && { start_time: startTime, duration_minutes: duration }),
                notes: notes.trim(),
                assignee_id: assigneeId,
                reminder_minutes: reminder,
            });
            onSaved(saved, "Event updated.");
        } catch (cause) {
            if (cause instanceof ApiError && cause.isValidationError && cause.details) {
                setErrors(cause.details);
            } else {
                setErrors({ form: cause instanceof ApiError ? cause.message : "Could not save the event." });
            }
        } finally {
            setSaving(false);
        }
    };

    const slotError = errors.start_time ?? errors.duration_minutes;
    const status = TASK_STATUS_STYLES[task.status];

    return (
        <form onSubmit={submit} noValidate>
            <ModalBody>
                {/* Status, and the moves that change it. */}
                <div className="flex flex-wrap items-center gap-2 rounded-lg bg-slate-50 px-3 py-2 dark:bg-slate-800/50">
                    <span className={cx("rounded-full px-2 py-0.5 text-xs font-medium ring-1 ring-inset", status.badge)}>
                        {status.label}
                    </span>
                    {confirming === "cancel" ? (
                        <div className="ml-auto flex flex-wrap items-center gap-2" role="group" aria-label="Confirm cancel">
                            <span className="text-sm text-slate-700 dark:text-slate-300">Cancel this event?</span>
                            <Button variant="danger" size="sm" loading={acting} onClick={() => changeStatus("cancel")} autoFocus>
                                Cancel event
                            </Button>
                            <Button variant="ghost" size="sm" disabled={acting} onClick={() => setConfirming(null)}>
                                Keep it
                            </Button>
                        </div>
                    ) : (
                        <div className="ml-auto flex flex-wrap gap-2">
                            {task.status === "cancelled" ? (
                                <Button size="sm" disabled={busy} onClick={() => changeStatus("restore")}>
                                    <RotateCcw className="size-3.5" aria-hidden="true" />
                                    Restore
                                </Button>
                            ) : task.status === "completed" ? (
                                <Button size="sm" disabled={busy} onClick={() => changeStatus("reopen")}>
                                    <RotateCcw className="size-3.5" aria-hidden="true" />
                                    Reopen
                                </Button>
                            ) : (
                                <>
                                    <Button size="sm" disabled={busy} onClick={() => changeStatus("complete")}>
                                        <Check className="size-3.5" aria-hidden="true" />
                                        Mark complete
                                    </Button>
                                    <Button size="sm" variant="ghost" disabled={busy} onClick={() => setConfirming("cancel")}>
                                        <Ban className="size-3.5" aria-hidden="true" />
                                        Cancel event
                                    </Button>
                                </>
                            )}
                        </div>
                    )}
                </div>

                <Field id="edit-task-title" label="Title" required error={errors.title}>
                    <TextInput
                        id="edit-task-title"
                        value={title}
                        maxLength={200}
                        invalid={Boolean(errors.title)}
                        onChange={(event) => setTitle(event.target.value)}
                        autoFocus
                    />
                </Field>

                <div className="space-y-1.5">
                    <p className="text-xs font-medium text-slate-700 dark:text-slate-300">Type</p>
                    <TaskTypePicker value={type} onChange={setType} />
                    {errors.type && <p className="text-xs text-rose-600 dark:text-rose-400">{errors.type}</p>}
                </div>

                <Field id="edit-task-date" label="Date" required error={errors.due_date}>
                    <TextInput
                        id="edit-task-date"
                        type="date"
                        value={dueDate}
                        invalid={Boolean(errors.due_date)}
                        onChange={(event) => setDueDate(event.target.value)}
                    />
                </Field>

                <div className="space-y-1.5">
                    <p className="text-xs font-medium text-slate-700 dark:text-slate-300">Time</p>
                    <TimeSlotPicker
                        idPrefix="edit-task"
                        startTime={startTime}
                        duration={duration}
                        onChange={(nextStart, nextDuration) => {
                            setStartTime(nextStart);
                            setDuration(nextDuration);
                        }}
                    />
                    {slotError && <p className="text-xs text-rose-600 dark:text-rose-400">{slotError}</p>}
                </div>

                <div className="grid gap-4 sm:grid-cols-2">
                    <Field id="edit-task-assignee" label="Assigned to" error={errors.assignee_id}>
                        <AssigneeSelect
                            id="edit-task-assignee"
                            value={assigneeId}
                            onChange={setAssigneeId}
                            team={team.data}
                            loading={team.loading}
                            invalid={Boolean(errors.assignee_id)}
                        />
                    </Field>
                    <Field
                        id="edit-task-reminder"
                        label="Reminder"
                        error={errors.reminder_minutes}
                        hint={
                            closed
                                ? "Completed and cancelled events are not reminded about."
                                : startTime
                                  ? undefined
                                  : "All-day events are reminded about from 9:00 AM."
                        }
                    >
                        <ReminderSelect
                            id="edit-task-reminder"
                            value={reminder}
                            onChange={setReminder}
                            invalid={Boolean(errors.reminder_minutes)}
                        />
                    </Field>
                </div>

                <Field id="edit-task-notes" label="Notes / agenda" error={errors.notes}>
                    <Textarea
                        id="edit-task-notes"
                        rows={3}
                        maxLength={2000}
                        value={notes}
                        invalid={Boolean(errors.notes)}
                        placeholder="What to cover, links, dial-in details…"
                        onChange={(event) => setNotes(event.target.value)}
                    />
                </Field>

                {errors.form && (
                    <p className="text-sm text-rose-600 dark:text-rose-400" role="alert">
                        {errors.form}
                    </p>
                )}
            </ModalBody>
            <ModalFooter>
                {confirming === "delete" ? (
                    <div className="flex flex-wrap items-center gap-2" role="group" aria-label="Confirm delete">
                        <span className="text-sm text-slate-700 dark:text-slate-300">Delete this event?</span>
                        <Button variant="danger" size="sm" loading={acting} onClick={remove} autoFocus>
                            Delete
                        </Button>
                        <Button variant="ghost" size="sm" disabled={acting} onClick={() => setConfirming(null)}>
                            Keep
                        </Button>
                    </div>
                ) : (
                    <Button
                        variant="ghost"
                        className="text-rose-600 hover:bg-rose-50 hover:text-rose-700 dark:text-rose-400 dark:hover:bg-rose-950/40 dark:hover:text-rose-300"
                        disabled={busy}
                        onClick={() => setConfirming("delete")}
                    >
                        <Trash2 className="size-4" aria-hidden="true" />
                        Delete
                    </Button>
                )}

                <div className="ml-auto flex gap-2">
                    <Button onClick={onCancel}>Close</Button>
                    <Button type="submit" variant="primary" loading={saving} disabled={busy}>
                        Save changes
                    </Button>
                </div>
            </ModalFooter>
        </form>
    );
}
