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
export type Source = "website" | "referral" | "linkedin" | "cold_call" | "meta_ads" | "google_ads" | "other";

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
    | "meeting"
    | "task_completed"
    | "task_cancelled"
    | "owner_change"
    | "automation"
    | "ad_form"
    | "quality_change"
    | "booking"
    | "feedback";

/** The subset of activity types a user is allowed to log. */
export type UserActivityType = "note" | "call" | "email" | "meeting";

/** Mirrors model.Health. */
export type Health = "active" | "cooling" | "cold" | "closed";

/** Mirrors model.ScoreFactor. */
export interface ScoreFactor {
    key:
        | "stage"
        | "recent_contact"
        | "engagement"
        | "source"
        | "next_step"
        | "deal_value"
        | "phone"
        | "notes";
    label: string;
    points: number;
    max: number;
}

/** Mirrors model.ScorePenalty. points is negative; only penalties that apply are sent. */
export interface ScorePenalty {
    key: "overdue_tasks" | "stuck";
    label: string;
    points: number;
}

/**
 * Mirrors model.LeadScore. total is always the factor points plus the
 * (negative) penalty points, floored at 0.
 */
export interface LeadScore {
    total: number;
    band: ScoreBand;
    factors: ScoreFactor[];
    penalties: ScorePenalty[];
}

/** Mirrors model.ScoreBand: hot is 70+, warm 40-69, cold below 40. */
export type ScoreBand = "hot" | "warm" | "cold";

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
    /** Expected deal size in whole rupees; null when not estimated yet. */
    deal_value: number | null;
    /** The team member who owns the lead; null when unowned. */
    owner_id: number | null;
    /** Why a Won or Lost deal closed; null for open leads, and closed ones given no reason. */
    close_reason: CloseReason | null;
    close_note: string | null;
    created_at: string; // RFC 3339 timestamp
    updated_at: string;
    /** Derived on every read, never stored: see backend/internal/model/insights.go. */
    last_touch_at: string;
    days_since_touch: number;
    health: Health;
    has_open_task: boolean;
    /** null for Won and Lost leads. */
    score: LeadScore | null;
    /** When the lead entered its current stage, and how many whole days ago. */
    stage_entered_at: string;
    days_in_stage: number;
    /** Days allowed in this stage before the deal counts as stuck; 0 for Won and Lost. */
    stuck_limit_days: number;
    /** Sitting in its stage for longer than stuck_limit_days. See model/stuck.go. */
    stuck: boolean;
    /** Present only on the single-lead endpoint, which returns the full timeline. */
    activities?: Activity[];
    /** Likewise single-lead only: every stage the lead has been in, oldest first. */
    stage_history?: StageHistoryEntry[];

    /** Lead quality: see backend/internal/model/quality.go. */
    quality: Quality;
    /** null when the lead could not be judged yet (Not Evaluated). */
    quality_score: number | null;
    quality_breakdown: QualityHit[];
    /** Why the lead has its quality, in a sentence. */
    quality_reason: string;
    quality_updated_at: string | null;
    quality_override: number | null;
    quality_override_reason: string | null;
    quality_override_at: string | null;
    /** Read from the stage on the server, so the two never disagree. */
    qualification_status: QualificationStatus;
    qualification_reason: string;
    spam_confirmed: boolean;
    /** This lead's events are never sent to Meta. */
    meta_share_opt_out: boolean;

    /** The latest conversion event sent to Meta; "" when none. */
    meta_capi_status: CapiStatus | "";
    meta_last_event_name: string;
    meta_last_event_at: string | null;
    meta_last_error_code: number | null;

    /** Single-lead endpoint only, for leads from Meta lead forms. */
    meta_attribution?: MetaAttribution;
    capi_events?: CapiEvent[];
}

/** Mirrors model.StageHistoryEntry: the lead entered to_stage at changed_at. */
export interface StageHistoryEntry {
    id: number;
    /** null on the first entry, which is the stage the lead was created in. */
    from_stage: Stage | null;
    to_stage: Stage;
    changed_at: string;
}

/** Mirrors model.Activity. */
export interface Activity {
    id: number;
    lead_id: number;
    type: ActivityType;
    note: string;
    created_at: string;
    /** Set once someone corrected the note; null if never edited. */
    edited_at: string | null;
}

/** Mirrors model.TaskType: what kind of follow-up a task is. */
export type TaskType = "todo" | "call" | "email" | "meeting" | "demo" | "follow_up";

/** Mirrors model.TaskStatus. Worked out by the server on every read. */
export type TaskStatus = "upcoming" | "overdue" | "completed" | "cancelled";

/** Mirrors model.Task. */
export interface Task {
    id: number;
    lead_id: number;
    title: string;
    /** Completing a call/email/meeting logs it on the timeline as that contact. */
    type: TaskType;
    /** A calendar date, "YYYY-MM-DD" -- no time of day, no time zone. */
    due_date: string;
    /** Wall-clock start, "HH:MM", or null for an all-day task. */
    start_time: string | null;
    /** Length of the slot in minutes; null exactly when start_time is. */
    duration_minutes: number | null;
    /** null while the task is open. */
    completed_at: string | null;
    created_at: string;
    /** Agenda or meeting details. */
    notes: string;
    /** The team member it is for, who gets its reminders. */
    assignee_id: number | null;
    /** Minutes before the start to remind (5, 15, 30, 1440); null for none. */
    reminder_minutes: number | null;
    /** When the reminder fires; null once completed or cancelled. */
    remind_at: string | null;
    cancelled_at: string | null;
    status: TaskStatus;
    /** Only set where tasks from many leads are listed (the dashboard). */
    lead_name?: string;
    lead_company?: string;
}

/** Mirrors model.StageCount. */
export interface StageCount {
    stage: Stage;
    label: string;
    count: number;
    /** Sum of deal values in this stage, in rupees. */
    value: number;
    /** Leads in this stage with no deal value yet. */
    unvalued_count: number;
    /** Win probability in percent. */
    probability: number;
    /** value x probability -- this stage's share of the forecast. */
    weighted: number;
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
    pipeline_value: number;
    weighted_forecast: number;
    won_value: number;
    /** Open tasks due today or earlier, most overdue first, at most 10. */
    due_tasks: Task[];
    overdue_tasks: number;
    due_today_tasks: number;
    /** Today's agenda: every task due today, open or done, timed ones first by start time. */
    today_tasks: Task[];
    open_leads_without_task: number;
    cooling_leads: number;
    cold_leads: number;
    /** Up to five open leads furthest past their stage's cooling threshold. */
    going_cold: Lead[];
    /** Open deals past their stage's time limit, and the five furthest past it. */
    stuck_count: number;
    stuck_deals: Lead[];
    /** Open leads in the Hot band, and the five highest-scoring open leads. */
    hot_count: number;
    hottest_leads: Lead[];
}

/** Mirrors model.PipelineColumn: one stage column of the board. */
export interface PipelineColumn {
    stage: Stage;
    label: string;
    probability: number;
    /** True totals for the whole stage, even when `leads` is capped. */
    count: number;
    value: number;
    weighted: number;
    /** At most 50 cards; open stages best score first, closed most recent first. */
    leads: Lead[];
}

/** Mirrors model.WeekCount. */
export interface WeekCount {
    /** The Monday the week starts on, "YYYY-MM-DD". */
    week_start: string;
    count: number;
}

/** Mirrors model.SourceReport. */
export interface SourceReport {
    source: Source;
    label: string;
    leads: number;
    won: number;
    lost: number;
    /** won / (won + lost) in percent; null when nothing has closed yet. */
    win_rate: number | null;
    won_value: number;
}

/** Mirrors model.StageDuration. */
export interface StageDuration {
    stage: Stage;
    label: string;
    /** Mean days per finished stint; null when no lead has left the stage. */
    avg_days: number | null;
    samples: number;
}

/** Mirrors model.ReportSummary (GET /api/reports). */
export interface ReportSummary {
    /** The last 12 weeks, oldest first, empty weeks included. */
    leads_per_week: WeekCount[];
    by_source: SourceReport[];
    stage_durations: StageDuration[];
    /** Lost and Won leads by why they closed, most deals first. */
    loss_reasons: ReasonCount[];
    win_reasons: ReasonCount[];
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
    deal_value: number | null;
    owner_id: number | null;
    /** Only with a Won or Lost stage; required when the lead becomes Lost. */
    close_reason?: CloseReason | null;
    close_note?: string;
}

/**
 * A code from Meta.lost_reasons or Meta.won_reasons (model.CloseReason). The
 * same code can mean different things on Won and Lost ("price"), so labels
 * are always looked up with the stage.
 */
export type CloseReason = string;

/** close_reason and close_note, sent with a move to Won or Lost. */
export interface CloseInput {
    close_reason: CloseReason | null;
    close_note: string;
}

/** Mirrors model.ReasonCount: one row of the win or loss breakdown. */
export interface ReasonCount {
    /** null for deals closed with no reason recorded. */
    reason: CloseReason | null;
    label: string;
    count: number;
    value: number;
}

/** Request body for POST /api/leads/:id/tasks and PUT /api/tasks/:id (model.TaskInput). */
export interface TaskInput {
    title: string;
    /** Defaults to "todo" on the server. */
    type?: TaskType;
    due_date: string;
    /** "HH:MM"; omit for an all-day task. */
    start_time?: string;
    /** Defaults to 30 on the server when start_time is given. */
    duration_minutes?: number;
    notes?: string;
    /** On create, null or omitted means the lead's owner; on edit, null unassigns. */
    assignee_id?: number | null;
    /** null or omitted means no reminder. */
    reminder_minutes?: number | null;
}

/** Mirrors model.TeamMember. */
export interface TeamMember {
    id: number;
    name: string;
    email: string;
    email_reminders: boolean;
    default_reminder_minutes: number | null;
    /** Gets the morning summary email. */
    daily_digest: boolean;
    /** Their account: role, whether they may log in, and whether a password is set. */
    role: Role;
    active: boolean;
    has_login: boolean;
    /** Signed themselves up; waiting for an admin to approve (switch on). */
    signup_pending: boolean;
    last_login_at: string | null;
    created_at: string;
    /** Their most recent daily summary; absent if none was ever attempted. */
    last_digest?: DigestRecord;
}

/** Mirrors model.DigestRecord: one day's summary email for one person. */
export interface DigestRecord {
    date: string;
    status: "sending" | "sent" | "failed" | "skipped";
    attempts: number;
    detail: string;
    updated_at: string;
}

/** Request body for POST /api/team and PUT /api/team/:id. */
export interface TeamMemberInput {
    name: string;
    email: string;
    email_reminders: boolean;
    default_reminder_minutes: number | null;
    daily_digest: boolean;
}

/** Mirrors model.EmailStatus. */
export type EmailStatus = "pending" | "sending" | "sent" | "failed" | "skipped";

/** Mirrors model.Notification: a reminder that has come due. */
export interface AppNotification {
    id: number;
    remind_at: string;
    task_id: number;
    task_title: string;
    task_type: TaskType;
    task_status: TaskStatus;
    due_date: string;
    start_time: string | null;
    duration_minutes: number | null;
    lead_id: number;
    lead_name: string;
    lead_company: string;
    recipient_id: number | null;
    recipient_name: string | null;
    recipient_email: string | null;
    created_at: string;
    read_at: string | null;
    email_status: EmailStatus;
    email_attempts: number;
    email_next_attempt_at: string | null;
    email_sent_at: string | null;
    /** Why it was skipped or failed, or who it was sent to. */
    email_detail: string;
}

/** Mirrors model.DealAlert: a deal that sat in one stage past its limit. */
export interface DealAlert {
    id: number;
    lead_id: number;
    lead_name: string;
    lead_company: string;
    owner_id: number | null;
    stage: Stage;
    stage_entered_at: string;
    limit_days: number;
    /** Days in the stage so far, or in all once resolved. */
    days_in_stage: number;
    /** The deal has since moved stage, or been won or lost. */
    resolved: boolean;
    created_at: string;
    read_at: string | null;
}

/** Mirrors model.NotificationList. */
export interface NotificationList {
    items: AppNotification[];
    /** Unread reminders. */
    unread: number;
    /** Stuck-deal alerts, still-stuck first. */
    deal_alerts: DealAlert[];
    /** Unread alerts whose deal is still stuck. */
    alerts_unread: number;
}

/** Mirrors model.ReminderSettings. Never contains credentials. */
export interface ReminderSettings {
    email_configured: boolean;
    from: string;
    timezone: string;
    /** The local hour (0-23) from which daily summaries are sent. */
    digest_hour: number;
}

/** Mirrors model.NextStep: one suggested follow-up for a lead. */
export interface NextStep {
    kind: "overdue" | "prepare" | "first_contact" | "no_reply" | "follow_up" | "plan" | "review";
    priority: "high" | "medium" | "low";
    title: string;
    detail: string;
    /** The event the suggestion is about, if any. */
    task_id?: number;
    /** Pre-fills the new event form when the suggestion is to schedule something. */
    suggest?: { type: TaskType; title: string; due_date: string };
}

/**
 * Request body for PUT /api/tasks/:id/schedule (model.TaskScheduleInput).
 * Replaces the whole slot: no start_time means all day.
 */
export interface TaskScheduleInput {
    due_date: string;
    start_time?: string;
    duration_minutes?: number;
}

/** One row of GET /api/tasks (model.TaskWithLead). */
export interface TaskWithLead {
    task: Task;
    /** The task's lead, with its health and score. */
    lead: Lead;
}

/** Mirrors model.FollowUpSummary: GET /api/follow-ups. */
export interface FollowUpSummary {
    /** The server's date the buckets were cut on, "YYYY-MM-DD". */
    today: string;
    /** Open tasks whose time has passed, most overdue first. */
    overdue: TaskWithLead[];
    /** The rest of today's open tasks. */
    due_today: TaskWithLead[];
    /** Open tasks due in the seven days after today. */
    this_week: TaskWithLead[];
    /** Open leads that are cooling or cold, furthest gone first. */
    going_cold: Lead[];
    /** Open leads with no open task, best score first. */
    no_next_step: Lead[];
    /** Open deals past their stage's time limit, furthest past first. */
    stuck: Lead[];
}

/** Response of PATCH /api/tasks/:id. */
export interface TaskStatusResult {
    task: Task;
    /** The timeline entry written when this call completed or cancelled the task, else null. */
    activity: Activity | null;
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
    stages: (MetaOption<Stage> & { probability: number })[];
    sources: MetaOption<Source>[];
    activity_types: MetaOption<UserActivityType>[];
    template_fields: TemplateField[];
    lost_reasons: MetaOption<CloseReason>[];
    won_reasons: MetaOption<CloseReason>[];
}

/** Mirrors model.TemplateField: one {{field}} an email template can use. */
export interface TemplateField {
    key: string;
    label: string;
    /** What it looks like filled in, for the editor's preview. */
    example: string;
}

/** Mirrors model.EmailTemplate. */
export interface EmailTemplate {
    id: number;
    name: string;
    subject: string;
    body: string;
    created_at: string;
    updated_at: string;
}

/** Body of POST /api/email-templates and PUT /api/email-templates/:id. */
export interface EmailTemplateInput {
    name: string;
    subject: string;
    body: string;
}

/** Mirrors model.AutomationRule: "when a new lead comes in, plan this task". */
export interface AutomationRule {
    id: number;
    name: string;
    enabled: boolean;
    trigger: "lead_created";
    /** Empty means any source. */
    sources: Source[];
    /** null means any deal value, including none. */
    min_deal_value: number | null;
    /** May use the email template {{fields}}. */
    task_title: string;
    task_type: TaskType;
    due_in_days: number;
    /** null gives the task to the lead's owner. */
    assignee_id: number | null;
    run_count: number;
    last_run_at: string | null;
    created_at: string;
    updated_at: string;
}

/** Body of POST /api/automations and PUT /api/automations/:id. */
export interface AutomationRuleInput {
    name: string;
    enabled: boolean;
    sources: Source[];
    min_deal_value: number | null;
    task_title: string;
    task_type: TaskType;
    due_in_days: number;
    assignee_id: number | null;
}

/** Mirrors model.LeadAssignment: new leads nobody was chosen for go to these people in turns. */
export interface LeadAssignment {
    enabled: boolean;
    member_ids: number[];
    /** Got the last lead; the next goes to whoever follows. */
    last_member_id: number | null;
}

/** Body of PUT /api/lead-assignment. */
export interface LeadAssignmentInput {
    enabled: boolean;
    member_ids: number[];
}

/** Body of POST /api/leads/:id/emails and /email/preview, fields and all. */
export interface EmailInput {
    subject: string;
    body: string;
    /** true sends it from the server; false only logs it on the timeline. */
    send?: boolean;
}

/** Mirrors model.RenderedEmail: an email filled in for one lead. */
export interface RenderedEmail {
    to: string;
    subject: string;
    body: string;
    /** Fields with nothing to fill in, left blank (e.g. owner_name on an unowned lead). */
    missing: string[];
}

/** Response of POST /api/leads/:id/emails. */
export interface LeadEmailResult {
    activity: Activity;
    email: RenderedEmail;
    sent: boolean;
}

/** Query parameters accepted by GET /api/leads. */
export interface LeadListParams {
    search?: string;
    stage?: Stage | "all";
    source?: Source | "all";
    /** "open" = every stage before Won/Lost; "closed" = Won or Lost. */
    status?: "open" | "closed";
    /** A team member's id, or "none" for unowned leads. */
    owner?: number | "none";
    /** Only leads whose score is in this band. Closed leads have none, so never match. */
    band?: ScoreBand | "all";
    /** Only leads of this quality. */
    quality?: Quality | "all";
    sort_by?: "created_at" | "updated_at" | "name" | "company" | "stage" | "deal_value" | "score";
    sort_dir?: "asc" | "desc";
    page?: number;
    page_size?: number;
    /** Only these leads, comma-separated ids: for exporting the ones ticked. */
    ids?: string;
}

/** An ad lead form platform. Mirrors model.AdProvider. */
export type AdProvider = "meta" | "google";

/** One answer on an ad lead form. Mirrors model.AdAnswer. */
export interface AdAnswer {
    key: string;
    label: string;
    value: string;
}

/** One ad form submission received, and what became of it. Mirrors model.AdLead. */
export interface AdLead {
    id: number;
    provider: AdProvider;
    external_id: string;
    form_id: string;
    campaign: string;
    ad: string;
    is_test: boolean;
    /** created: a new lead; duplicate: noted on the lead with that email; test: no lead; failed: see error. */
    status: "created" | "duplicate" | "test" | "failed";
    error: string;
    lead_id: number | null;
    /** "" when there is no lead, or it was deleted. */
    lead_name: string;
    answers: AdAnswer[];
    received_at: string;
}

/** Whether one platform is set up. Secrets are never sent, only which are missing. */
export interface IntegrationStatus {
    configured: boolean;
    /** The backend/.env settings still empty. */
    missing: string[];
    /** The full webhook address, or "" until PUBLIC_API_URL is set. */
    webhook_url: string;
    /** The webhook's path on the server. */
    path: string;
}

/** Response of GET /api/integrations. */
export interface IntegrationsSummary {
    public_url: string;
    meta: IntegrationStatus;
    google: IntegrationStatus;
    /** Latest submissions received, newest first. */
    recent: AdLead[];
}

/** Body of POST /api/leads/bulk. Mirrors model.BulkInput. */
export type BulkInput = { ids: number[] } & (
    | { action: "owner"; owner_id: number | null }
    | ({ action: "stage"; stage: Stage } & Partial<CloseInput>)
    | { action: "source"; source: Source }
    | { action: "delete" }
);

/** Response of POST /api/leads/bulk. Mirrors model.BulkResult. */
export interface BulkResult {
    action: BulkInput["action"];
    changed: number;
    /** Leads that could not be changed, and why. */
    failed: { id: number; message: string }[];
}

/** A row of an import file that was not imported, and why. Mirrors model.ImportProblem. */
export interface ImportProblem {
    /** The row's line in the file; the header is line 1. */
    line: number;
    name: string;
    email: string;
    messages: string[];
    /** The row as written in the file. */
    cells: string[];
}

/** A column of an import file and the field it goes into ("" for none). Mirrors model.ImportHeader. */
export interface ImportHeader {
    name: string;
    field: string;
}

/** A field a column can be imported into. Mirrors model.ImportField. */
export interface ImportField {
    key: string;
    label: string;
    required: boolean;
}

/** Response of POST /api/leads/import. Mirrors model.ImportResult. */
export interface ImportResult {
    /** true: nothing was saved, `ready` says how many would be. */
    dry_run: boolean;
    total_rows: number;
    ready: number;
    imported: number;
    duplicates: ImportProblem[];
    errors: ImportProblem[];
    /** The file's columns and the field each goes into. */
    headers: ImportHeader[];
    /** Every field there is to choose from. */
    fields: ImportField[];
    /** Labels of required fields no column goes into yet; while any, nothing was checked. */
    missing: string[];
    /** The first row's cells, to show what each column holds. */
    sample: string[];
}

/** A paginated list response: { data, pagination }. */
export interface Paginated<T> {
    data: T[];
    pagination: PageMeta;
}

// ---------------------------------------------------------------------------
// Sales Intelligence: Lead Scoring, Funnel Analytics, and Source ROI
// ---------------------------------------------------------------------------

export interface IntelligenceSummary {
    total_leads: number;
    open_leads: number;
    won_leads: number;
    lost_leads: number;
    total_pipeline_value: number;
    total_won_revenue: number;
    overall_conversion_rate: number;
    average_deal_size: number;
    average_sales_cycle_days: number;
    average_lead_score: number;
    overall_channel_roi: number;
    overall_revenue_multiple: number;
}

export interface ScoreBandBreakdown {
    band: ScoreBand;
    label: string;
    count: number;
    percentage: number;
    total_value: number;
    average_probability: number;
}

export interface ScoringFactorRule {
    key: string;
    label: string;
    max_points: number;
    description: string;
}

export interface ScoringIntelligence {
    average_score: number;
    hot_count: number;
    warm_count: number;
    cold_count: number;
    hot_value: number;
    warm_value: number;
    cold_value: number;
    band_breakdown: ScoreBandBreakdown[];
    factor_weights: ScoringFactorRule[];
    top_leads: Lead[];
    at_risk_deals: Lead[];
}

export interface FunnelStageMetric {
    stage: Stage;
    label: string;
    entered_count: number;
    current_count: number;
    current_value: number;
    conversion_rate: number;
    cumulative_conversion: number;
    drop_off_count: number;
    drop_off_rate: number;
    avg_duration_days: number;
}

export interface FunnelAnalytics {
    stages: FunnelStageMetric[];
    total_funnel_entries: number;
    overall_conversion_rate: number;
    avg_sales_cycle_days: number;
    bottleneck_stage: string;
    bottleneck_reason: string;
    bottleneck_recommendation: string;
}

export interface SourceROIMetric {
    source: Source;
    label: string;
    leads: number;
    share_of_leads: number;
    won: number;
    lost: number;
    open: number;
    win_rate: number;
    won_value: number;
    pipeline_value: number;
    avg_deal_size: number;
    benchmark_cost_per_lead: number;
    total_acquisition_cost: number;
    net_profit: number;
    roi: number;
    revenue_multiple: number;
    cac: number;
    efficiency_tier: string;
}

export interface SourceROIReport {
    sources: SourceROIMetric[];
    total_acquisition_cost: number;
    total_won_revenue: number;
    total_net_profit: number;
    overall_roi: number;
    overall_revenue_multiple: number;
    top_performing_source: string;
    lowest_cac_source: string;
}

export interface SalesIntelligenceReport {
    summary: IntelligenceSummary;
    scoring: ScoringIntelligence;
    funnel: FunnelAnalytics;
    source_roi: SourceROIReport;
}


// ---------------------------------------------------------------------------
// Login
// ---------------------------------------------------------------------------

/** Mirrors model.Role. */
export type Role = "admin" | "manager" | "sales";

/** Mirrors model.Viewer: who is logged in. id is 0 for the admin API key. */
export interface Viewer {
    id: number;
    name: string;
    email: string;
    role: Role;
}

// ---------------------------------------------------------------------------
// Lead quality and Meta Conversions API
// ---------------------------------------------------------------------------

/** Mirrors model.Quality. */
export type Quality = "good" | "average" | "poor" | "not_evaluated";

/** Mirrors model.QualificationStatus: read from the stage. */
export type QualificationStatus = "pending" | "qualified" | "not_qualified";

/** One quality rule's outcome. Mirrors model.QualityHit. */
export interface QualityHit {
    rule_id: string;
    label: string;
    category: QualityCategory;
    /** unknown: the data is not there yet, which never counts against a lead. */
    outcome: "matched" | "failed" | "unknown";
    points: number;
    max: number;
    required: boolean;
}

export type QualityCategory =
    | "contact_validity"
    | "service_fit"
    | "budget_fit"
    | "follow_up_response"
    | "meeting_attendance"
    | "other";

/** Mirrors model.CAPIStatus: one conversion event's delivery. */
export type CapiStatus = "pending" | "sent" | "failed" | "retry_scheduled" | "cancelled";

/** Mirrors model.CAPIHealth, plus "untested". */
export type CapiHealth =
    | "connected"
    | "invalid_credentials"
    | "permission_denied"
    | "temporarily_unavailable"
    | "not_configured"
    | "disabled"
    | "untested";

/** Mirrors model.MetaAttribution. */
export interface MetaAttribution {
    lead_id: number;
    meta_lead_id: string;
    page_id: string;
    form_id: string;
    ad_account_id: string;
    campaign_id: string;
    campaign_name: string;
    adset_id: string;
    adset_name: string;
    ad_id: string;
    ad_name: string;
    is_organic: boolean;
    meta_created_at: string | null;
    created_at: string;
}

/** Mirrors model.CAPIEvent. Carries no customer data. */
export interface CapiEvent {
    id: number;
    lead_id: number;
    lead_name: string;
    meta_lead_id: string;
    stage: Stage;
    event_name: string;
    event_id: string;
    event_time: string;
    test_mode: boolean;
    status: CapiStatus;
    attempts: number;
    next_attempt_at: string | null;
    last_error: string;
    error_code: number | null;
    fbtrace_id: string;
    sent_at: string | null;
    created_at: string;
}

/** Mirrors model.StageEvent. stage "new" means "lead created". */
export interface StageEvent {
    stage: Stage;
    event_name: string;
    disabled?: boolean;
}

/** Mirrors model.SecretState: a credential, described but never shown. */
export interface SecretState {
    set: boolean;
    hint: string;
    source: "saved" | "env" | "";
}

/** GET /api/integrations/meta-capi. */
export interface CapiSettings {
    dataset_id: string;
    app_id: string;
    test_event_code: string;
    test_mode: boolean;
    enabled: boolean;
    event_mapping: StageEvent[];
    crm_name: string;
    phone_country_code: string;
    access_token: SecretState;
    status: CapiHealth;
    status_detail: string;
    status_at: string | null;
    token_rotated_at: string | null;
    encryption_ready: boolean;
    secrets_locked: boolean;
    counts: Record<CapiStatus, number>;
}

/** PUT /api/integrations/meta-capi. access_token "" keeps the saved one. */
export interface CapiSettingsInput {
    dataset_id: string;
    app_id: string;
    test_event_code: string;
    test_mode: boolean;
    enabled: boolean;
    event_mapping: StageEvent[];
    crm_name: string;
    phone_country_code: string;
    access_token: string;
}

export interface CapiTestResult {
    ok: boolean;
    status: CapiHealth;
    detail: string;
    dataset: string;
}

export interface CapiTestEventResult {
    event_name: string;
    event_id: string;
    test_event_code: string;
    events_received: number;
    fbtrace_id: string;
    used_lead_id: boolean;
}

export interface CapiEventLog {
    events: CapiEvent[];
    counts: Record<CapiStatus, number>;
}

export interface AuditEntry {
    id: number;
    action: string;
    actor: string;
    detail: Record<string, unknown>;
    created_at: string;
}

/** Mirrors model.MetaLeadSummary. */
export interface MetaLeadSummary {
    total: number;
    qualified: number;
    converted: number;
    unqualified: number;
    conversion_rate: number | null;
}

export interface MetaLeadRow {
    lead: Lead;
    attribution: MetaAttribution;
    effective_score: number | null;
    capi_status: CapiStatus | "";
}

export interface MetaFilterOption {
    value: string;
    label: string;
    count: number;
}

export interface MetaLeadsReport {
    summary: MetaLeadSummary;
    leads: MetaLeadRow[];
    options: {
        ad_accounts: MetaFilterOption[];
        pages: MetaFilterOption[];
        forms: MetaFilterOption[];
        campaigns: MetaFilterOption[];
        ads: MetaFilterOption[];
    };
}

/** Query of GET /api/meta/leads. Empty values mean "any". */
export interface MetaLeadParams {
    from?: string;
    to?: string;
    form_id?: string;
    campaign_id?: string;
    quality?: Quality | "";
    qualification?: QualificationStatus | "";
    capi_status?: CapiStatus | "none" | "";
    stage?: Stage | "";
    owner?: string;
    page?: number;
    page_size?: number;
}

/** Mirrors model.CustomerLink: a page the lead opens without logging in. */
export interface CustomerLink {
    kind: "booking" | "feedback";
    url: string;
    created_at: string;
    emailed_at: string | null;
}

/** Mirrors model.Feedback: what a won customer said. */
export interface GivenFeedback {
    rating: number;
    comment: string;
    referral_lead_id: number | null;
    created_at: string;
}

/** Mirrors model.CustomerLinks: GET /api/leads/:id/links. */
export interface CustomerLinks {
    booking: CustomerLink | null;
    feedback: CustomerLink | null;
    given_feedback: GivenFeedback | null;
}

/** Mirrors model.BookedMeeting. */
export interface BookedMeeting {
    date: string;
    time: string;
    owner_name: string;
}

/** Mirrors model.BookingPage: GET /api/public/booking/:token. */
export interface BookingPage {
    lead_name: string;
    company: string;
    /** "" when nobody is assigned the lead yet. */
    owner_name: string;
    booked: BookedMeeting | null;
    days: { date: string; times: string[] }[];
    slot_minutes: number;
}

/** Mirrors model.FeedbackPage: GET /api/public/feedback/:token. */
export interface FeedbackPage {
    lead_name: string;
    company: string;
    owner_name: string;
    given: boolean;
}

/** Body of POST /api/public/feedback/:token. */
export interface FeedbackInput {
    rating: number;
    comment: string;
    referral: { name: string; email: string; phone: string; company: string } | null;
}

/** Mirrors model.SheetField: a lead field that can be a spreadsheet column. */
export interface SheetField {
    key: string;
    label: string;
    default: boolean;
}

/** Mirrors model.SheetSync: one export to Google Sheets. */
export interface SheetSync {
    id: number;
    trigger: "manual" | "auto" | "retry";
    status: "running" | "success" | "partial" | "failed";
    started_at: string;
    finished_at: string | null;
    leads_total: number;
    rows_updated: number;
    rows_added: number;
    rows_failed: number;
    error: string;
    started_by: string;
    retry_of: number | null;
}

/** Mirrors model.SheetsSettings: GET /api/integrations/google-sheets (settings). */
export interface SheetsSettings {
    oauth_configured: boolean;
    encryption_ready: boolean;
    picker_available: boolean;
    redirect_url: string;
    missing_env: string[];
    status: "not_connected" | "connected" | "error";
    account_email: string;
    token_locked: boolean;
    spreadsheet_id: string;
    spreadsheet_name: string;
    spreadsheet_url: string;
    sheet_title: string;
    columns: string[];
    stages: Stage[];
    auto_sync: boolean;
    auto_sync_minutes: number;
    last_sync_at: string | null;
    last_sync_count: number | null;
    last_error: string;
    last_error_at: string | null;
    fields: SheetField[];
    auto_sync_options: number[];
    configured_by_name: string;
}

export interface SheetsView {
    settings: SheetsSettings;
    syncs: SheetSync[];
}

/** Mirrors gsheets.Spreadsheet. */
export interface Spreadsheet {
    id: string;
    name: string;
    url: string;
    tabs: string[];
}

/** Body of PUT /api/integrations/google-sheets. */
export interface SheetsConfigInput {
    spreadsheet_id: string;
    sheet_title: string;
    new_sheet_title: string;
    columns: string[];
    stages: Stage[];
    auto_sync: boolean;
    auto_sync_minutes: number;
}

/** Mirrors gsheets.TestResult. */
export interface SheetsTestResult {
    ok: boolean;
    message: string;
    account: string;
    spreadsheet: string;
    tab: string;
    lead_rows: number;
}
