import { useEffect, useState } from "react";
import type { FormEvent, ReactNode } from "react";
import { Link } from "react-router-dom";
import { ArrowLeft, FlaskConical, KeyRound, PlugZap, Power, RotateCcw, Save, Send } from "lucide-react";

import { ApiError, api } from "../lib/api";
import {
    CAPI_HEALTH_STYLES,
    CAPI_STATUS_ORDER,
    CAPI_STATUS_STYLES,
    STAGE_STYLES,
    cx,
    formatDateTime,
    formatRelative,
} from "../lib/display";
import type { CapiSettings, CapiSettingsInput, CapiStatus, Stage, StageEvent } from "../lib/types";
import { useAsync } from "../hooks/useAsync";
import { useToast } from "../context/toast";
import { RoleGate } from "../components/RoleGate";
import { CapiStatusBadge } from "../components/QualityBadges";
import { Badge, Button, Card, CardHeader, EmptyState, ErrorState, Field, PageHeader, Skeleton, TextInput } from "../components/ui";

/** The lifecycle milestones the page offers, in order. Meta's CRM integration
 * expects every stage, the raw lead included, so all three start enabled. */
const MILESTONES: { stage: Stage; label: string; hint: string; defaultName: string }[] = [
    { stage: "new", label: "Lead created", hint: "A lead arrives from a Meta lead form.", defaultName: "Lead" },
    { stage: "qualified", label: "Lead qualified", hint: "The lead reaches Qualified (or later).", defaultName: "Qualified Lead" },
    { stage: "won", label: "Lead converted / won", hint: "The deal is Won.", defaultName: "Converted" },
];

/** Settings > Marketing Integrations > Meta CAPI. */
export function MetaCapiPage() {
    return (
        <>
            <Link
                to="/integrations"
                className="mb-3 inline-flex items-center gap-1 text-xs font-medium text-slate-500 hover:text-slate-800 dark:text-slate-400 dark:hover:text-slate-200"
            >
                <ArrowLeft className="size-3.5" aria-hidden="true" /> Integrations
            </Link>
            <PageHeader
                title="Meta Conversions API"
                subtitle="Tell Meta which of your Meta form leads became qualified and converted, so it can find more like them."
            />
            <RoleGate roles={["admin"]} what="Meta CAPI configuration">
                <CapiSettingsPanel />
            </RoleGate>
        </>
    );
}

function toInput(s: CapiSettings): CapiSettingsInput {
    // Every milestone gets a row, enabled unless saved otherwise.
    const mapping: StageEvent[] = MILESTONES.map((m) => {
        const saved = s.event_mapping.find((e) => e.stage === m.stage);
        return saved ?? { stage: m.stage, event_name: m.defaultName, disabled: true };
    });
    // Keep any other stage an earlier version mapped.
    for (const e of s.event_mapping) {
        if (!MILESTONES.some((m) => m.stage === e.stage)) mapping.push(e);
    }
    return {
        dataset_id: s.dataset_id,
        app_id: s.app_id,
        test_event_code: s.test_event_code,
        test_mode: s.test_mode,
        enabled: s.enabled,
        event_mapping: mapping,
        crm_name: s.crm_name,
        phone_country_code: s.phone_country_code,
        access_token: "",
    };
}

function CapiSettingsPanel() {
    const toast = useToast();
    const settings = useAsync((signal) => api.capiSettings(signal), []);
    const [form, setForm] = useState<CapiSettingsInput | null>(null);
    const [errors, setErrors] = useState<Record<string, string>>({});
    const [busy, setBusy] = useState<"" | "save" | "test" | "event" | "disable" | "token">("");
    const [newToken, setNewToken] = useState<string | null>(null);
    const [lastResult, setLastResult] = useState<{ ok: boolean; text: string } | null>(null);
    const [historyKey, setHistoryKey] = useState(0);

    useEffect(() => {
        if (settings.data) setForm(toInput(settings.data));
    }, [settings.data]);

    if (settings.loading) return <Skeleton className="h-96 w-full rounded-2xl" />;
    if (settings.error) return <ErrorState message={settings.error.message} onRetry={settings.refetch} />;
    if (!settings.data || !form) return null;
    const data = settings.data;
    const health = CAPI_HEALTH_STYLES[data.status];

    const set = <K extends keyof CapiSettingsInput>(key: K, value: CapiSettingsInput[K]) =>
        setForm((f) => (f ? { ...f, [key]: value } : f));
    const setMapping = (i: number, patch: Partial<StageEvent>) =>
        set("event_mapping", form.event_mapping.map((m, j) => (j === i ? { ...m, ...patch } : m)));

    const fail = (cause: unknown, fallback: string) => {
        if (cause instanceof ApiError && cause.details) setErrors(cause.details);
        toast.error(cause instanceof ApiError ? cause.message : fallback);
    };
    const run = async (kind: typeof busy, fn: () => Promise<void>, fallback: string) => {
        setBusy(kind);
        setErrors({});
        try {
            await fn();
        } catch (cause) {
            fail(cause, fallback);
        } finally {
            setBusy("");
        }
    };

    const save = (event: FormEvent) => {
        event.preventDefault();
        void run(
            "save",
            async () => {
                const saved = await api.saveCapiSettings(form);
                settings.setData(saved);
                toast.success("Configuration saved.");
            },
            "The configuration could not be saved.",
        );
    };
    const test = () =>
        run(
            "test",
            async () => {
                const r = await api.testCapi();
                setLastResult({ ok: r.ok, text: r.detail });
                settings.refetch();
            },
            "The connection could not be tested.",
        );
    const testEvent = () =>
        run(
            "event",
            async () => {
                const r = await api.sendCapiTestEvent();
                setLastResult({
                    ok: true,
                    text: `Meta received ${r.events_received} test event "${r.event_name}" (${
                        r.used_lead_id ? "with a real Meta lead id" : "with a hashed example email"
                    }). Look for it in Events Manager > Test Events under code ${r.test_event_code}.`,
                });
                settings.refetch();
            },
            "The test event could not be sent.",
        );
    const rotate = () =>
        run(
            "token",
            async () => {
                const saved = await api.rotateCapiToken(newToken ?? "");
                settings.setData(saved);
                setNewToken(null);
                toast.success("Access token updated. Test the connection to check it.");
            },
            "The token could not be updated.",
        );
    const disable = () =>
        run(
            "disable",
            async () => {
                settings.setData(await api.disableCapi());
                toast.success("Meta CAPI disabled. Queued events wait until it is enabled again.");
            },
            "The integration could not be disabled.",
        );

    return (
        <div className="space-y-6">
            {!data.encryption_ready && (
                <Notice tone="amber">
                    The server has no <Code>SECRETS_ENCRYPTION_KEY</Code>, so a token cannot be saved here yet.
                </Notice>
            )}
            {data.secrets_locked && (
                <Notice tone="red">
                    Saved credentials cannot be decrypted with the server's current key. Enter the access token again.
                </Notice>
            )}

            <Card>
                <CardHeader
                    title="Configuration"
                    subtitle={
                        data.status_at
                            ? `Last checked ${formatRelative(data.status_at)}${data.status_detail ? ` — ${data.status_detail}` : ""}`
                            : "Not checked with Meta yet."
                    }
                    action={
                        <Badge tone={health.badge}>
                            <span className={cx("size-1.5 rounded-full", health.dot)} aria-hidden="true" />
                            {health.label}
                        </Badge>
                    }
                />
                <form onSubmit={save} className="space-y-5 p-5">
                    <div className="grid gap-4 sm:grid-cols-2">
                        <Field
                            id="dataset"
                            label="Meta Pixel / Dataset ID"
                            required
                            error={errors["config.dataset_id"]}
                            hint="Events Manager > Data sources > your dataset."
                        >
                            <TextInput
                                id="dataset"
                                inputMode="numeric"
                                value={form.dataset_id}
                                onChange={(e) => set("dataset_id", e.target.value)}
                                invalid={!!errors["config.dataset_id"]}
                            />
                        </Field>
                        <Field
                            id="app"
                            label="Meta App ID"
                            error={errors["config.app_id"]}
                            hint="Optional. The app your token was made with."
                        >
                            <TextInput
                                id="app"
                                inputMode="numeric"
                                value={form.app_id}
                                onChange={(e) => set("app_id", e.target.value)}
                                invalid={!!errors["config.app_id"]}
                            />
                        </Field>
                    </div>

                    <div className="space-y-1.5">
                        <p className="text-xs font-medium text-slate-700 dark:text-slate-300">
                            CAPI Access Token<span className="ml-0.5 text-rose-500">*</span>
                        </p>
                        {data.access_token.set && newToken === null ? (
                            <div className="flex flex-wrap items-center gap-2">
                                <span className="rounded-lg border border-slate-200 bg-slate-50 px-3 py-2 font-mono text-xs text-slate-600 dark:border-slate-800 dark:bg-slate-950 dark:text-slate-300">
                                    {data.access_token.hint || "••••••••"}
                                </span>
                                <span className="text-xs text-slate-500 dark:text-slate-400">
                                    {data.access_token.source === "env" ? "from the server's .env" : "saved, encrypted"}
                                    {data.token_rotated_at && ` · updated ${formatRelative(data.token_rotated_at)}`}
                                </span>
                                <Button size="sm" onClick={() => setNewToken("")}>
                                    <KeyRound className="size-3.5" aria-hidden="true" />
                                    Update Access Token
                                </Button>
                            </div>
                        ) : data.access_token.set ? (
                            <div className="flex flex-wrap items-center gap-2">
                                <TextInput
                                    type="password"
                                    autoComplete="off"
                                    placeholder="New access token"
                                    value={newToken ?? ""}
                                    onChange={(e) => setNewToken(e.target.value)}
                                    className="max-w-md"
                                    aria-label="New access token"
                                />
                                <Button size="sm" variant="primary" onClick={rotate} loading={busy === "token"} disabled={!newToken}>
                                    Save token
                                </Button>
                                <Button size="sm" variant="ghost" onClick={() => setNewToken(null)}>
                                    Cancel
                                </Button>
                            </div>
                        ) : (
                            <TextInput
                                type="password"
                                autoComplete="off"
                                placeholder="System user access token for the dataset"
                                value={form.access_token}
                                onChange={(e) => set("access_token", e.target.value)}
                                invalid={!!errors["secrets.capi_token"]}
                                aria-label="CAPI access token"
                            />
                        )}
                        {errors["secrets.capi_token"] && (
                            <p className="text-xs text-rose-600 dark:text-rose-400">{errors["secrets.capi_token"]}</p>
                        )}
                        <p className="text-xs text-slate-500 dark:text-slate-400">
                            Stored encrypted on the server and never sent back to this page.
                        </p>
                    </div>

                    <div className="grid gap-4 sm:grid-cols-3">
                        <Field
                            id="test-code"
                            label="Test Event Code"
                            error={errors["config.test_event_code"]}
                            hint="Events Manager > Test Events. Used in test mode only."
                        >
                            <TextInput
                                id="test-code"
                                value={form.test_event_code}
                                onChange={(e) => set("test_event_code", e.target.value)}
                                invalid={!!errors["config.test_event_code"]}
                            />
                        </Field>
                        <Field id="crm-name" label="CRM name sent to Meta" error={errors["config.crm_name"]}>
                            <TextInput id="crm-name" value={form.crm_name} onChange={(e) => set("crm_name", e.target.value)} />
                        </Field>
                        <Field
                            id="cc"
                            label="Default phone country code"
                            error={errors["config.phone_country_code"]}
                            hint="e.g. 91. Numbers without one are otherwise left out."
                        >
                            <TextInput id="cc" value={form.phone_country_code} onChange={(e) => set("phone_country_code", e.target.value)} />
                        </Field>
                    </div>

                    <div className="flex flex-wrap gap-6">
                        <Toggle
                            checked={form.enabled}
                            onChange={(v) => set("enabled", v)}
                            label="Send events to Meta"
                            hint="Off: nothing is sent; events wait."
                        />
                        <Toggle
                            checked={form.test_mode}
                            onChange={(v) => set("test_mode", v)}
                            label="Test mode"
                            hint="Events only appear under Test Events and never count. Turning it off needs a passed test."
                        />
                    </div>

                    <div>
                        <p className="mb-2 text-xs font-medium text-slate-700 dark:text-slate-300">Lifecycle events</p>
                        <div className="divide-y divide-slate-100 rounded-xl border border-slate-200 dark:divide-slate-800 dark:border-slate-800">
                            {form.event_mapping.map((m, i) => {
                                const milestone = MILESTONES.find((x) => x.stage === m.stage);
                                const error =
                                    errors[`config.event_mapping[${i}].event_name`] ?? errors[`config.event_mapping[${i}].stage`];
                                return (
                                    <div key={m.stage} className="flex flex-wrap items-center gap-3 px-4 py-3">
                                        <input
                                            type="checkbox"
                                            className="size-4 accent-brand-600"
                                            checked={!m.disabled}
                                            onChange={(e) => setMapping(i, { disabled: !e.target.checked })}
                                            aria-label={`Send ${milestone?.label ?? STAGE_STYLES[m.stage].label}`}
                                        />
                                        <div className="min-w-48 flex-1">
                                            <p className="text-sm font-medium text-slate-900 dark:text-slate-100">
                                                {milestone?.label ?? STAGE_STYLES[m.stage].label}
                                            </p>
                                            <p className="text-xs text-slate-500 dark:text-slate-400">
                                                {milestone?.hint ?? `The lead reaches ${STAGE_STYLES[m.stage].label}.`}
                                            </p>
                                        </div>
                                        <div className="w-56">
                                            <TextInput
                                                value={m.event_name}
                                                onChange={(e) => setMapping(i, { event_name: e.target.value })}
                                                aria-label="Event name sent to Meta"
                                                invalid={!!error}
                                                disabled={m.disabled}
                                            />
                                            {error && <p className="mt-1 text-xs text-rose-600 dark:text-rose-400">{error}</p>}
                                        </div>
                                    </div>
                                );
                            })}
                        </div>
                        <p className="mt-2 text-xs text-slate-500 dark:text-slate-400">
                            Event names are your stage names in Meta's CRM integration. Meta expects every stage, starting
                            with the raw lead; a lead that skips ahead also sends the earlier enabled ones. Only leads from Meta
                            lead forms are sent, never a lead that opted out, and never Qualified or Converted for a lead
                            confirmed as spam.
                        </p>
                    </div>

                    {lastResult && (
                        <Notice tone={lastResult.ok ? "green" : "red"}>{lastResult.text}</Notice>
                    )}

                    <div className="flex flex-wrap gap-2 border-t border-slate-200 pt-4 dark:border-slate-800">
                        <Button type="submit" variant="primary" loading={busy === "save"}>
                            {busy !== "save" && <Save className="size-4" aria-hidden="true" />}
                            Save Configuration
                        </Button>
                        <Button onClick={test} loading={busy === "test"}>
                            {busy !== "test" && <PlugZap className="size-4" aria-hidden="true" />}
                            Test Connection
                        </Button>
                        <Button onClick={testEvent} loading={busy === "event"}>
                            {busy !== "event" && <FlaskConical className="size-4" aria-hidden="true" />}
                            Send Test Event
                        </Button>
                        {data.enabled && (
                            <Button variant="danger" onClick={disable} loading={busy === "disable"} className="ml-auto">
                                {busy !== "disable" && <Power className="size-4" aria-hidden="true" />}
                                Disable Integration
                            </Button>
                        )}
                    </div>
                </form>
            </Card>

            <DeliveryHistory key={historyKey} counts={data.counts} onChanged={() => setHistoryKey((k) => k + 1)} />
            <AuditLog />
        </div>
    );
}

// ---------------------------------------------------------------------------
// Delivery history
// ---------------------------------------------------------------------------

function DeliveryHistory({ counts, onChanged }: { counts: Record<CapiStatus, number>; onChanged: () => void }) {
    const toast = useToast();
    const [status, setStatus] = useState<CapiStatus | "">("");
    const [from, setFrom] = useState("");
    const [to, setTo] = useState("");
    const [retrying, setRetrying] = useState<number | "all" | null>(null);
    const log = useAsync((signal) => api.capiEvents({ status, from, to }, signal), [status, from, to]);

    const retry = async (id: number | "all") => {
        setRetrying(id);
        try {
            if (id === "all") {
                const r = await api.retryFailedCapiEvents();
                toast.success(`${r.requeued} failed event${r.requeued === 1 ? "" : "s"} queued again.`);
            } else {
                await api.retryCapiEvent(id);
                toast.success("Queued again; it goes out within a minute.");
            }
            log.refetch();
            onChanged();
        } catch (cause) {
            toast.error(cause instanceof ApiError ? cause.message : "Could not retry.");
        } finally {
            setRetrying(null);
        }
    };

    const events = log.data?.events ?? [];
    return (
        <Card>
            <CardHeader
                title="Event delivery history"
                subtitle="Newest first. No customer data is kept here: matching data is hashed and sent, never logged."
                action={
                    counts.failed > 0 && (
                        <Button size="sm" onClick={() => retry("all")} loading={retrying === "all"}>
                            <RotateCcw className="size-3.5" aria-hidden="true" />
                            Retry all failed
                        </Button>
                    )
                }
            />
            <div className="flex flex-wrap items-end gap-3 border-b border-slate-200 px-5 py-3 dark:border-slate-800">
                <div className="inline-flex flex-wrap rounded-lg bg-slate-100 p-0.5 dark:bg-slate-800" role="tablist" aria-label="Status">
                    {(["", ...CAPI_STATUS_ORDER] as const).map((s) => (
                        <button
                            key={s || "all"}
                            type="button"
                            role="tab"
                            aria-selected={status === s}
                            onClick={() => setStatus(s)}
                            className={cx(
                                "rounded-md px-2.5 py-1 text-xs font-medium",
                                status === s
                                    ? "bg-white text-slate-900 shadow-sm dark:bg-slate-950 dark:text-slate-100"
                                    : "text-slate-500 hover:text-slate-800 dark:text-slate-400 dark:hover:text-slate-200",
                            )}
                        >
                            {s ? CAPI_STATUS_STYLES[s].label : "All"}
                            {s && <span className="ml-1 tabular-nums opacity-60">{counts[s] ?? 0}</span>}
                        </button>
                    ))}
                </div>
                <label className="text-xs text-slate-500 dark:text-slate-400">
                    From
                    <TextInput type="date" value={from} onChange={(e) => setFrom(e.target.value)} className="mt-1 h-8 w-40" />
                </label>
                <label className="text-xs text-slate-500 dark:text-slate-400">
                    To
                    <TextInput type="date" value={to} onChange={(e) => setTo(e.target.value)} className="mt-1 h-8 w-40" />
                </label>
            </div>
            {log.error ? (
                <ErrorState message={log.error.message} onRetry={log.refetch} />
            ) : events.length === 0 ? (
                <EmptyState icon={Send} title="No events" description="Events appear here as leads reach the stages above." />
            ) : (
                <div className="overflow-x-auto">
                    <table className="w-full text-left text-sm">
                        <thead className="text-xs text-slate-500 dark:text-slate-400">
                            <tr className="border-b border-slate-200 dark:border-slate-800">
                                <th className="px-5 py-2 font-medium">Lead</th>
                                <th className="px-3 py-2 font-medium">Event</th>
                                <th className="px-3 py-2 font-medium">Status</th>
                                <th className="px-3 py-2 font-medium">Attempts</th>
                                <th className="px-3 py-2 font-medium">Details</th>
                                <th className="px-3 py-2 font-medium">When</th>
                                <th className="px-5 py-2" />
                            </tr>
                        </thead>
                        <tbody className="divide-y divide-slate-100 dark:divide-slate-800">
                            {events.map((e) => (
                                <tr key={e.id} className="align-top">
                                    <td className="px-5 py-2.5">
                                        <Link
                                            to={`/leads/${e.lead_id}`}
                                            className="font-medium text-slate-900 hover:underline dark:text-slate-100"
                                        >
                                            {e.lead_name || `Lead ${e.lead_id}`}
                                        </Link>
                                    </td>
                                    <td className="px-3 py-2.5">
                                        <span className="text-slate-800 dark:text-slate-200">{e.event_name}</span>
                                        {e.test_mode && <span className="ml-1 text-xs text-slate-400">(test)</span>}
                                    </td>
                                    <td className="px-3 py-2.5">
                                        <CapiStatusBadge status={e.status} />
                                    </td>
                                    <td className="px-3 py-2.5 tabular-nums text-slate-600 dark:text-slate-300">{e.attempts}</td>
                                    <td className="max-w-xs px-3 py-2.5 text-xs text-slate-500 dark:text-slate-400">
                                        {e.error_code !== null && <span className="font-mono">#{e.error_code} </span>}
                                        {e.last_error}
                                        {e.next_attempt_at && <> Next try {formatRelative(e.next_attempt_at)}.</>}
                                    </td>
                                    <td className="px-3 py-2.5 text-xs whitespace-nowrap text-slate-500 dark:text-slate-400" title={formatDateTime(e.created_at)}>
                                        {formatRelative(e.sent_at ?? e.created_at)}
                                    </td>
                                    <td className="px-5 py-2.5 text-right">
                                        {(e.status === "failed" || e.status === "retry_scheduled") && (
                                            <Button size="sm" onClick={() => retry(e.id)} loading={retrying === e.id}>
                                                Retry
                                            </Button>
                                        )}
                                    </td>
                                </tr>
                            ))}
                        </tbody>
                    </table>
                </div>
            )}
        </Card>
    );
}

function AuditLog() {
    const audit = useAsync((signal) => api.capiAudit(signal), []);
    const entries = audit.data ?? [];
    return (
        <Card>
            <CardHeader title="Audit log" subtitle="Changes to the integration's settings and credentials. Tokens are never recorded." />
            {entries.length === 0 ? (
                <p className="px-5 py-4 text-sm text-slate-500 dark:text-slate-400">Nothing yet.</p>
            ) : (
                <ul className="divide-y divide-slate-100 text-sm dark:divide-slate-800">
                    {entries.slice(0, 20).map((e) => (
                        <li key={e.id} className="flex flex-wrap items-baseline gap-x-3 px-5 py-2">
                            <span className="font-mono text-xs text-slate-700 dark:text-slate-200">{e.action}</span>
                            <span className="text-xs text-slate-500 dark:text-slate-400">{e.actor}</span>
                            <span className="ml-auto text-xs text-slate-400" title={formatDateTime(e.created_at)}>
                                {formatRelative(e.created_at)}
                            </span>
                        </li>
                    ))}
                </ul>
            )}
        </Card>
    );
}

// ---------------------------------------------------------------------------
// Bits
// ---------------------------------------------------------------------------

function Toggle({ checked, onChange, label, hint }: { checked: boolean; onChange: (v: boolean) => void; label: string; hint: string }) {
    return (
        <label className="flex max-w-xs cursor-pointer items-start gap-2.5">
            <input type="checkbox" className="mt-0.5 size-4 accent-brand-600" checked={checked} onChange={(e) => onChange(e.target.checked)} />
            <span>
                <span className="block text-sm font-medium text-slate-900 dark:text-slate-100">{label}</span>
                <span className="block text-xs text-slate-500 dark:text-slate-400">{hint}</span>
            </span>
        </label>
    );
}

const NOTICE_TONES = {
    green: "border-emerald-200 bg-emerald-50 text-emerald-800 dark:border-emerald-900 dark:bg-emerald-950/50 dark:text-emerald-200",
    amber: "border-amber-200 bg-amber-50 text-amber-800 dark:border-amber-900 dark:bg-amber-950/50 dark:text-amber-200",
    red: "border-rose-200 bg-rose-50 text-rose-800 dark:border-rose-900 dark:bg-rose-950/50 dark:text-rose-200",
};

function Notice({ tone, children }: { tone: keyof typeof NOTICE_TONES; children: ReactNode }) {
    return <p className={cx("rounded-lg border px-3 py-2 text-xs leading-relaxed", NOTICE_TONES[tone])}>{children}</p>;
}

function Code({ children }: { children: ReactNode }) {
    return <code className="rounded bg-black/5 px-1 font-mono text-[11px] dark:bg-white/10">{children}</code>;
}
