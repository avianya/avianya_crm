import { TASK_TYPES, TASK_TYPE_STYLES, cx } from "../lib/display";
import type { TaskType } from "../lib/types";

/** A row of pills for choosing what kind of task this is. */
export function TaskTypePicker({
    value,
    onChange,
    compact = false,
}: {
    value: TaskType;
    onChange: (type: TaskType) => void;
    compact?: boolean;
}) {
    return (
        <div className="inline-flex flex-wrap gap-1" role="radiogroup" aria-label="Task type">
            {TASK_TYPES.map((type) => {
                const { label, icon: Icon } = TASK_TYPE_STYLES[type];
                const active = value === type;
                return (
                    <button
                        key={type}
                        type="button"
                        role="radio"
                        aria-checked={active}
                        onClick={() => onChange(type)}
                        className={cx(
                            "inline-flex items-center gap-1 rounded-full font-medium ring-1 ring-inset transition-colors",
                            compact ? "px-2 py-0.5 text-xs" : "px-2.5 py-1 text-sm",
                            active
                                ? "bg-brand-50 text-brand-700 ring-brand-200 dark:bg-brand-950 dark:text-brand-300 dark:ring-brand-800"
                                : "text-slate-600 ring-slate-200 hover:bg-slate-50 dark:text-slate-300 dark:ring-slate-700 dark:hover:bg-slate-800",
                        )}
                    >
                        <Icon className="size-3.5" aria-hidden="true" />
                        {label}
                    </button>
                );
            })}
        </div>
    );
}

/**
 * A task's type as a small icon, with the type named for screen readers. To-do
 * is the default and needs no marker, so it renders nothing unless `always`.
 */
export function TaskTypeIcon({
    type,
    className = "size-3",
    always = false,
}: {
    type: TaskType;
    className?: string;
    always?: boolean;
}) {
    if (type === "todo" && !always) return null;
    const { label, icon: Icon } = TASK_TYPE_STYLES[type];
    return (
        <>
            <Icon className={cx("inline shrink-0", className)} aria-hidden="true" />
            <span className="sr-only">{label}: </span>
        </>
    );
}
