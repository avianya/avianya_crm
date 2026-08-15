/**
 * TypeScript mirrors of the Go structs in backend/internal/model.
 *
 * These are hand-written rather than generated, so they have to be kept in step
 * with the backend by hand. The tradeoff is deliberate for a project this size:
 * one small file to review beats a code-generation step in the build. Every type
 * here names the Go type it mirrors so the pair is easy to find.
 */

/** Mirrors model.Stage. Values match the lead_stage Postgres enum. */
export type Stage =
    | "new"
    | "contacted"
    | "qualified"
    | "proposal_sent"
    | "negotiation"
    | "won"
    | "lost";

/** Mirrors model.Source. */
export type Source = "website" | "referral" | "linkedin" | "cold_call" | "other";

/**
 * Mirrors model.ActivityType.
 *
 * "created" and "stage_change" are written by the server and appear in responses,
 * but the API rejects them on input -- see UserActivityType below.
 */
export type ActivityType =
    | "created"
    | "stage_change"
    | "note"
    | "call"
    | "email"
    | "meeting";

/** The subset of activity types a user is allowed to log. */
export type UserActivityType = "note" | "call" | "email" | "meeting";

/** Mirrors model.Lead. */
export interface Lead {
    id: number;
    name: string;
    company: string;
    email: string;
    /** null when no phone was recorded (SQL NULL). */
    phone: string | null;
    source: Source;
    stage: Stage;
    notes: string;
    created_at: string; // RFC 3339 timestamp
    updated_at: string;
    /** Present only on the single-lead endpoint, which returns the full timeline. */
    activities?: Activity[];
}

/** Mirrors model.Activity. */
export interface Activity {
    id: number;
    lead_id: number;
    type: ActivityType;
    note: string;
    created_at: string;
}

/** Mirrors model.StageCount. */
export interface StageCount {
    stage: Stage;
    label: string;
    count: number;
}

/** Mirrors model.DashboardSummary. */
export interface DashboardSummary {
    total_leads: number;
    /** Always one entry per stage, in lifecycle order, zeros included. */
    by_stage: StageCount[];
    won_count: number;
    lost_count: number;
    open_count: number;
    conversion_rate: number;
    added_last_7_days: number;
    recent_leads: Lead[];
}

/** Mirrors model.PageMeta. */
export interface PageMeta {
    page: number;
    page_size: number;
    total_items: number;
    total_pages: number;
}

/** Request body for POST /api/leads and PUT /api/leads/:id (model.LeadInput). */
export interface LeadInput {
    name: string;
    company: string;
    email: string;
    phone: string;
    source: Source;
    stage: Stage;
    notes: string;
}

/** Request body for POST /api/leads/:id/activities (model.ActivityInput). */
export interface ActivityInput {
    type: UserActivityType;
    note: string;
}

/** One entry of the /api/meta vocabularies. */
export interface MetaOption<T extends string> {
    value: T;
    label: string;
}

/** Response of GET /api/meta. */
export interface Meta {
    stages: MetaOption<Stage>[];
    sources: MetaOption<Source>[];
    activity_types: MetaOption<UserActivityType>[];
}

/** Query parameters accepted by GET /api/leads. */
export interface LeadListParams {
    search?: string;
    stage?: Stage | "all";
    source?: Source | "all";
    sort_by?: "created_at" | "updated_at" | "name" | "company" | "stage";
    sort_dir?: "asc" | "desc";
    page?: number;
    page_size?: number;
}

/** A paginated list response: { data, pagination }. */
export interface Paginated<T> {
    data: T[];
    pagination: PageMeta;
}
