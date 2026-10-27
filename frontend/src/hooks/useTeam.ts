import { api } from "../lib/api";
import type { TeamMember } from "../lib/types";
import { useAsync } from "./useAsync";

/**
 * One shared request for the team list. Owner and assignee dropdowns appear
 * in several places on one page, and the team changes rarely, so they all read
 * the same promise instead of asking the server each time. The Team page calls
 * invalidateTeam() after any change.
 */
let cached: Promise<TeamMember[]> | null = null;

export function invalidateTeam() {
    cached = null;
}

function loadTeam(): Promise<TeamMember[]> {
    if (!cached) {
        cached = api.listTeam().catch((cause: unknown) => {
            cached = null; // let the next caller try again
            throw cause;
        });
    }
    return cached;
}

/** The team members, sorted by name. `data` is null until loaded. */
export function useTeam() {
    return useAsync(() => loadTeam(), []);
}

/** The member's name, or null when the id is unknown or not set. */
export function memberName(team: TeamMember[] | null, id: number | null | undefined): string | null {
    if (id == null || !team) return null;
    return team.find((member) => member.id === id)?.name ?? null;
}

/**
 * The reminder a new event starts with: the default of the person it is for.
 * No person, or no default, means no reminder.
 */
export function defaultReminderFor(team: TeamMember[] | null, memberId: number | null | undefined): number | null {
    if (memberId == null || !team) return null;
    return team.find((member) => member.id === memberId)?.default_reminder_minutes ?? null;
}
