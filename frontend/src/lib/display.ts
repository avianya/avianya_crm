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
    CircleDashed,
    Globe,
    Handshake,
    Mail,
    MessageSquare,
    Network,
    Phone,
    PhoneCall,
    Send,
    Sparkles,
    Target,
    ThumbsDown,
    Trophy,
    UserPlus,
    Users,
} from "lucide-react";
import type { LucideIcon } from "lucide-react";

import type { ActivityType, Source, Stage } from "./types";

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
};

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

/** Joins conditional class names, dropping anything falsy. */
export function cx(...classes: (string | false | null | undefined)[]): string {
    return classes.filter(Boolean).join(" ");
}
