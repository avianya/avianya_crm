/**
 * The single place in the app that talks HTTP.
 *
 * Every component calls a named function from here instead of using fetch
 * directly, which means URL construction, the response envelope, and error
 * translation all live in one file. Change the API shape and you change one file.
 */

import type {
    Activity,
    ActivityInput,
    AdLead,
    AdProvider,
    AppNotification,
    AuditEntry,
    AutomationRule,
    AutomationRuleInput,
    CapiEvent,
    CapiEventLog,
    CapiSettings,
    SheetsConfigInput,
    SheetsTestResult,
    SheetsView,
    Spreadsheet,
    BookedMeeting,
    BookingPage,
    CustomerLinks,
    FeedbackInput,
    FeedbackPage,
    CapiSettingsInput,
    CapiStatus,
    CapiTestEventResult,
    CapiTestResult,
    BulkInput,
    BulkResult,
    CloseInput,
    DashboardSummary,
    EmailInput,
    EmailTemplate,
    EmailTemplateInput,
    FollowUpSummary,
    IntegrationsSummary,
    ImportResult,
    Lead,
    LeadAssignment,
    LeadAssignmentInput,
    LeadEmailResult,
    LeadInput,
    LeadListParams,
    Meta,
    MetaLeadParams,
    MetaLeadsReport,
    NextStep,
    NotificationList,
    PageMeta,
    Paginated,
    PipelineColumn,
    ReminderSettings,
    RenderedEmail,
    ReportSummary,
    SalesIntelligenceReport,
    Stage,
    Task,
    TaskInput,
    TaskScheduleInput,
    TaskStatusResult,
    TaskWithLead,
    TeamMember,
    TeamMemberInput,
    Role,
    Viewer,
} from "./types";

/**
 * Base URL of the API.
 *
 * Vite replaces import.meta.env.VITE_* at build time from .env files. Only
 * variables prefixed VITE_ are exposed to the browser bundle -- that prefix is
 * Vite's guard against accidentally shipping a server secret to the client.
 */
const API_BASE_URL: string =
    import.meta.env.VITE_API_BASE_URL ?? "http://localhost:8080/api";

/**
 * An error carrying the HTTP status and the backend's structured error body.
 *
 * Extending Error means it still behaves like a normal exception (stack trace,
 * try/catch, .message), while `details` gives forms the per-field messages needed
 * to mark individual inputs as invalid.
 */
export class ApiError extends Error {
    readonly status: number;
    readonly code: string;
    readonly details?: Record<string, string>;

    constructor(
        status: number,
        code: string,
        message: string,
        details?: Record<string, string>,
    ) {
        super(message);
        this.name = "ApiError";
        this.status = status;
        this.code = code;
        this.details = details;
    }

    /** True when the server rejected the input, i.e. `details` is worth reading. */
    get isValidationError(): boolean {
        return this.status === 422 || this.code === "validation_error";
    }

    /** True when another lead already uses the submitted email. */
    get isConflict(): boolean {
        return this.status === 409;
    }

    get isNotFound(): boolean {
        return this.status === 404;
    }
}

/** Shape of an error body from the backend. */
interface ErrorBody {
    error?: { code?: string; message?: string; details?: Record<string, string> };
}

/**
 * Performs one request and returns the parsed response envelope, throwing an
 * ApiError for anything that is not a 2xx.
 *
 * This is the low-level primitive. Most callers want `request` below, which also
 * unwraps `data`; list endpoints use this directly because they need the sibling
 * `pagination` field too.
 */
async function requestEnvelope<TEnvelope>(
    path: string,
    init?: RequestInit,
): Promise<TEnvelope> {
    let response: Response;

    try {
        response = await fetch(`${API_BASE_URL}${path}`, {
            ...init,
            // The session lives in an HttpOnly cookie the API set at login;
            // "include" sends it, though the API is on another port.
            credentials: "include",
            headers: {
                Accept: "application/json",
                // Required on every change: another site cannot set it (CSRF).
                "X-Requested-With": "crm",
                // Only send Content-Type when there is a body to describe.
                ...(init?.body ? { "Content-Type": "application/json" } : {}),
                ...init?.headers,
            },
        });
    } catch (cause) {
        // fetch only rejects on a genuine network failure -- server down, DNS
        // failure, CORS refusal. An HTTP 500 is a *resolved* promise, handled
        // below. Distinguishing the two lets us give a much better message.
        if (cause instanceof DOMException && cause.name === "AbortError") {
            throw cause; // a cancelled request is not a failure to report
        }
        throw new ApiError(
            0,
            "network_error",
            `Cannot reach the API at ${API_BASE_URL}. Is the Go server running?`,
        );
    }

    // 204 No Content (what DELETE returns) has no body to parse.
    if (response.status === 204) {
        return undefined as TEnvelope;
    }

    const text = await response.text();
    let body: unknown = null;

    if (text) {
        try {
            body = JSON.parse(text);
        } catch {
            // A non-JSON body means something other than our API answered --
            // a proxy error page, or a crash before the JSON was written.
            throw new ApiError(
                response.status,
                "invalid_response",
                `The server returned a response that was not JSON (HTTP ${response.status}).`,
            );
        }
    }

    if (response.status === 401 && !path.startsWith("/auth/")) {
        // Logged out, or the session ended: tell the app to show the login.
        window.dispatchEvent(new Event(SESSION_ENDED));
    }

    if (!response.ok) {
        const errorBody = body as ErrorBody | null;
        throw new ApiError(
            response.status,
            errorBody?.error?.code ?? "unknown_error",
            errorBody?.error?.message ?? `Request failed with HTTP ${response.status}.`,
            errorBody?.error?.details,
        );
    }

    return body as TEnvelope;
}

/**
 * Performs one request and unwraps the `{ data: ... }` envelope.
 *
 * Generic over T -- the type of the `data` field -- so callers get a properly
 * typed result instead of `any`.
 */
async function request<T>(path: string, init?: RequestInit): Promise<T> {
    const envelope = await requestEnvelope<{ data: T }>(path, init);
    // DELETE returns 204 with no envelope at all; T is void in that case.
    return envelope?.data;
}

/** Fired when the API says the session has ended; the app shows the login. */
export const SESSION_ENDED = "crm:session-ended";

/** Requests that only some roles may make; the server checks the role. */
function adminRequest<T>(path: string, init?: RequestInit): Promise<T> {
    return request<T>(path, init);
}

/** Builds a query string, skipping empty values and "all" sentinels. */
function toQueryString(params: LeadListParams | MetaLeadParams | Record<string, string | number | undefined>): string {
    const search = new URLSearchParams();

    for (const [key, value] of Object.entries(params)) {
        // "all" is the UI's way of saying "no filter", so it is dropped rather
        // than sent to a backend that would reject it as an unknown stage.
        if (value === undefined || value === "" || value === "all") continue;
        search.set(key, String(value));
    }

    const query = search.toString();
    return query ? `?${query}` : "";
}

// ---------------------------------------------------------------------------
// Endpoints
// ---------------------------------------------------------------------------

export const api = {
    // --- login ----------------------------------------------------------------

    /** POST /api/auth/login — sets the session cookie. */
    login: (email: string, password: string) =>
        request<Viewer>("/auth/login", { method: "POST", body: JSON.stringify({ email, password }) }),

    /** POST /api/auth/signup — creates a Sales account and logs it in. */
    signup: (name: string, email: string, password: string) =>
        request<Viewer>("/auth/signup", { method: "POST", body: JSON.stringify({ name, email, password }) }),

    /** POST /api/auth/forgot — emails a one-hour reset link, if the email has an account. */
    forgotPassword: (email: string) =>
        request<{ message: string }>("/auth/forgot", { method: "POST", body: JSON.stringify({ email }) }),

    /** POST /api/auth/reset — sets a new password with the emailed link's token. */
    resetPassword: (token: string, password: string) =>
        request<void>("/auth/reset", { method: "POST", body: JSON.stringify({ token, password }) }),

    /** POST /api/auth/logout */
    logout: () => request<void>("/auth/logout", { method: "POST" }),

    /** GET /api/auth/me — who is logged in; 401 when nobody. */
    me: (signal?: AbortSignal) => request<Viewer>("/auth/me", { signal }),

    /** PUT /api/auth/password — change your own password. */
    changePassword: (currentPassword: string, newPassword: string) =>
        request<void>("/auth/password", {
            method: "PUT",
            body: JSON.stringify({ current_password: currentPassword, new_password: newPassword }),
        }),

    /** PUT /api/team/:id/password (admin) — give a member a login, or reset it. */
    setMemberPassword: (id: number, password: string) =>
        request<TeamMember>(`/team/${id}/password`, { method: "PUT", body: JSON.stringify({ password }) }),

    /** PUT /api/team/:id/account (admin) — role, or switch the login off. */
    updateMemberAccount: (id: number, change: { role?: Role; active?: boolean }) =>
        request<TeamMember>(`/team/${id}/account`, { method: "PUT", body: JSON.stringify(change) }),

    /** GET /api/health */
    health: (signal?: AbortSignal) =>
        request<{ status: string; database: string }>("/health", { signal }),

    /**
     * GET /api/meta
     *
     * Stage/source/activity-type vocabularies with display labels. Fetched once
     * and shared, so dropdowns and filters are built from the backend's list
     * instead of a second copy hard-coded in the UI.
     */
    meta: (signal?: AbortSignal) => request<Meta>("/meta", { signal }),

    /** GET /api/pipeline — the board, one column per stage in lifecycle order. */
    pipeline: (signal?: AbortSignal) =>
        request<PipelineColumn[]>("/pipeline", { signal }),

    /** GET /api/reports — weekly intake, source performance, time in stage. */
    reports: (signal?: AbortSignal) => request<ReportSummary>("/reports", { signal }),

    /** GET /api/sales-intelligence — Lead Scoring, Funnel Analytics, and Source ROI. */
    salesIntelligence: (signal?: AbortSignal) =>
        request<SalesIntelligenceReport>("/sales-intelligence", { signal }),

    /** POST /api/sales-intelligence/seed — Seeds or resets realistic intelligence dataset. */
    seedIntelligence: () =>
        request<{ message: string; total_leads: number; report?: SalesIntelligenceReport }>(
            "/sales-intelligence/seed",
            { method: "POST" },
        ),

    /** GET /api/dashboard */
    dashboard: (signal?: AbortSignal) =>
        request<DashboardSummary>("/dashboard", { signal }),

    /**
     * GET /api/follow-ups — what is due and which leads need contact.
     * owner: a member's id, "none" for nobody's, or omitted for everyone.
     */
    followUps: (owner: number | "none" | undefined, signal?: AbortSignal) =>
        request<FollowUpSummary>(`/follow-ups${owner === undefined ? "" : `?owner=${owner}`}`, { signal }),

    /**
     * GET /api/leads
     *
     * Returns the page of leads *and* the pagination block, so it keeps the whole
     * envelope instead of unwrapping to `data`.
     */
    listLeads: (params: LeadListParams = {}, signal?: AbortSignal) =>
        requestEnvelope<Paginated<Lead>>(`/leads${toQueryString(params)}`, { signal }),

    /** GET /api/leads/:id — includes the full activity timeline. */
    getLead: (id: number, signal?: AbortSignal) =>
        request<Lead>(`/leads/${id}`, { signal }),

    /** POST /api/leads */
    createLead: (input: LeadInput) =>
        request<Lead>("/leads", { method: "POST", body: JSON.stringify(input) }),

    /** PUT /api/leads/:id — a full replacement of the editable fields. */
    updateLead: (id: number, input: LeadInput) =>
        request<Lead>(`/leads/${id}`, { method: "PUT", body: JSON.stringify(input) }),

    /**
     * PATCH /api/leads/:id/stage
     *
     * Used by the inline stage dropdown. Sending just the stage means the list
     * view does not need the lead's other fields in order to move it along.
     */
    /**
     * POST /api/leads/bulk — one change for many leads, each logged on its own
     * timeline. Leads that could not be changed come back in `failed`.
     */
    bulkLeads: (input: BulkInput) =>
        request<BulkResult>("/leads/bulk", { method: "POST", body: JSON.stringify(input) }),

    updateLeadStage: (id: number, stage: Stage, close?: CloseInput) =>
        request<Lead>(`/leads/${id}/stage`, {
            method: "PATCH",
            // Moving to Lost needs close_reason (422 on it otherwise); the same
            // stage with a new reason changes just the reason.
            body: JSON.stringify({ stage, ...close }),
        }),

    /** PATCH /api/leads/:id/owner — null leaves the lead unowned. Returns the lead with its timeline. */
    updateLeadOwner: (id: number, ownerId: number | null) =>
        request<Lead>(`/leads/${id}/owner`, {
            method: "PATCH",
            body: JSON.stringify({ owner_id: ownerId }),
        }),

    /** PATCH /api/leads/:id/notes — changes the notes only. Returns the lead with its timeline. */
    updateLeadNotes: (id: number, notes: string) =>
        request<Lead>(`/leads/${id}/notes`, {
            method: "PATCH",
            body: JSON.stringify({ notes }),
        }),

    /** DELETE /api/leads/:id */
    deleteLead: (id: number) => request<void>(`/leads/${id}`, { method: "DELETE" }),

    /** GET /api/leads/:id/activities */
    listActivities: (id: number, signal?: AbortSignal) =>
        request<Activity[]>(`/leads/${id}/activities`, { signal }),

    /** POST /api/leads/:id/activities */
    createActivity: (id: number, input: ActivityInput) =>
        request<Activity>(`/leads/${id}/activities`, {
            method: "POST",
            body: JSON.stringify(input),
        }),

    /** PUT /api/activities/:id — corrects a note, call, email or meeting. */
    updateActivity: (id: number, note: string) =>
        request<Activity>(`/activities/${id}`, {
            method: "PUT",
            body: JSON.stringify({ note }),
        }),

    /** DELETE /api/activities/:id — removes a note, call, email or meeting. */
    deleteActivity: (id: number) => request<void>(`/activities/${id}`, { method: "DELETE" }),

    /**
     * GET /api/tasks — every task across all leads, each with its lead.
     * "open" is soonest due first; "completed" is the latest 50, newest first.
     */
    listAllTasks: (status: "open" | "completed", signal?: AbortSignal) =>
        request<TaskWithLead[]>(`/tasks?status=${status}`, { signal }),

    /** GET /api/tasks?from=&to= — every task due in the range, any status (at most 62 days). */
    listTasksBetween: (from: string, to: string, signal?: AbortSignal) =>
        request<TaskWithLead[]>(`/tasks?from=${from}&to=${to}`, { signal }),

    /** GET /api/leads/:id/tasks — open first, soonest due first. */
    listTasks: (id: number, signal?: AbortSignal) =>
        request<Task[]>(`/leads/${id}/tasks`, { signal }),

    /** POST /api/leads/:id/tasks */
    createTask: (id: number, input: TaskInput) =>
        request<Task>(`/leads/${id}/tasks`, {
            method: "POST",
            body: JSON.stringify(input),
        }),

    /**
     * PATCH /api/tasks/:id
     *
     * Completing a task also writes a timeline entry, which comes back in the
     * response so the detail page can show it without refetching the timeline.
     */
    setTaskCompleted: (taskId: number, completed: boolean) =>
        request<TaskStatusResult>(`/tasks/${taskId}`, {
            method: "PATCH",
            body: JSON.stringify({ completed }),
        }),

    /** PATCH /api/tasks/:id — cancels an event (logged on the timeline) or restores it. */
    setTaskCancelled: (taskId: number, cancelled: boolean) =>
        request<TaskStatusResult>(`/tasks/${taskId}`, {
            method: "PATCH",
            body: JSON.stringify({ cancelled }),
        }),

    /** PUT /api/tasks/:id — the whole edit form. */
    updateTask: (taskId: number, input: TaskInput) =>
        request<Task>(`/tasks/${taskId}`, {
            method: "PUT",
            body: JSON.stringify(input),
        }),

    /** PUT /api/tasks/:id/schedule — moves a task to a new date and time slot. */
    rescheduleTask: (taskId: number, input: TaskScheduleInput) =>
        request<Task>(`/tasks/${taskId}/schedule`, {
            method: "PUT",
            body: JSON.stringify(input),
        }),

    /** DELETE /api/tasks/:id */
    deleteTask: (taskId: number) => request<void>(`/tasks/${taskId}`, { method: "DELETE" }),

    /** GET /api/leads/:id/next-steps — suggested follow-ups, most urgent first. */
    nextSteps: (leadId: number, signal?: AbortSignal) =>
        request<NextStep[]>(`/leads/${leadId}/next-steps`, { signal }),

    /**
     * GET /api/leads/export — every lead matching the list's filters and sort,
     * all pages, as a CSV file. Resolves to the file; saveFile downloads it.
     */
    exportLeads: async (params: LeadListParams): Promise<Blob> => {
        const filters = { ...params, page: undefined, page_size: undefined };
        let response: Response;
        try {
            response = await fetch(`${API_BASE_URL}/leads/export${toQueryString(filters)}`, {
                credentials: "include",
            });
        } catch {
            throw new ApiError(0, "network_error", `Cannot reach the API at ${API_BASE_URL}. Is the Go server running?`);
        }
        if (!response.ok) {
            const body = (await response.json().catch(() => null)) as ErrorBody | null;
            throw new ApiError(
                response.status,
                body?.error?.code ?? "unknown_error",
                body?.error?.message ?? `Export failed with HTTP ${response.status}.`,
                body?.error?.details,
            );
        }
        return response.blob();
    },

    /**
     * POST /api/leads/import — the body is the CSV file's text. dryRun checks
     * every row and saves nothing (the preview); runAutomations runs the
     * automation rules on each imported lead. columns gives the field for each
     * column by position ("" for none); left out, the server works them out
     * from the headers. A problem with the whole file is a 422 on "file".
     */
    importLeads: (csv: string, options: { dryRun: boolean; runAutomations: boolean; columns?: string[] }) => {
        const query = new URLSearchParams({
            dry_run: String(options.dryRun),
            run_automations: String(options.runAutomations),
        });
        if (options.columns) query.set("columns", JSON.stringify(options.columns));
        return request<ImportResult>(`/leads/import?${query}`, {
            method: "POST",
            body: csv,
            headers: { "Content-Type": "text/csv" },
        });
    },

    // --- email ------------------------------------------------------------------

    /** GET /api/email-templates — every template, by name. */
    listEmailTemplates: (signal?: AbortSignal) =>
        request<EmailTemplate[]>("/email-templates", { signal }),

    /** POST /api/email-templates — a typo in a {{field}} is a 422 on that field. */
    createEmailTemplate: (input: EmailTemplateInput) =>
        request<EmailTemplate>("/email-templates", { method: "POST", body: JSON.stringify(input) }),

    /** PUT /api/email-templates/:id */
    updateEmailTemplate: (id: number, input: EmailTemplateInput) =>
        request<EmailTemplate>(`/email-templates/${id}`, { method: "PUT", body: JSON.stringify(input) }),

    /** DELETE /api/email-templates/:id — emails already sent stay on their timelines. */
    deleteEmailTemplate: (id: number) => request<void>(`/email-templates/${id}`, { method: "DELETE" }),

    /** POST /api/leads/:id/email/preview — fills in the {{fields}} for the lead; sends nothing. */
    previewLeadEmail: (leadId: number, input: EmailInput, signal?: AbortSignal) =>
        request<RenderedEmail>(`/leads/${leadId}/email/preview`, {
            method: "POST",
            body: JSON.stringify(input),
            signal,
        }),

    /**
     * POST /api/leads/:id/emails — send: true sends it from the server (409 when
     * email is not set up); false only logs it. Either way it lands on the timeline.
     */
    sendLeadEmail: (leadId: number, input: EmailInput) =>
        request<LeadEmailResult>(`/leads/${leadId}/emails`, { method: "POST", body: JSON.stringify(input) }),

    // --- automations ----------------------------------------------------------

    /** GET /api/automations — every rule, in the order they run. */
    listAutomationRules: (signal?: AbortSignal) => request<AutomationRule[]>("/automations", { signal }),

    /** POST /api/automations */
    createAutomationRule: (input: AutomationRuleInput) =>
        request<AutomationRule>("/automations", { method: "POST", body: JSON.stringify(input) }),

    /** PUT /api/automations/:id — the whole rule; also how it is switched on or off. */
    updateAutomationRule: (id: number, input: AutomationRuleInput) =>
        request<AutomationRule>(`/automations/${id}`, { method: "PUT", body: JSON.stringify(input) }),

    /** DELETE /api/automations/:id — tasks it already planned stay. */
    deleteAutomationRule: (id: number) => request<void>(`/automations/${id}`, { method: "DELETE" }),

    // --- Google Sheets ---------------------------------------------------------

    /** GET /api/integrations/google-sheets — status, choices and recent syncs. */
    sheets: (signal?: AbortSignal) => adminRequest<SheetsView>("/integrations/google-sheets", { signal }),

    /** PUT /api/integrations/google-sheets — the sheet, columns and auto-sync. */
    saveSheets: (input: SheetsConfigInput) =>
        adminRequest<SheetsView>("/integrations/google-sheets", { method: "PUT", body: JSON.stringify(input) }),

    /** POST /api/integrations/google-sheets/connect — Google's consent page to open. */
    connectSheets: () => adminRequest<{ url: string }>("/integrations/google-sheets/connect", { method: "POST" }),

    /** POST /api/integrations/google-sheets/disconnect */
    disconnectSheets: () => adminRequest<SheetsView>("/integrations/google-sheets/disconnect", { method: "POST" }),

    /** POST /api/integrations/google-sheets/test */
    testSheets: () => adminRequest<SheetsTestResult>("/integrations/google-sheets/test", { method: "POST" }),

    /** GET /api/integrations/google-sheets/spreadsheets — the ones the CRM may open. */
    listSpreadsheets: (signal?: AbortSignal) =>
        adminRequest<Spreadsheet[]>("/integrations/google-sheets/spreadsheets", { signal }),

    /** GET /api/integrations/google-sheets/spreadsheets/:id — its tabs. */
    getSpreadsheet: (id: string, signal?: AbortSignal) =>
        adminRequest<Spreadsheet>(`/integrations/google-sheets/spreadsheets/${encodeURIComponent(id)}`, { signal }),

    /** POST /api/integrations/google-sheets/spreadsheets — a new spreadsheet. */
    createSpreadsheet: (name: string) =>
        adminRequest<Spreadsheet>("/integrations/google-sheets/spreadsheets", {
            method: "POST",
            body: JSON.stringify({ name }),
        }),

    /** GET /api/integrations/google-sheets/picker-token — for Google's file picker only. */
    sheetsPickerToken: () =>
        adminRequest<{ access_token: string; expires_at: string; api_key: string; app_id: string }>(
            "/integrations/google-sheets/picker-token",
        ),

    /** POST /api/integrations/google-sheets/sync — runs in the background. */
    syncSheets: () => adminRequest<{ message: string }>("/integrations/google-sheets/sync", { method: "POST" }),

    /** POST /api/integrations/google-sheets/syncs/:id/retry */
    retrySheetSync: (id: number) =>
        adminRequest<{ message: string }>(`/integrations/google-sheets/syncs/${id}/retry`, { method: "POST" }),

    // --- customer links -------------------------------------------------------

    /** GET /api/leads/:id/links — the booking and thank-you links, and any feedback given. */
    customerLinks: (leadId: number, signal?: AbortSignal) =>
        request<CustomerLinks>(`/leads/${leadId}/links`, { signal }),

    /** POST /api/leads/:id/links/:kind — makes the link, or keeps the one there is. */
    createCustomerLink: (leadId: number, kind: "booking" | "feedback") =>
        request<CustomerLinks>(`/leads/${leadId}/links/${kind}`, { method: "POST" }),

    /** GET /api/public/booking/:token — no login; the lead's free slots. */
    bookingPage: (token: string, signal?: AbortSignal) =>
        request<BookingPage>(`/public/booking/${encodeURIComponent(token)}`, { signal }),

    /** POST /api/public/booking/:token */
    bookMeeting: (token: string, input: { date: string; time: string; note: string }) =>
        request<BookedMeeting>(`/public/booking/${encodeURIComponent(token)}`, {
            method: "POST",
            body: JSON.stringify(input),
        }),

    /** GET /api/public/feedback/:token — no login. */
    feedbackPage: (token: string, signal?: AbortSignal) =>
        request<FeedbackPage>(`/public/feedback/${encodeURIComponent(token)}`, { signal }),

    /** POST /api/public/feedback/:token */
    giveFeedback: (token: string, input: FeedbackInput) =>
        request<{ message: string }>(`/public/feedback/${encodeURIComponent(token)}`, {
            method: "POST",
            body: JSON.stringify(input),
        }),

    /** GET /api/lead-assignment — who new leads go to, in turns. */
    getLeadAssignment: (signal?: AbortSignal) => request<LeadAssignment>("/lead-assignment", { signal }),

    /** PUT /api/lead-assignment — also how it is switched on or off. */
    saveLeadAssignment: (input: LeadAssignmentInput) =>
        request<LeadAssignment>("/lead-assignment", { method: "PUT", body: JSON.stringify(input) }),

    // --- team -----------------------------------------------------------------

    /** GET /api/team */
    listTeam: (signal?: AbortSignal) => request<TeamMember[]>("/team", { signal }),

    /** POST /api/team */
    createTeamMember: (input: TeamMemberInput) =>
        request<TeamMember>("/team", { method: "POST", body: JSON.stringify(input) }),

    /** PUT /api/team/:id */
    updateTeamMember: (id: number, input: TeamMemberInput) =>
        request<TeamMember>(`/team/${id}`, { method: "PUT", body: JSON.stringify(input) }),

    /** DELETE /api/team/:id — their leads and events become unowned, nothing is deleted. */
    deleteTeamMember: (id: number) => request<void>(`/team/${id}`, { method: "DELETE" }),

    /** POST /api/team/:id/test-email — a real email; rejects with the server's reason. */
    sendTestEmail: (id: number) =>
        request<{ sent_to: string }>(`/team/${id}/test-email`, { method: "POST" }),

    /** POST /api/team/:id/digest — their daily summary, now; rejects with the server's reason. */
    sendDigestNow: (id: number) =>
        request<{ sent_to: string }>(`/team/${id}/digest`, { method: "POST" }),

    // --- notifications ---------------------------------------------------------

    /** GET /api/notifications — the latest reminders and the unread count. */
    listNotifications: (signal?: AbortSignal) =>
        request<NotificationList>("/notifications", { signal }),

    /** GET /api/notifications/settings — whether reminder emails can go out. */
    reminderSettings: (signal?: AbortSignal) =>
        request<ReminderSettings>("/notifications/settings", { signal }),

    /** POST /api/notifications/:id/read */
    markNotificationRead: (id: number) =>
        request<void>(`/notifications/${id}/read`, { method: "POST" }),

    /** POST /api/deal-alerts/:id/read */
    markDealAlertRead: (id: number) => request<void>(`/deal-alerts/${id}/read`, { method: "POST" }),

    /** POST /api/notifications/read-all — reminders and deal alerts alike. */
    markAllNotificationsRead: () => request<void>("/notifications/read-all", { method: "POST" }),

    /** POST /api/notifications/:id/retry — queues a failed or skipped email again. */
    retryNotificationEmail: (id: number) =>
        request<AppNotification>(`/notifications/${id}/retry`, { method: "POST" }),

    // --- integrations -------------------------------------------------------

    /** GET /api/integrations — Meta and Google set-up status, and the latest ad leads. */
    integrations: (signal?: AbortSignal) => request<IntegrationsSummary>("/integrations", { signal }),

    /** POST /api/integrations/test — a made-up submission, recorded as a test; it makes no lead. */
    sendTestAdLead: (provider: AdProvider) =>
        request<AdLead>("/integrations/test", { method: "POST", body: JSON.stringify({ provider }) }),

    /** POST /api/ad-leads/:id/retry — takes a failed submission in again. */
    retryAdLead: (id: number) => request<AdLead>(`/ad-leads/${id}/retry`, { method: "POST" }),

    // --- lead quality --------------------------------------------------------

    /** PATCH /api/leads/:id/qualification — why the lead is (not) qualified. */
    updateQualification: (id: number, reason: string) =>
        request<Lead>(`/leads/${id}/qualification`, { method: "PATCH", body: JSON.stringify({ reason }) }),

    /** PATCH /api/leads/:id/flags — confirm spam, or opt out of sharing with Meta. */
    updateLeadFlags: (id: number, flags: { spam_confirmed?: boolean; meta_share_opt_out?: boolean }) =>
        request<Lead>(`/leads/${id}/flags`, { method: "PATCH", body: JSON.stringify(flags) }),

    /** PUT /api/leads/:id/quality-override (admin) */
    overrideQuality: (id: number, score: number, reason: string) =>
        adminRequest<Lead>(`/leads/${id}/quality-override`, { method: "PUT", body: JSON.stringify({ score, reason }) }),

    /** DELETE /api/leads/:id/quality-override (admin) */
    clearQualityOverride: (id: number) =>
        adminRequest<Lead>(`/leads/${id}/quality-override`, { method: "DELETE" }),

    // --- Meta CAPI (admin) ---------------------------------------------------

    capiSettings: (signal?: AbortSignal) => adminRequest<CapiSettings>("/integrations/meta-capi", { signal }),

    saveCapiSettings: (input: CapiSettingsInput) =>
        adminRequest<CapiSettings>("/integrations/meta-capi", { method: "PUT", body: JSON.stringify(input) }),

    /** Update Access Token: replaces only the token. */
    rotateCapiToken: (accessToken: string) =>
        adminRequest<CapiSettings>("/integrations/meta-capi/token", {
            method: "PUT",
            body: JSON.stringify({ access_token: accessToken }),
        }),

    disableCapi: () => adminRequest<CapiSettings>("/integrations/meta-capi/disable", { method: "POST" }),

    testCapi: () => adminRequest<CapiTestResult>("/integrations/meta-capi/test", { method: "POST" }),

    sendCapiTestEvent: () =>
        adminRequest<CapiTestEventResult>("/integrations/meta-capi/test-event", { method: "POST" }),

    capiEvents: (params: { status?: CapiStatus | ""; lead_id?: number; from?: string; to?: string }, signal?: AbortSignal) =>
        adminRequest<CapiEventLog>(`/integrations/meta-capi/events${toQueryString(params)}`, { signal }),

    retryCapiEvent: (id: number) =>
        adminRequest<CapiEvent>(`/integrations/meta-capi/events/${id}/retry`, { method: "POST" }),

    retryFailedCapiEvents: () =>
        adminRequest<{ requeued: number }>("/integrations/meta-capi/events/retry-failed", { method: "POST" }),

    capiAudit: (signal?: AbortSignal) => adminRequest<AuditEntry[]>("/integrations/meta-capi/audit", { signal }),

    /** GET /api/meta/leads — the Meta leads dashboard, with pagination. */
    metaLeads: (params: MetaLeadParams, signal?: AbortSignal) =>
        requestEnvelope<{ data: MetaLeadsReport; pagination: PageMeta }>(`/meta/leads${toQueryString(params)}`, { signal }),
};

/** Hands a file to the browser as a download. */
export function saveFile(blob: Blob, filename: string): void {
    const href = URL.createObjectURL(blob);
    const link = document.createElement("a");
    link.href = href;
    link.download = filename;
    document.body.appendChild(link);
    link.click();
    link.remove();
    // After the click has been handled, or some browsers cancel the download.
    setTimeout(() => URL.revokeObjectURL(href), 1000);
}
