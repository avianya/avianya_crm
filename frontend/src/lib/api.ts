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
    DashboardSummary,
    Lead,
    LeadInput,
    LeadListParams,
    Meta,
    Paginated,
    Stage,
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
            headers: {
                Accept: "application/json",
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

/** Builds a query string, skipping empty values and "all" sentinels. */
function toQueryString(params: LeadListParams): string {
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

    /** GET /api/dashboard */
    dashboard: (signal?: AbortSignal) =>
        request<DashboardSummary>("/dashboard", { signal }),

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
    updateLeadStage: (id: number, stage: Stage) =>
        request<Lead>(`/leads/${id}/stage`, {
            method: "PATCH",
            body: JSON.stringify({ stage }),
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
};
