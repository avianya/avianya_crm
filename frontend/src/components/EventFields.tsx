import { REMINDER_OPTIONS, cx, formatReminder } from "../lib/display";
import type { TeamMember } from "../lib/types";
import { Select } from "./ui";

/** "No reminder" or one of the reminder lead times. */
export function ReminderSelect({
    id,
    value,
    onChange,
    compact = false,
    invalid,
}: {
    id: string;
    value: number | null;
    onChange: (minutes: number | null) => void;
    compact?: boolean;
    invalid?: boolean;
}) {
    const options = (
        <>
            <option value="">No reminder</option>
            {REMINDER_OPTIONS.map((minutes) => (
                <option key={minutes} value={minutes}>
                    {formatReminder(minutes)}
                </option>
            ))}
        </>
    );
    const change = (raw: string) => onChange(raw ? Number(raw) : null);

    if (compact) {
        return (
            <select
                id={id}
                value={value ?? ""}
                onChange={(event) => change(event.target.value)}
                className={cx(
                    "h-7 rounded-md border bg-white px-2 text-xs text-slate-700 dark:bg-slate-950 dark:text-slate-200",
                    invalid ? "border-rose-400" : "border-slate-300 dark:border-slate-700",
                )}
            >
                {options}
            </select>
        );
    }
    return (
        <Select id={id} value={value ?? ""} invalid={invalid} onChange={(event) => change(event.target.value)}>
            {options}
        </Select>
    );
}

/** Who the event is for. "Lead owner" when unassigned on create. */
export function AssigneeSelect({
    id,
    value,
    onChange,
    team,
    loading,
    emptyLabel = "Nobody",
    invalid,
}: {
    id: string;
    value: number | null;
    onChange: (memberId: number | null) => void;
    team: TeamMember[] | null;
    loading?: boolean;
    emptyLabel?: string;
    invalid?: boolean;
}) {
    return (
        <Select
            id={id}
            value={value ?? ""}
            invalid={invalid}
            disabled={loading}
            onChange={(event) => onChange(event.target.value ? Number(event.target.value) : null)}
        >
            <option value="">{loading ? "Loading team…" : emptyLabel}</option>
            {(team ?? []).map((member) => (
                <option key={member.id} value={member.id}>
                    {member.name}
                    {member.email_reminders ? "" : " (email reminders off)"}
                </option>
            ))}
        </Select>
    );
}
