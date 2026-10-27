import { useState } from "react";
import type { ReactNode } from "react";
import { Link } from "react-router-dom";
import {
    ChevronDown,
    CircleAlert,
    Copy,
    Info,
    Megaphone,
    RefreshCw,
    RotateCcw,
    Search,
    Send,
} from "lucide-react";

import { ApiError, api } from "../lib/api";
import { avatarColor, cx, formatDateTime, formatRelative, initials } from "../lib/display";
import type { AdLead, AdProvider, IntegrationStatus } from "../lib/types";
import { useAsync } from "../hooks/useAsync";
import { useToast } from "../context/toast";
import { Badge, Button, Card, EmptyState, ErrorState, PageHeader, Select, Skeleton } from "../components/ui";
import { GoogleSheetsCard } from "../components/GoogleSheetsCard";
import { PopOnClick } from "../components/PopOnClick";

const PROVIDERS: Record<
    AdProvider,
    { label: string; title: string; blurb: string; icon: typeof Megaphone; tile: string }
> = {
    meta: {
        label: "Meta",
        title: "Meta Lead Ads",
        blurb: "Facebook and Instagram lead forms",
        icon: Megaphone,
        tile: "bg-blue-600 text-white dark:bg-blue-500",
    },
    google: {
        label: "Google",
        title: "Google Ads",
        blurb: "Lead forms on search and YouTube ads",
        icon: Search,
        tile: "bg-amber-500 text-white dark:bg-amber-500",
    },
};

const STATUS_STYLES: Record<AdLead["status"], { label: string; tone: string }> = {
    created: {
        label: "New lead",
        tone: "bg-emerald-50 text-emerald-700 ring-emerald-200 dark:bg-emerald-950 dark:text-emerald-300 dark:ring-emerald-800",
    },
    duplicate: {
        label: "Already a lead",
        tone: "bg-sky-50 text-sky-700 ring-sky-200 dark:bg-sky-950 dark:text-sky-300 dark:ring-sky-800",
    },
    test: {
        label: "Test",
        tone: "bg-slate-100 text-slate-600 ring-slate-200 dark:bg-slate-800 dark:text-slate-300 dark:ring-slate-700",
    },
    failed: {
        label: "Failed",
        tone: "bg-rose-50 text-rose-700 ring-rose-200 dark:bg-rose-950 dark:text-rose-300 dark:ring-rose-800",
    },
};

type Filter = "all" | AdProvider | "failed";

const FILTERS: { value: Filter; label: string }[] = [
    { value: "all", label: "All sources" },
    { value: "meta", label: "Meta" },
    { value: "google", label: "Google" },
    { value: "failed", label: "Failed only" },
];

/**
 * Leads from ad lead forms on Meta (Facebook / Instagram) and Google Ads. They
 * arrive by webhook; this page says whether each is connected, where its
 * webhook is, and what has arrived.
 */
export function IntegrationsPage() {
    const toast = useToast();
    const summary = useAsync((signal) => api.integrations(signal), []);
    const [testing, setTesting] = useState<AdProvider | null>(null);
    const [retrying, setRetrying] = useState<number | null>(null);
    const [filter, setFilter] = useState<Filter>("all");
    const [listOpen, setListOpen] = useState(false);

    const sendTest = async (provider: AdProvider) => {
        setTesting(provider);
        try {
            await api.sendTestAdLead(provider);
            toast.success(`Test ${PROVIDERS[provider].label} lead received. It's in the list below and made no lead.`);
            summary.refetch();
        } catch (cause) {
            toast.error(cause instanceof ApiError ? cause.message : "The test lead could not be sent.");
        } finally {
            setTesting(null);
        }
    };

    const retry = async (record: AdLead) => {
        setRetrying(record.id);
        try {
            const result = await api.retryAdLead(record.id);
            if (result.status === "failed") toast.error(`Still failing: ${result.error}`);
            else toast.success(result.status === "created" ? `${result.lead_name || "The lead"} added.` : "Done.");
            summary.refetch();
        } catch (cause) {
            toast.error(cause instanceof ApiError ? cause.message : "Could not retry.");
        } finally {
            setRetrying(null);
        }
    };

    const data = summary.data;
    const recent = data?.recent ?? [];
    const shown = recent.filter((r) =>
        filter === "all" ? true : filter === "failed" ? r.status === "failed" : r.provider === filter,
    );
    const count = (status: AdLead["status"]) => recent.filter((r) => r.status === status).length;

    return (
        <>
            <PageHeader
                title="Integrations"
                subtitle="Leads from your Facebook, Instagram and Google ad forms arrive here by themselves."
                action={
                    <Button size="sm" onClick={summary.refetch} loading={summary.fetching}>
                        {!summary.fetching && <RefreshCw className="size-3.5" aria-hidden="true" />}
                        Refresh
                    </Button>
                }
            />

            {summary.loading ? (
                <div className="grid gap-4 sm:grid-cols-2">
                    <Skeleton className="h-52 w-full rounded-2xl" />
                    <Skeleton className="h-52 w-full rounded-2xl" />
                </div>
            ) : summary.error ? (
                <ErrorState message={summary.error.message} onRetry={summary.refetch} />
            ) : data ? (
                <div className="space-y-6">
                    <div className="grid items-start gap-4 sm:grid-cols-2">
                        {(["meta", "google"] as const).map((provider) => (
                            <PopOnClick key={provider} index={provider === "meta" ? 0 : 1}>
                                <ProviderCard
                                    provider={provider}
                                    status={data[provider]}
                                    publicURL={data.public_url}
                                    testing={testing === provider}
                                    onTest={() => sendTest(provider)}
                                />
                            </PopOnClick>
                        ))}
                    </div>

                    <PopOnClick index={2}>
                        <GoogleSheetsCard />
                    </PopOnClick>

                    <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
                        <Stat label="Received" value={recent.length} />
                        <Stat label="New leads" value={count("created")} tone="text-emerald-600 dark:text-emerald-400" />
                        <Stat label="Already leads" value={count("duplicate")} tone="text-sky-600 dark:text-sky-400" />
                        <Stat label="Failed" value={count("failed")} tone="text-rose-600 dark:text-rose-400" />
                    </div>

                    <Card>
                        {/* One dropdown: closed, it is a single line; open, the list and its filter. */}
                        <div
                            className={cx(
                                "flex items-center justify-between gap-3 px-5 py-3.5",
                                listOpen && "border-b border-slate-200 dark:border-slate-800",
                            )}
                        >
                            <button
                                type="button"
                                onClick={() => setListOpen((o) => !o)}
                                aria-expanded={listOpen}
                                className="flex min-w-0 flex-1 items-center gap-2 text-left"
                            >
                                <ChevronDown
                                    className={cx(
                                        "size-4 shrink-0 text-slate-500 transition-transform",
                                        !listOpen && "-rotate-90",
                                    )}
                                    aria-hidden="true"
                                />
                                <span className="min-w-0">
                                    <span className="block text-sm font-semibold text-slate-900 dark:text-slate-100">
                                        Latest ad leads ({recent.length})
                                    </span>
                                    <span className="block text-xs text-slate-500 dark:text-slate-400">
                                        {listOpen
                                            ? "Newest first. A lead sent twice is only taken once."
                                            : "Click to show them."}
                                    </span>
                                </span>
                            </button>
                            {listOpen && (
                                <Select
                                    aria-label="Show"
                                    value={filter}
                                    onChange={(e) => setFilter(e.target.value as Filter)}
                                    className="h-8 w-auto text-xs"
                                >
                                    {FILTERS.map((f) => (
                                        <option key={f.value} value={f.value}>
                                            {f.label}
                                        </option>
                                    ))}
                                </Select>
                            )}
                        </div>

                        {!listOpen ? null : shown.length === 0 ? (
                            <EmptyState
                                icon={Megaphone}
                                title={recent.length === 0 ? "Nothing received yet" : "Nothing here"}
                                description={
                                    recent.length === 0
                                        ? "Ad form leads appear here as they arrive. Send a test lead to see how one looks."
                                        : "No ad leads match this filter."
                                }
                            />
                        ) : (
                            <ul className="divide-y divide-slate-100 dark:divide-slate-800">
                                {shown.map((record) => (
                                    <AdLeadRow
                                        key={record.id}
                                        record={record}
                                        retrying={retrying === record.id}
                                        onRetry={() => retry(record)}
                                    />
                                ))}
                            </ul>
                        )}
                    </Card>
                </div>
            ) : null}
        </>
    );
}

// ---------------------------------------------------------------------------
// Platform cards
// ---------------------------------------------------------------------------

function ProviderCard({
    provider,
    status,
    publicURL,
    testing,
    onTest,
}: {
    provider: AdProvider;
    status: IntegrationStatus;
    publicURL: string;
    testing: boolean;
    onTest: () => void;
}) {
    const toast = useToast();
    const [open, setOpen] = useState(false);
    const { title, blurb, icon: Icon, tile } = PROVIDERS[provider];

    const copy = async () => {
        try {
            await navigator.clipboard.writeText(status.webhook_url);
            toast.success("Webhook address copied.");
        } catch {
            toast.error("Could not copy. Select the address and copy it by hand.");
        }
    };

    return (
        <Card className="overflow-hidden">
            <div className="space-y-4 p-5">
                <div className="flex items-start gap-3">
                    <span className={cx("flex size-10 shrink-0 items-center justify-center rounded-xl", tile)}>
                        <Icon className="size-5" aria-hidden="true" />
                    </span>
                    <div className="min-w-0 flex-1">
                        <p className="text-sm font-semibold text-slate-900 dark:text-slate-100">{title}</p>
                        <p className="text-xs text-slate-500 dark:text-slate-400">{blurb}</p>
                    </div>
                    <ConnectionPill connected={status.configured} />
                </div>

                <div>
                    <p className="mb-1.5 text-xs font-medium text-slate-600 dark:text-slate-400">Webhook address</p>
                    {status.webhook_url ? (
                        <div className="flex items-center gap-1 rounded-lg border border-slate-200 bg-slate-50 py-1 pr-1 pl-3 dark:border-slate-800 dark:bg-slate-950">
                            <span className="min-w-0 flex-1 truncate font-mono text-xs text-slate-700 dark:text-slate-300">
                                {status.webhook_url}
                            </span>
                            <Button size="sm" variant="ghost" onClick={copy} aria-label={`Copy the ${title} webhook address`}>
                                <Copy className="size-3.5" aria-hidden="true" />
                            </Button>
                        </div>
                    ) : (
                        <p className="flex items-start gap-1.5 rounded-lg border border-dashed border-slate-300 px-3 py-2 text-xs text-slate-500 dark:border-slate-700 dark:text-slate-400">
                            <Info className="mt-px size-3.5 shrink-0" aria-hidden="true" />
                            Appears once the server has a public address. See How to connect.
                        </p>
                    )}
                </div>

                <div className="flex flex-wrap items-center gap-2">
                    <Button size="sm" onClick={onTest} loading={testing}>
                        {!testing && <Send className="size-3.5" aria-hidden="true" />}
                        Send test lead
                    </Button>
                    <Button size="sm" variant="ghost" onClick={() => setOpen((o) => !o)} aria-expanded={open}>
                        How to connect
                        <ChevronDown
                            className={cx("size-3.5 transition-transform", open && "rotate-180")}
                            aria-hidden="true"
                        />
                    </Button>
                </div>
            </div>

            {open && (
                <div className="border-t border-slate-200 bg-slate-50/70 px-5 py-4 dark:border-slate-800 dark:bg-slate-950/40">
                    <Steps>
                        {!publicURL && (
                            <Step>
                                Give the server a public https address: deploy it, or while testing run a tunnel such
                                as <Code>ngrok http 8080</Code>. Put it in <Code>PUBLIC_API_URL</Code>.
                            </Step>
                        )}
                        {provider === "meta" ? (
                            <>
                                <Step>
                                    At <Strong>developers.facebook.com</Strong>, create a Business app and add{" "}
                                    <Strong>Webhooks</Strong>.
                                </Step>
                                <Step>
                                    Subscribe the <Strong>Page</Strong> object to <Code>leadgen</Code>, using the
                                    webhook address and your verify token.
                                </Step>
                                <Step>
                                    Give the app <Code>leads_retrieval</Code>, connect your Page and create a
                                    long-lived Page access token.
                                </Step>
                            </>
                        ) : (
                            <>
                                <Step>Choose a key: any long random phrase.</Step>
                                <Step>
                                    In Google Ads, open the lead form and choose the <Strong>webhook</Strong> lead
                                    delivery. Paste the webhook address and the key.
                                </Step>
                            </>
                        )}
                        <Step>
                            {status.missing.length > 0 ? (
                                <>
                                    Add to <Code>backend/.env</Code> and restart the server:
                                    <span className="mt-1.5 flex flex-wrap gap-1">
                                        {status.missing.map((name) => (
                                            <Code key={name}>{name}</Code>
                                        ))}
                                    </span>
                                </>
                            ) : (
                                <>
                                    Settings are in <Code>backend/.env</Code>. Done.
                                </>
                            )}
                        </Step>
                        <Step>
                            {provider === "meta" ? (
                                <>
                                    Try it with Meta's <Strong>Lead Ads Testing Tool</Strong>.
                                </>
                            ) : (
                                <>
                                    Click <Strong>Send test data</Strong> in Google Ads.
                                </>
                            )}{" "}
                            The lead shows up below.
                        </Step>
                    </Steps>
                </div>
            )}
        </Card>
    );
}

function ConnectionPill({ connected }: { connected: boolean }) {
    return (
        <span
            className={cx(
                "inline-flex shrink-0 items-center gap-1.5 rounded-full px-2.5 py-1 text-[11px] font-medium ring-1 ring-inset",
                connected
                    ? "bg-emerald-50 text-emerald-700 ring-emerald-200 dark:bg-emerald-950 dark:text-emerald-300 dark:ring-emerald-800"
                    : "bg-amber-50 text-amber-700 ring-amber-200 dark:bg-amber-950/60 dark:text-amber-300 dark:ring-amber-800",
            )}
        >
            <span
                className={cx("size-1.5 rounded-full", connected ? "bg-emerald-500" : "bg-amber-500")}
                aria-hidden="true"
            />
            {connected ? "Connected" : "Not connected"}
        </span>
    );
}

/** A numbered list whose numbers sit in small circles. */
function Steps({ children }: { children: ReactNode }) {
    return <ol className="space-y-3 [counter-reset:step]">{children}</ol>;
}

function Step({ children }: { children: ReactNode }) {
    return (
        <li className="flex gap-3 text-xs leading-relaxed text-slate-600 [counter-increment:step] before:flex before:size-5 before:shrink-0 before:items-center before:justify-center before:rounded-full before:bg-white before:text-[10px] before:font-semibold before:text-slate-600 before:ring-1 before:ring-slate-300 before:content-[counter(step)] dark:text-slate-300 dark:before:bg-slate-900 dark:before:text-slate-300 dark:before:ring-slate-700">
            <span className="min-w-0 pt-0.5">{children}</span>
        </li>
    );
}

function Code({ children }: { children: ReactNode }) {
    return (
        <code className="rounded bg-slate-200/70 px-1 py-px font-mono text-[11px] text-slate-800 dark:bg-slate-800 dark:text-slate-200">
            {children}
        </code>
    );
}

function Strong({ children }: { children: ReactNode }) {
    return <span className="font-medium text-slate-900 dark:text-slate-100">{children}</span>;
}

function Stat({ label, value, tone }: { label: string; value: number; tone?: string }) {
    return (
        <div className="rounded-xl border border-slate-200 bg-white px-4 py-3 dark:border-slate-800 dark:bg-slate-900">
            <p className={cx("text-xl font-semibold tabular-nums", tone ?? "text-slate-900 dark:text-slate-100")}>
                {value}
            </p>
            <p className="text-xs text-slate-500 dark:text-slate-400">{label}</p>
        </div>
    );
}

// ---------------------------------------------------------------------------
// Received leads
// ---------------------------------------------------------------------------

function AdLeadRow({ record, retrying, onRetry }: { record: AdLead; retrying: boolean; onRetry: () => void }) {
    const [open, setOpen] = useState(false);
    const { label: providerLabel, icon: Icon } = PROVIDERS[record.provider];
    const status = STATUS_STYLES[record.status];
    const answer = (keys: string[]) => record.answers.find((a) => keys.includes(a.key.toLowerCase()))?.value ?? "";
    const email = answer(["email"]);
    const who =
        record.lead_name ||
        answer(["full_name", "name"]) ||
        [answer(["first_name"]), answer(["last_name"])].filter(Boolean).join(" ") ||
        email ||
        `${providerLabel} lead`;

    return (
        <li className="px-5 py-3">
            <div className="flex items-center gap-3">
                <span
                    className={cx(
                        "flex size-8 shrink-0 items-center justify-center rounded-full text-[11px] font-semibold",
                        avatarColor(who),
                    )}
                    aria-hidden="true"
                >
                    {initials(who)}
                </span>

                <div className="min-w-0 flex-1">
                    <div className="flex min-w-0 items-center gap-2">
                        {record.lead_id && record.lead_name ? (
                            <Link
                                to={`/leads/${record.lead_id}`}
                                className="truncate text-sm font-medium text-slate-900 hover:text-brand-700 hover:underline dark:text-slate-100 dark:hover:text-brand-400"
                            >
                                {who}
                            </Link>
                        ) : (
                            <span className="truncate text-sm font-medium text-slate-900 dark:text-slate-100">{who}</span>
                        )}
                        <Badge tone={status.tone}>{status.label}</Badge>
                    </div>
                    <p className="mt-0.5 flex min-w-0 items-center gap-1 truncate text-xs text-slate-500 dark:text-slate-400">
                        <Icon className="size-3 shrink-0" aria-hidden="true" />
                        <span className="truncate">
                            {[providerLabel, email !== who ? email : "", record.campaign && `campaign ${record.campaign}`]
                                .filter(Boolean)
                                .join(" · ")}
                        </span>
                    </p>
                </div>

                <div className="flex shrink-0 items-center gap-1">
                    <span
                        className="hidden text-xs whitespace-nowrap text-slate-400 sm:inline dark:text-slate-500"
                        title={formatDateTime(record.received_at)}
                    >
                        {formatRelative(record.received_at)}
                    </span>
                    {record.status === "failed" && (
                        <Button size="sm" onClick={onRetry} loading={retrying}>
                            {!retrying && <RotateCcw className="size-3.5" aria-hidden="true" />}
                            Retry
                        </Button>
                    )}
                    {record.answers.length > 0 && (
                        <Button
                            size="sm"
                            variant="ghost"
                            onClick={() => setOpen((o) => !o)}
                            aria-expanded={open}
                            aria-label={open ? "Hide answers" : "Show answers"}
                        >
                            <ChevronDown className={cx("size-4 transition-transform", open && "rotate-180")} aria-hidden="true" />
                        </Button>
                    )}
                </div>
            </div>

            {record.error && (
                <p className="mt-2 ml-11 flex items-start gap-1.5 text-xs text-rose-600 dark:text-rose-400">
                    <CircleAlert className="mt-px size-3.5 shrink-0" aria-hidden="true" />
                    {record.error}
                </p>
            )}

            {open && (
                <dl className="mt-2 ml-11 grid grid-cols-[minmax(6rem,auto)_1fr] gap-x-4 gap-y-1 rounded-lg bg-slate-50 px-3 py-2 text-xs dark:bg-slate-950/50">
                    {record.answers.map((a, i) => (
                        <div key={i} className="contents">
                            <dt className="text-slate-500 dark:text-slate-400">{a.label}</dt>
                            <dd className="break-words text-slate-800 dark:text-slate-200">{a.value || "—"}</dd>
                        </div>
                    ))}
                </dl>
            )}
        </li>
    );
}
