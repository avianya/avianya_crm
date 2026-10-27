/**
 * Presentation metadata and formatting helpers.
 *
 * This is the "how things look" layer: which colour a stage wears, which icon an
 * activity gets, how a timestamp is phrased. Keeping it out of the components
 * means the stage palette is defined once and every screen -- table, badge, chart,
 * step indicator -- stays visually consistent by construction.
 */

import {
    ArrowRightLeft,
    CalendarDays,
    CalendarX,
    CircleDashed,
    CalendarCheck,
    Gauge,
    Star,
    Globe,
    CircleCheck,
    Handshake,
    ListTodo,
    Mail,
    Megaphone,
    MessageSquare,
    Network,
    Phone,
    PhoneCall,
    Presentation,
    Repeat,
    Search,
    Send,
    Sparkles,
    Target,
    ThumbsDown,
    Trophy,
    UserCog,
    UserPlus,
    Users,
    Zap,
} from "lucide-react";
import type { LucideIcon } from "lucide-react";

import type {
    ActivityType,
    CapiHealth,
    CapiStatus,
    Health,
    QualificationStatus,
    Quality,
    QualityCategory,
    ScoreBand,
    ScoreFactor,
    ScorePenalty,
    Source,
    Stage,
    StageHistoryEntry,
    TaskStatus,
    TaskType,
} from "./types";

// ---------------------------------------------------------------------------
// Stages
// ---------------------------------------------------------------------------

interface StageStyle {
    label: string;
    /** Icon shown in the step indicator and the stage badge. */
    icon: LucideIcon;
    /** Tailwind classes for a pill/badge on a light surface. */
    badge: string;
    /** Solid fill, for chart bars and the active step in the indicator. */
    solid: string;
    /** Text-only colour, for the accented number in a stat card. */
    text: string;
    /** Border colour, for the timeline rail and card accents. */
    border: string;
}

/**
 * The stage palette.
 *
 * The hues are ordered to read as progress: cool and neutral at the start,
 * warming through the middle of the pipeline, resolving to green (won) or red
 * (lost). That way the leads table communicates pipeline health at a glance,
 * before you read a single word.
 *
 * Records are keyed by Stage, so TypeScript will refuse to compile if a stage is
 * added to the union and not given a style here -- the compiler enforces that
 * this stays complete.
 */
export const STAGE_STYLES: Record<Stage, StageStyle> = {
    new: {
        label: "New",
        icon: Sparkles,
        badge: "bg-slate-100 text-slate-700 ring-slate-200 dark:bg-slate-800 dark:text-slate-300 dark:ring-slate-700",
        solid: "bg-slate-400 dark:bg-slate-500",
        text: "text-slate-600 dark:text-slate-300",
        border: "border-slate-300 dark:border-slate-600",
    },
    contacted: {
        label: "Contacted",
        icon: PhoneCall,
        badge: "bg-sky-50 text-sky-700 ring-sky-200 dark:bg-sky-950 dark:text-sky-300 dark:ring-sky-800",
        solid: "bg-sky-500",
        text: "text-sky-600 dark:text-sky-400",
        border: "border-sky-300 dark:border-sky-700",
    },
    qualified: {
        label: "Qualified",
        icon: Target,
        badge: "bg-violet-50 text-violet-700 ring-violet-200 dark:bg-violet-950 dark:text-violet-300 dark:ring-violet-800",
        solid: "bg-violet-500",
        text: "text-violet-600 dark:text-violet-400",
        border: "border-violet-300 dark:border-violet-700",
    },
    proposal_sent: {
        label: "Proposal Sent",
        icon: Send,
        badge: "bg-amber-50 text-amber-700 ring-amber-200 dark:bg-amber-950 dark:text-amber-300 dark:ring-amber-800",
        solid: "bg-amber-500",
        text: "text-amber-600 dark:text-amber-400",
        border: "border-amber-300 dark:border-amber-700",
    },
    negotiation: {
        label: "Negotiation",
        icon: Handshake,
        badge: "bg-orange-50 text-orange-700 ring-orange-200 dark:bg-orange-950 dark:text-orange-300 dark:ring-orange-800",
        solid: "bg-orange-500",
        text: "text-orange-600 dark:text-orange-400",
        border: "border-orange-300 dark:border-orange-700",
    },
    won: {
        label: "Won",
        icon: Trophy,
        badge: "bg-emerald-50 text-emerald-700 ring-emerald-200 dark:bg-emerald-950 dark:text-emerald-300 dark:ring-emerald-800",
        solid: "bg-emerald-500",
        text: "text-emerald-600 dark:text-emerald-400",
        border: "border-emerald-300 dark:border-emerald-700",
    },
    lost: {
        label: "Lost",
        icon: ThumbsDown,
        badge: "bg-rose-50 text-rose-700 ring-rose-200 dark:bg-rose-950 dark:text-rose-300 dark:ring-rose-800",
        solid: "bg-rose-500",
        text: "text-rose-600 dark:text-rose-400",
        border: "border-rose-300 dark:border-rose-700",
    },
};

/**
 * Stages in lifecycle order, matching model.Stages in the backend and the
 * declaration order of the lead_stage Postgres enum.
 *
 * The step indicator walks this list, so the order here is the order a lead is
 * shown as progressing through.
 */
export const STAGE_ORDER: Stage[] = [
    "new",
    "contacted",
    "qualified",
    "proposal_sent",
    "negotiation",
    "won",
    "lost",
];

/** The five stages that make up the open pipeline, in order. */
export const PIPELINE_STAGES: Stage[] = STAGE_ORDER.slice(0, 5);

/** Won and Lost -- the two terminal stages. */
export const CLOSED_STAGES: Stage[] = ["won", "lost"];

export const isClosedStage = (stage: Stage): boolean => CLOSED_STAGES.includes(stage);

/** Position of a stage in the lifecycle, or -1 for the closed stages. */
export const pipelineIndex = (stage: Stage): number => PIPELINE_STAGES.indexOf(stage);

/**
 * The next stage to suggest advancing to, or null if there is nothing obvious.
 *
 * Powers the "Advance to X" button on the detail page: the common case is
 * "move this one forward", and it should not require picking from a dropdown.
 */
export function nextStage(stage: Stage): Stage | null {
    if (isClosedStage(stage)) return null;

    const index = pipelineIndex(stage);
    if (index === -1) return null;

    // The last open stage (Negotiation) advances to Won rather than off the end.
    return index === PIPELINE_STAGES.length - 1 ? "won" : PIPELINE_STAGES[index + 1];
}

// ---------------------------------------------------------------------------
// Sources
// ---------------------------------------------------------------------------

export const SOURCE_STYLES: Record<Source, { label: string; icon: LucideIcon }> = {
    website: { label: "Website", icon: Globe },
    referral: { label: "Referral", icon: Users },
    // lucide dropped its brand icons in v1, so LinkedIn gets a generic
    // "professional network" glyph rather than a pinned-version workaround.
    linkedin: { label: "LinkedIn", icon: Network },
    cold_call: { label: "Cold Call", icon: Phone },
    // Generic glyphs again: no brand icons in lucide.
    meta_ads: { label: "Meta Ads", icon: Megaphone },
    google_ads: { label: "Google Ads", icon: Search },
    other: { label: "Other", icon: CircleDashed },
};

// ---------------------------------------------------------------------------
// Activity types
// ---------------------------------------------------------------------------

interface ActivityStyle {
    label: string;
    icon: LucideIcon;
    /** Classes for the small round icon marker on the timeline. */
    marker: string;
}

export const ACTIVITY_STYLES: Record<ActivityType, ActivityStyle> = {
    created: {
        label: "Lead created",
        icon: UserPlus,
        marker: "bg-brand-100 text-brand-700 dark:bg-brand-950 dark:text-brand-300",
    },
    stage_change: {
        label: "Stage change",
        icon: ArrowRightLeft,
        marker:
            "bg-violet-100 text-violet-700 dark:bg-violet-950 dark:text-violet-300",
    },
    note: {
        label: "Note",
        icon: MessageSquare,
        marker: "bg-slate-100 text-slate-600 dark:bg-slate-800 dark:text-slate-300",
    },
    call: {
        label: "Call",
        icon: Phone,
        marker: "bg-sky-100 text-sky-700 dark:bg-sky-950 dark:text-sky-300",
    },
    email: {
        label: "Email",
        icon: Mail,
        marker: "bg-amber-100 text-amber-700 dark:bg-amber-950 dark:text-amber-300",
    },
    meeting: {
        label: "Meeting",
        icon: CalendarDays,
        marker:
            "bg-emerald-100 text-emerald-700 dark:bg-emerald-950 dark:text-emerald-300",
    },
    task_completed: {
        label: "Task completed",
        icon: CircleCheck,
        marker: "bg-teal-100 text-teal-700 dark:bg-teal-950 dark:text-teal-300",
    },
    task_cancelled: {
        label: "Event cancelled",
        icon: CalendarX,
        marker: "bg-rose-100 text-rose-700 dark:bg-rose-950 dark:text-rose-300",
    },
    owner_change: {
        label: "Owner change",
        icon: UserCog,
        marker: "bg-indigo-100 text-indigo-700 dark:bg-indigo-950 dark:text-indigo-300",
    },
    automation: {
        label: "Automation",
        icon: Zap,
        marker: "bg-amber-100 text-amber-700 dark:bg-amber-950 dark:text-amber-300",
    },
    ad_form: {
        label: "Ad form",
        icon: Megaphone,
        marker: "bg-fuchsia-100 text-fuchsia-700 dark:bg-fuchsia-950 dark:text-fuchsia-300",
    },
    quality_change: {
        label: "Quality",
        icon: Gauge,
        marker: "bg-lime-100 text-lime-700 dark:bg-lime-950 dark:text-lime-300",
    },
    booking: {
        label: "Meeting booked",
        icon: CalendarCheck,
        marker: "bg-cyan-100 text-cyan-700 dark:bg-cyan-950 dark:text-cyan-300",
    },
    feedback: {
        label: "Feedback",
        icon: Star,
        marker: "bg-yellow-100 text-yellow-700 dark:bg-yellow-950 dark:text-yellow-300",
    },
};

/**
 * Whether a timeline entry may be edited or deleted: only the kinds a user
 * logs. Entries the server wrote are a record of what happened. Mirrors
 * model.UserActivityTypes.
 */
export function isEditableActivity(type: ActivityType): boolean {
    return type === "note" || type === "call" || type === "email" || type === "meeting";
}

// ---------------------------------------------------------------------------
// Task types
// ---------------------------------------------------------------------------

/** Every task type, in the order the picker shows them. */
export const TASK_TYPES: TaskType[] = ["todo", "call", "email", "meeting", "demo", "follow_up"];

/**
 * Calls, emails and meetings borrow the timeline's icons, so a planned call and
 * the call it becomes once done look the same.
 */
export const TASK_TYPE_STYLES: Record<TaskType, { label: string; icon: LucideIcon }> = {
    todo: { label: "To-do", icon: ListTodo },
    call: { label: "Call", icon: ACTIVITY_STYLES.call.icon },
    email: { label: "Email", icon: ACTIVITY_STYLES.email.icon },
    meeting: { label: "Meeting", icon: ACTIVITY_STYLES.meeting.icon },
    demo: { label: "Demo", icon: Presentation },
    follow_up: { label: "Follow-up", icon: Repeat },
};

/** Badges for an event's status. Upcoming is the normal case and gets none. */
export const TASK_STATUS_STYLES: Record<TaskStatus, { label: string; badge: string }> = {
    upcoming: {
        label: "Upcoming",
        badge: "bg-sky-50 text-sky-700 ring-sky-200 dark:bg-sky-950 dark:text-sky-300 dark:ring-sky-800",
    },
    overdue: {
        label: "Overdue",
        badge: "bg-rose-50 text-rose-700 ring-rose-200 dark:bg-rose-950 dark:text-rose-300 dark:ring-rose-800",
    },
    completed: {
        label: "Completed",
        badge: "bg-teal-50 text-teal-700 ring-teal-200 dark:bg-teal-950 dark:text-teal-300 dark:ring-teal-800",
    },
    cancelled: {
        label: "Cancelled",
        badge: "bg-slate-100 text-slate-600 ring-slate-200 dark:bg-slate-800 dark:text-slate-300 dark:ring-slate-700",
    },
};

/** Reminder lead times offered, in minutes. Matches model.ReminderOptions. */
export const REMINDER_OPTIONS = [5, 15, 30, 1440] as const;

/** "15 min before", "1 day before". */
export function formatReminder(minutes: number): string {
    if (minutes === 1440) return "1 day before";
    if (minutes % 60 === 0) return `${minutes / 60} h before`;
    return `${minutes} min before`;
}

// ---------------------------------------------------------------------------
// Formatting
// ---------------------------------------------------------------------------

/** e.g. "14 Aug 2026" */
export function formatDate(iso: string): string {
    return new Date(iso).toLocaleDateString(undefined, {
        day: "numeric",
        month: "short",
        year: "numeric",
    });
}

/** e.g. "14 Aug 2026, 5:39 pm" */
export function formatDateTime(iso: string): string {
    return new Date(iso).toLocaleString(undefined, {
        day: "numeric",
        month: "short",
        year: "numeric",
        hour: "numeric",
        minute: "2-digit",
    });
}

/**
 * A short relative time: "just now", "3h ago", "12 Aug".
 *
 * Relative labels are what make a timeline readable at a glance -- "2 days ago"
 * answers "is this stale?" without any mental arithmetic. Anything older than a
 * week falls back to an absolute date, because "37 days ago" does not.
 */
export function formatRelative(iso: string): string {
    const then = new Date(iso).getTime();
    const seconds = Math.round((Date.now() - then) / 1000);

    if (seconds < 60) return "just now";

    const minutes = Math.round(seconds / 60);
    if (minutes < 60) return `${minutes}m ago`;

    const hours = Math.round(minutes / 60);
    if (hours < 24) return `${hours}h ago`;

    const days = Math.round(hours / 24);
    if (days === 1) return "yesterday";
    if (days < 7) return `${days}d ago`;

    return formatDate(iso);
}

/**
 * Up to two initials from a name, for the avatar circles.
 * "Ananya Sharma" -> "AS", "Cher" -> "C".
 */
export function initials(name: string): string {
    const words = name.trim().split(/\s+/).filter(Boolean);
    if (words.length === 0) return "?";

    const first = words[0][0];
    const last = words.length > 1 ? words[words.length - 1][0] : "";

    return (first + last).toUpperCase();
}

/**
 * A deterministic avatar colour derived from the name.
 *
 * Deterministic matters: the same person must keep the same colour on every
 * screen and every reload, which rules out anything random. A tiny string hash
 * gives a stable index into the palette.
 */
export function avatarColor(name: string): string {
    const palette = [
        "bg-brand-100 text-brand-700 dark:bg-brand-950 dark:text-brand-300",
        "bg-sky-100 text-sky-700 dark:bg-sky-950 dark:text-sky-300",
        "bg-emerald-100 text-emerald-700 dark:bg-emerald-950 dark:text-emerald-300",
        "bg-amber-100 text-amber-700 dark:bg-amber-950 dark:text-amber-300",
        "bg-rose-100 text-rose-700 dark:bg-rose-950 dark:text-rose-300",
        "bg-violet-100 text-violet-700 dark:bg-violet-950 dark:text-violet-300",
        "bg-teal-100 text-teal-700 dark:bg-teal-950 dark:text-teal-300",
    ];

    let hash = 0;
    for (let i = 0; i < name.length; i++) {
        hash = (hash * 31 + name.charCodeAt(i)) | 0; // |0 keeps it a 32-bit int
    }

    return palette[Math.abs(hash) % palette.length];
}

// ---------------------------------------------------------------------------
// Lead health and score
// ---------------------------------------------------------------------------

export const HEALTH_STYLES: Record<Health, { label: string; dot: string; badge: string }> = {
    active: {
        label: "Active",
        dot: "bg-emerald-500",
        badge: "bg-emerald-50 text-emerald-700 ring-emerald-200 dark:bg-emerald-950 dark:text-emerald-300 dark:ring-emerald-800",
    },
    cooling: {
        label: "Cooling",
        dot: "bg-amber-500",
        badge: "bg-amber-50 text-amber-700 ring-amber-200 dark:bg-amber-950 dark:text-amber-300 dark:ring-amber-800",
    },
    cold: {
        label: "Cold",
        dot: "bg-sky-500",
        badge: "bg-sky-50 text-sky-700 ring-sky-200 dark:bg-sky-950 dark:text-sky-300 dark:ring-sky-800",
    },
    closed: {
        label: "Closed",
        dot: "bg-slate-300 dark:bg-slate-600",
        badge: "bg-slate-100 text-slate-500 ring-slate-200 dark:bg-slate-800 dark:text-slate-400 dark:ring-slate-700",
    },
};

/** "Touched today", "1 day since contact", "9 days since contact". */
export function formatSilence(days: number): string {
    if (days === 0) return "Touched today";
    return `${days} ${days === 1 ? "day" : "days"} since contact`;
}

/**
 * Score bands, hottest first. The cut-offs live on the server (model.BandFor);
 * scoreBand repeats them only to colour a bare number.
 */
export const BAND_ORDER: ScoreBand[] = ["hot", "warm", "cold"];

export const BAND_STYLES: Record<ScoreBand, { label: string; tone: string; badge: string }> = {
    hot: {
        label: "Hot",
        tone: "text-orange-600 dark:text-orange-400",
        badge: "bg-orange-50 text-orange-700 ring-orange-200 dark:bg-orange-950 dark:text-orange-300 dark:ring-orange-800",
    },
    warm: {
        label: "Warm",
        tone: "text-amber-600 dark:text-amber-400",
        badge: "bg-amber-50 text-amber-700 ring-amber-200 dark:bg-amber-950 dark:text-amber-300 dark:ring-amber-800",
    },
    cold: {
        label: "Cold",
        tone: "text-slate-500 dark:text-slate-400",
        badge: "bg-slate-100 text-slate-600 ring-slate-200 dark:bg-slate-800 dark:text-slate-300 dark:ring-slate-700",
    },
};

export function scoreBand(total: number): ScoreBand {
    if (total >= 70) return "hot";
    if (total >= 40) return "warm";
    return "cold";
}

/** Colour for a 0-100 score: the same three bands everywhere it appears. */
export function scoreTone(total: number): string {
    return BAND_STYLES[scoreBand(total)].tone;
}

/**
 * What to do to earn a factor's missing points.
 *
 * This is what turns the score from a grade into a to-do list: every gap comes
 * with the action that closes it.
 */
export const SCORE_TIPS: Record<ScoreFactor["key"], string> = {
    stage: "Move the lead forward in the pipeline.",
    recent_contact: "Log a call, email, meeting or note.",
    engagement: "Book a call or meeting. Four conversations in 30 days earns full points.",
    source: "Referrals score highest. Ask happy customers to introduce you.",
    next_step: "Add a follow-up task.",
    deal_value: "Estimate the deal value.",
    phone: "Add a phone number.",
    notes: "Write a few lines of context in Notes.",
};

/** How to clear each penalty and get its points back. */
export const PENALTY_TIPS: Record<ScorePenalty["key"], string> = {
    overdue_tasks: "Complete, reschedule or cancel the overdue tasks.",
    stuck: "Move the deal to its next stage, or mark it lost.",
};

// ---------------------------------------------------------------------------
// Money and due dates
// ---------------------------------------------------------------------------

const RUPEES = new Intl.NumberFormat("en-IN", {
    style: "currency",
    currency: "INR",
    maximumFractionDigits: 0,
});

const RUPEES_COMPACT = new Intl.NumberFormat("en-IN", {
    style: "currency",
    currency: "INR",
    notation: "compact",
    maximumFractionDigits: 1,
});

/** e.g. "₹2,50,000" -- Indian digit grouping, whole rupees. */
export function formatMoney(value: number): string {
    return RUPEES.format(value);
}

/** e.g. "₹2.5L", "₹1.2Cr" -- for stat cards where space is tight. */
export function formatMoneyCompact(value: number): string {
    return RUPEES_COMPACT.format(value);
}

/**
 * Today's date as "YYYY-MM-DD" in the viewer's own time zone.
 *
 * Built by hand because toISOString() is UTC: just after midnight in India it
 * would still say yesterday.
 */
export function todayISODate(): string {
    const now = new Date();
    const month = String(now.getMonth() + 1).padStart(2, "0");
    const day = String(now.getDate()).padStart(2, "0");
    return `${now.getFullYear()}-${month}-${day}`;
}

/** Slot lengths offered when scheduling a task, in minutes. */
export const TASK_DURATIONS = [15, 30, 45, 60, 90, 120, 180, 240];

/** Matches model.DefaultTaskDuration on the server. */
export const DEFAULT_TASK_DURATION = 30;

/** Minutes since midnight for an "HH:MM" time. */
export function minutesOfDay(time: string): number {
    const [hours, minutes] = time.split(":").map(Number);
    return hours * 60 + minutes;
}

/** "HH:MM" for a number of minutes since midnight, wrapping past 24:00. */
export function timeFromMinutes(total: number): string {
    const wrapped = ((total % 1440) + 1440) % 1440;
    const hours = String(Math.floor(wrapped / 60)).padStart(2, "0");
    const minutes = String(wrapped % 60).padStart(2, "0");
    return `${hours}:${minutes}`;
}

/** "14:30" as the viewer's locale writes it, e.g. "2:30 PM". */
export function formatClock(time: string): string {
    const [hours, minutes] = time.split(":").map(Number);
    return new Date(2000, 0, 1, hours, minutes).toLocaleTimeString(undefined, {
        hour: "numeric",
        minute: "2-digit",
    });
}

/** "2:30 PM – 3:00 PM" for a timed task, null for an all-day one. */
export function formatTimeSlot(task: { start_time: string | null; duration_minutes: number | null }): string | null {
    if (!task.start_time) return null;
    const end = timeFromMinutes(minutesOfDay(task.start_time) + (task.duration_minutes ?? DEFAULT_TASK_DURATION));
    return `${formatClock(task.start_time)} – ${formatClock(end)}`;
}

/** "30 min", "1 hr", "1 hr 30 min". */
export function formatDuration(minutes: number): string {
    const hours = Math.floor(minutes / 60);
    const rest = minutes % 60;
    if (hours === 0) return `${rest} min`;
    return rest === 0 ? `${hours} hr` : `${hours} hr ${rest} min`;
}

/** A "YYYY-MM-DD" date moved by whole days, without time zones getting involved. */
export function addDaysISO(isoDate: string, days: number): string {
    const [year, month, day] = isoDate.split("-").map(Number);
    const moved = new Date(Date.UTC(year, month - 1, day + days));
    return moved.toISOString().slice(0, 10);
}

/** Whole days from today to a "YYYY-MM-DD" date: negative means overdue. */
export function daysUntil(isoDate: string): number {
    const [year, month, day] = isoDate.split("-").map(Number);
    const [ty, tm, td] = todayISODate().split("-").map(Number);
    // Both sides as UTC midnights, so a daylight-saving shift cannot turn a
    // whole day into 23 or 25 hours.
    return Math.round((Date.UTC(year, month - 1, day) - Date.UTC(ty, tm - 1, td)) / 86_400_000);
}

/** "Overdue by 2 days", "Due today", "Due tomorrow", "Due in 5 days". */
export function formatDue(isoDate: string): string {
    const days = daysUntil(isoDate);
    if (days < -1) return `Overdue by ${-days} days`;
    if (days === -1) return "Overdue by 1 day";
    if (days === 0) return "Due today";
    if (days === 1) return "Due tomorrow";
    if (days < 7) return `Due in ${days} days`;

    // Parsed as a local date: new Date("YYYY-MM-DD") is UTC midnight, which
    // displays as the previous day anywhere west of Greenwich.
    const [year, month, day] = isoDate.split("-").map(Number);
    return `Due ${new Date(year, month - 1, day).toLocaleDateString(undefined, {
        day: "numeric",
        month: "short",
    })}`;
}

/** A length of time in the largest unit that reads naturally: "3 days", "5 hours". */
export function formatSpan(ms: number): string {
    const minutes = Math.floor(ms / 60_000);
    if (minutes < 1) return "under a minute";
    if (minutes < 60) return minutes === 1 ? "1 minute" : `${minutes} minutes`;
    const hours = Math.floor(minutes / 60);
    if (hours < 24) return hours === 1 ? "1 hour" : `${hours} hours`;
    const days = Math.floor(hours / 24);
    return days === 1 ? "1 day" : `${days} days`;
}

/**
 * The open stage a closed lead was in just before it was won or lost, or null
 * when its history does not say (e.g. it was created straight into Lost).
 */
export function stageBeforeClosing(history: StageHistoryEntry[] | undefined): Stage | null {
    const last = history?.[history.length - 1];
    if (!last || !isClosedStage(last.to_stage)) return null;
    return last.from_stage && !isClosedStage(last.from_stage) ? last.from_stage : null;
}

/** Joins conditional class names, dropping anything falsy. */
export function cx(...classes: (string | false | null | undefined)[]): string {
    return classes.filter(Boolean).join(" ");
}

// ---------------------------------------------------------------------------
// Lead quality and Meta Conversions API
// ---------------------------------------------------------------------------

const TONE = {
    green: "bg-emerald-50 text-emerald-700 ring-emerald-200 dark:bg-emerald-950 dark:text-emerald-300 dark:ring-emerald-800",
    amber: "bg-amber-50 text-amber-700 ring-amber-200 dark:bg-amber-950/60 dark:text-amber-300 dark:ring-amber-800",
    red: "bg-rose-50 text-rose-700 ring-rose-200 dark:bg-rose-950 dark:text-rose-300 dark:ring-rose-800",
    sky: "bg-sky-50 text-sky-700 ring-sky-200 dark:bg-sky-950 dark:text-sky-300 dark:ring-sky-800",
    grey: "bg-slate-100 text-slate-600 ring-slate-200 dark:bg-slate-800 dark:text-slate-300 dark:ring-slate-700",
};

export const QUALITY_ORDER: Quality[] = ["good", "average", "poor", "not_evaluated"];

export const QUALITY_STYLES: Record<Quality, { label: string; badge: string }> = {
    good: { label: "Good", badge: TONE.green },
    average: { label: "Average", badge: TONE.amber },
    poor: { label: "Poor", badge: TONE.red },
    not_evaluated: { label: "Not evaluated", badge: TONE.grey },
};

export const QUALIFICATION_STYLES: Record<QualificationStatus, { label: string; badge: string }> = {
    pending: { label: "Pending", badge: TONE.grey },
    qualified: { label: "Qualified", badge: TONE.green },
    not_qualified: { label: "Not qualified", badge: TONE.red },
};

export const QUALITY_CATEGORY_LABELS: Record<QualityCategory, string> = {
    contact_validity: "Contact details",
    service_fit: "Service fit",
    budget_fit: "Budget fit",
    follow_up_response: "Follow-up response",
    meeting_attendance: "Meeting attendance",
    other: "Other",
};

export const CAPI_STATUS_ORDER: CapiStatus[] = ["pending", "retry_scheduled", "sent", "failed", "cancelled"];

/** One conversion event's delivery. */
export const CAPI_STATUS_STYLES: Record<CapiStatus, { label: string; badge: string }> = {
    pending: { label: "Pending", badge: TONE.sky },
    retry_scheduled: { label: "Retry scheduled", badge: TONE.amber },
    sent: { label: "Sent", badge: TONE.green },
    failed: { label: "Failed", badge: TONE.red },
    cancelled: { label: "Not sent", badge: TONE.grey },
};

/** The Conversions API integration as a whole. */
export const CAPI_HEALTH_STYLES: Record<CapiHealth, { label: string; badge: string; dot: string }> = {
    connected: { label: "Connected", badge: TONE.green, dot: "bg-emerald-500" },
    invalid_credentials: { label: "Invalid credentials", badge: TONE.red, dot: "bg-rose-500" },
    permission_denied: { label: "Permission denied", badge: TONE.red, dot: "bg-rose-500" },
    temporarily_unavailable: { label: "Temporarily unavailable", badge: TONE.amber, dot: "bg-amber-500" },
    not_configured: { label: "Not configured", badge: TONE.grey, dot: "bg-slate-400" },
    disabled: { label: "Disabled", badge: TONE.grey, dot: "bg-slate-400" },
    untested: { label: "Not tested yet", badge: TONE.sky, dot: "bg-sky-500" },
};
