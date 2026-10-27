import { TASK_DURATIONS, cx, formatClock, formatDuration, timeFromMinutes } from "../lib/display";

/** Start times every 15 minutes, as a meeting scheduler offers them. */
const START_TIMES = Array.from({ length: 96 }, (_, index) => timeFromMinutes(index * 15));

/**
 * Picks a task's time slot: "All day", or a start time plus a duration.
 *
 * `startTime` is "" for all day. Choosing a time on an all-day task starts it
 * at 9:00, the most common first slot of a working day.
 */
export function TimeSlotPicker({
    idPrefix,
    startTime,
    duration,
    onChange,
    compact = false,
}: {
    idPrefix: string;
    startTime: string;
    duration: number;
    onChange: (startTime: string, duration: number) => void;
    compact?: boolean;
}) {
    const allDay = startTime === "";
    const control = cx(
        "rounded-md border border-slate-300 bg-white text-slate-700 dark:border-slate-700 dark:bg-slate-950 dark:text-slate-200",
        compact ? "h-7 px-2 text-xs" : "h-10 px-3 text-sm",
        "disabled:opacity-50",
    );

    return (
        <div className="flex flex-wrap items-center gap-2">
            <label
                className={cx(
                    "inline-flex cursor-pointer items-center gap-1.5 text-slate-600 dark:text-slate-300",
                    compact ? "text-xs" : "text-sm",
                )}
            >
                <input
                    type="checkbox"
                    checked={allDay}
                    onChange={(event) => onChange(event.target.checked ? "" : "09:00", duration)}
                    className="size-3.5 rounded border-slate-300 accent-brand-600"
                />
                All day
            </label>

            {!allDay && (
                <>
                    <label htmlFor={`${idPrefix}-start`} className="sr-only">
                        Start time
                    </label>
                    <select
                        id={`${idPrefix}-start`}
                        value={startTime}
                        onChange={(event) => onChange(event.target.value, duration)}
                        className={control}
                    >
                        {/* A time typed elsewhere may not be on the 15-minute grid. */}
                        {!START_TIMES.includes(startTime) && <option value={startTime}>{formatClock(startTime)}</option>}
                        {START_TIMES.map((time) => (
                            <option key={time} value={time}>
                                {formatClock(time)}
                            </option>
                        ))}
                    </select>

                    <label htmlFor={`${idPrefix}-duration`} className="sr-only">
                        Duration
                    </label>
                    <select
                        id={`${idPrefix}-duration`}
                        value={duration}
                        onChange={(event) => onChange(startTime, Number(event.target.value))}
                        className={control}
                    >
                        {TASK_DURATIONS.map((minutes) => (
                            <option key={minutes} value={minutes}>
                                {formatDuration(minutes)}
                            </option>
                        ))}
                    </select>
                </>
            )}
        </div>
    );
}
