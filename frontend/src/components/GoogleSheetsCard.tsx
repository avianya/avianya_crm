import { useEffect, useState } from "react";
import type { FormEvent } from "react";
import {
    ArrowDown,
    ArrowUp,
    CircleAlert,
    ExternalLink,
    FolderOpen,
    Plus,
    RefreshCw,
    RotateCcw,
    Settings2,
    Unplug,
} from "lucide-react";

import { ApiError, api } from "../lib/api";
import { STAGE_STYLES, cx, formatDateTime, formatRelative } from "../lib/display";
import type { SheetSync, SheetsSettings, SheetsView, Spreadsheet, Stage } from "../lib/types";
import { useAsync } from "../hooks/useAsync";
import { useToast } from "../context/toast";
import { ConfirmDialog } from "./ConfirmDialog";
import { Modal, ModalBody, ModalFooter } from "./Modal";
import { Badge, Button, Card, ErrorState, Field, Select, Skeleton, TextInput } from "./ui";

/** The Google Sheets logo: a green sheet with a folded corner and a table. */
export function GoogleSheetsIcon({ className }: { className?: string }) {
    return (
        <svg viewBox="0 0 48 64" className={className} aria-hidden="true">
            <path d="M29 0H5a5 5 0 0 0-5 5v54a5 5 0 0 0 5 5h38a5 5 0 0 0 5-5V19L29 0z" fill="#0F9D58" />
            <path d="M29 0v14a5 5 0 0 0 5 5h14L29 0z" fill="#87CEAC" />
            <path d="M11 29h26v22H11z" fill="#F1F1F1" />
            <path d="M11 36h26M11 43.5h26M22 29v22" stroke="#0F9D58" strokeWidth="2.2" />
        </svg>
    );
}

const STATUS = {
    connected: { label: "Connected", tone: "bg-emerald-50 text-emerald-700 ring-emerald-200 dark:bg-emerald-950 dark:text-emerald-300 dark:ring-emerald-800" },
    not_connected: { label: "Not connected", tone: "bg-slate-100 text-slate-600 ring-slate-200 dark:bg-slate-800 dark:text-slate-300 dark:ring-slate-700" },
    error: { label: "Error", tone: "bg-rose-50 text-rose-700 ring-rose-200 dark:bg-rose-950 dark:text-rose-300 dark:ring-rose-800" },
} as const;

const SYNC_STATUS: Record<SheetSync["status"], { label: string; tone: string }> = {
    running: { label: "Running", tone: STATUS.not_connected.tone },
    success: { label: "Done", tone: STATUS.connected.tone },
    partial: { label: "Partly done", tone: "bg-amber-50 text-amber-700 ring-amber-200 dark:bg-amber-950 dark:text-amber-300 dark:ring-amber-800" },
    failed: { label: "Failed", tone: STATUS.error.tone },
};

const TRIGGERS: Record<SheetSync["trigger"], string> = { manual: "Sync now", auto: "Automatic", retry: "Retry" };

/**
 * Google Sheets: connect a Google account, choose a sheet, and export leads
 * to it. Admins only, like the rest of Integrations.
 */
export function GoogleSheetsCard() {
    const toast = useToast();
    const view = useAsync((signal) => api.sheets(signal), []);
    const [busy, setBusy] = useState<"connect" | "test" | "sync" | null>(null);
    const [configuring, setConfiguring] = useState(false);
    const [disconnecting, setDisconnecting] = useState(false);
    const [retrying, setRetrying] = useState<number | null>(null);
    const [testMessage, setTestMessage] = useState<{ ok: boolean; text: string } | null>(null);

    // Google sends the admin back to /integrations?google_sheets=connected
    // (or google_sheets_error=...): say so once, then tidy the address.
    useEffect(() => {
        const params = new URLSearchParams(window.location.search);
        const ok = params.get("google_sheets");
        const problem = params.get("google_sheets_error");
        if (!ok && !problem) return;
        if (ok === "connected") toast.success("Google account connected. Now choose a spreadsheet in Configure.");
        if (problem) toast.error(problem);
        params.delete("google_sheets");
        params.delete("google_sheets_error");
        const rest = params.toString();
        window.history.replaceState(null, "", window.location.pathname + (rest ? `?${rest}` : ""));
    }, [toast]);

    // While a sync runs, check on it every two seconds.
    const running = view.data?.syncs.some((s) => s.status === "running") ?? false;
    useEffect(() => {
        if (!running) return;
        const timer = window.setInterval(view.refetch, 2000);
        return () => window.clearInterval(timer);
    }, [running, view.refetch]);

    const fail = (cause: unknown, fallback: string) =>
        toast.error(cause instanceof ApiError ? cause.message : fallback);

    const connect = async () => {
        setBusy("connect");
        try {
            const { url } = await api.connectSheets();
            window.location.assign(url); // to Google's consent page, and back
        } catch (cause) {
            fail(cause, "Could not start connecting Google.");
            setBusy(null);
        }
    };

    const test = async () => {
        setBusy("test");
        setTestMessage(null);
        try {
            const result = await api.testSheets();
            setTestMessage({ ok: result.ok, text: result.message });
            view.refetch();
        } catch (cause) {
            fail(cause, "Could not test the connection.");
        } finally {
            setBusy(null);
        }
    };

    const sync = async () => {
        setBusy("sync");
        try {
            await api.syncSheets();
            toast.success("Sync started.");
            view.refetch();
        } catch (cause) {
            fail(cause, "Could not start the sync.");
        } finally {
            setBusy(null);
        }
    };

    const retry = async (id: number) => {
        setRetrying(id);
        try {
            await api.retrySheetSync(id);
            toast.success("Retrying.");
            view.refetch();
        } catch (cause) {
            fail(cause, "Could not retry.");
        } finally {
            setRetrying(null);
        }
    };

    const disconnect = async () => {
        try {
            view.setData(await api.disconnectSheets());
            setTestMessage(null);
            toast.success("Google disconnected. Automatic syncs have stopped.");
        } catch (cause) {
            fail(cause, "Could not disconnect.");
            throw cause;
        }
    };

    if (view.loading && !view.data) return <Skeleton className="h-48 w-full rounded-2xl" />;
    if (view.error) return <ErrorState message={view.error.message} onRetry={view.refetch} />;
    const { settings: s, syncs } = view.data!;
    const connected = s.status !== "not_connected" && !s.token_locked;
    const status = STATUS[s.status];
    const ready = connected && s.spreadsheet_id !== "" && s.sheet_title !== "";
    const lastSync = syncs.find((y) => y.status !== "running");

    return (
        <Card>
            <div className="flex flex-wrap items-start gap-4 px-5 py-4">
                <GoogleSheetsIcon className="h-11 w-8 shrink-0" />
                <div className="min-w-0 flex-1">
                    <div className="flex flex-wrap items-center gap-2">
                        <h2 className="text-sm font-semibold text-slate-900 dark:text-slate-100">Google Sheets</h2>
                        <Badge tone={status.tone}>{status.label}</Badge>
                    </div>
                    <p className="text-xs text-slate-500 dark:text-slate-400">
                        Export leads to a spreadsheet. Each lead keeps one row, matched by its CRM Lead ID.
                    </p>
                    {connected && (
                        <p className="mt-1 text-xs text-slate-600 dark:text-slate-300">
                            Google account <strong>{s.account_email}</strong>
                            {s.configured_by_name && <> · connected by {s.configured_by_name}</>}
                        </p>
                    )}
                    {ready && (
                        <p className="mt-0.5 text-xs text-slate-600 dark:text-slate-300">
                            Sheet{" "}
                            <a
                                href={s.spreadsheet_url}
                                target="_blank"
                                rel="noreferrer"
                                className="inline-flex items-center gap-0.5 font-medium text-brand-600 hover:underline dark:text-brand-400"
                            >
                                {s.spreadsheet_name} › {s.sheet_title}
                                <ExternalLink className="size-3" aria-hidden="true" />
                            </a>
                            {s.auto_sync ? ` · syncs every ${s.auto_sync_minutes} minutes` : " · automatic sync off"}
                        </p>
                    )}
                </div>
                <div className="flex flex-wrap gap-2">
                    {!connected ? (
                        <Button
                            size="sm"
                            variant="primary"
                            loading={busy === "connect"}
                            disabled={!s.oauth_configured || !s.encryption_ready}
                            onClick={connect}
                        >
                            Connect Google account
                        </Button>
                    ) : (
                        <>
                            <Button size="sm" onClick={() => setConfiguring(true)}>
                                <Settings2 className="size-3.5" aria-hidden="true" />
                                Configure
                            </Button>
                            <Button size="sm" loading={busy === "test"} onClick={test}>
                                Test connection
                            </Button>
                            <Button size="sm" variant="primary" loading={busy === "sync" || running} disabled={!ready} onClick={sync}>
                                {busy !== "sync" && !running && <RefreshCw className="size-3.5" aria-hidden="true" />}
                                {running ? "Syncing…" : "Sync now"}
                            </Button>
                            <Button size="sm" variant="ghost" onClick={() => setDisconnecting(true)}>
                                <Unplug className="size-3.5" aria-hidden="true" />
                                Disconnect
                            </Button>
                        </>
                    )}
                </div>
            </div>

            <div className="space-y-2 border-t border-slate-100 px-5 py-3 text-xs dark:border-slate-800">
                {!s.oauth_configured || !s.encryption_ready ? (
                    <SetupNeeded settings={s} />
                ) : s.token_locked ? (
                    <Problem text="A Google sign-in is saved but cannot be read (SECRETS_ENCRYPTION_KEY changed). Disconnect and connect again." />
                ) : !connected ? (
                    <p className="text-slate-500 dark:text-slate-400">
                        Connecting opens Google. The CRM asks only for the spreadsheets you create or choose through it, not
                        the rest of your Drive.
                    </p>
                ) : (
                    <>
                        {s.status === "error" && s.last_error && <Problem text={s.last_error} />}
                        {testMessage && (testMessage.ok ? <p className="text-emerald-700 dark:text-emerald-400">{testMessage.text}</p> : <Problem text={testMessage.text} />)}
                        {!ready && <p className="text-slate-500 dark:text-slate-400">Next: Configure, to choose a spreadsheet and the columns.</p>}
                        <p className="text-slate-500 dark:text-slate-400">
                            {s.last_sync_at
                                ? `Last successful sync ${formatRelative(s.last_sync_at)}: ${s.last_sync_count ?? 0} leads.`
                                : "Not synced yet."}
                            {lastSync && lastSync.status !== "success" && ` The latest sync ${SYNC_STATUS[lastSync.status].label.toLowerCase()}.`}
                        </p>
                    </>
                )}
            </div>

            {syncs.length > 0 && <History syncs={syncs} retrying={retrying} onRetry={retry} />}

            {configuring && (
                <ConfigureModal
                    settings={s}
                    onClose={() => setConfiguring(false)}
                    onSaved={(next) => {
                        view.setData(next);
                        setConfiguring(false);
                        setTestMessage(null);
                        toast.success("Saved. Use Test connection, then Sync now.");
                    }}
                />
            )}

            <ConfirmDialog
                open={disconnecting}
                onClose={() => setDisconnecting(false)}
                onConfirm={disconnect}
                title="Disconnect Google Sheets"
                message="The CRM's access is removed at Google and the sign-in is deleted here. Automatic syncs stop. Rows already in the sheet stay as they are."
                confirmLabel="Disconnect"
            />
        </Card>
    );
}

function Problem({ text }: { text: string }) {
    return (
        <p className="flex items-start gap-1.5 text-rose-600 dark:text-rose-400">
            <CircleAlert className="mt-px size-3.5 shrink-0" aria-hidden="true" />
            {text}
        </p>
    );
}

function SetupNeeded({ settings }: { settings: SheetsSettings }) {
    return (
        <div className="space-y-1 text-slate-600 dark:text-slate-300">
            <p className="font-medium text-amber-700 dark:text-amber-400">Set up on the server first.</p>
            <p>
                Add to <code className="rounded bg-slate-100 px-1 dark:bg-slate-800">backend/.env</code>:{" "}
                {settings.missing_env.map((name, i) => (
                    <span key={name}>
                        {i > 0 && ", "}
                        <code className="rounded bg-slate-100 px-1 dark:bg-slate-800">{name}</code>
                    </span>
                ))}
                , then restart the server.
            </p>
            <p>
                In Google Cloud, the OAuth client's authorized redirect URI must be exactly{" "}
                <code className="rounded bg-slate-100 px-1 break-all dark:bg-slate-800">{settings.redirect_url}</code>
            </p>
        </div>
    );
}

function History({ syncs, retrying, onRetry }: { syncs: SheetSync[]; retrying: number | null; onRetry: (id: number) => void }) {
    const [open, setOpen] = useState(false);
    const shown = open ? syncs : syncs.slice(0, 3);
    return (
        <div className="border-t border-slate-100 dark:border-slate-800">
            <p className="px-5 pt-3 text-xs font-semibold text-slate-700 dark:text-slate-200">Sync history</p>
            <ul className="divide-y divide-slate-100 dark:divide-slate-800">
                {shown.map((y) => (
                    <li key={y.id} className="px-5 py-2.5 text-xs">
                        <div className="flex flex-wrap items-center gap-2">
                            <Badge tone={SYNC_STATUS[y.status].tone}>{SYNC_STATUS[y.status].label}</Badge>
                            <span className="text-slate-700 dark:text-slate-200">
                                {y.status === "running"
                                    ? `${y.leads_total} leads…`
                                    : `${y.rows_added} added · ${y.rows_updated} updated${y.rows_failed ? ` · ${y.rows_failed} failed` : ""}`}
                            </span>
                            <span className="text-slate-400 dark:text-slate-500" title={formatDateTime(y.started_at)}>
                                {TRIGGERS[y.trigger]}
                                {y.started_by && ` by ${y.started_by}`} · {formatRelative(y.started_at)}
                            </span>
                            {(y.status === "failed" || y.status === "partial") && (
                                <Button size="sm" variant="ghost" className="ml-auto" loading={retrying === y.id} onClick={() => onRetry(y.id)}>
                                    {retrying !== y.id && <RotateCcw className="size-3.5" aria-hidden="true" />}
                                    Retry
                                </Button>
                            )}
                        </div>
                        {y.error && <p className="mt-1 text-rose-600 dark:text-rose-400">{y.error}</p>}
                    </li>
                ))}
            </ul>
            {syncs.length > 3 && (
                <button
                    type="button"
                    onClick={() => setOpen((o) => !o)}
                    className="px-5 pb-3 text-xs font-medium text-brand-600 hover:underline dark:text-brand-400"
                >
                    {open ? "Show fewer" : `Show all ${syncs.length}`}
                </button>
            )}
        </div>
    );
}

// ---------------------------------------------------------------------------
// Configure
// ---------------------------------------------------------------------------

const NEW_TAB = "__new__";

function ConfigureModal({
    settings,
    onClose,
    onSaved,
}: {
    settings: SheetsSettings;
    onClose: () => void;
    onSaved: (view: SheetsView) => void;
}) {
    const toast = useToast();
    const list = useAsync((signal) => api.listSpreadsheets(signal), []);
    const [sheetId, setSheetId] = useState(settings.spreadsheet_id);
    const [extra, setExtra] = useState<Spreadsheet[]>([]); // created or picked just now
    const tabs = useAsync(
        (signal) => (sheetId ? api.getSpreadsheet(sheetId, signal) : Promise.resolve(null)),
        [sheetId],
    );
    const [tab, setTab] = useState(settings.sheet_title || NEW_TAB);
    const [newTab, setNewTab] = useState("CRM Leads");
    const [columns, setColumns] = useState<string[]>(settings.columns);
    const [stages, setStages] = useState<Stage[]>(settings.stages);
    const [autoSync, setAutoSync] = useState(settings.auto_sync);
    const [minutes, setMinutes] = useState(settings.auto_sync_minutes || 15);
    const [newName, setNewName] = useState("CRM Leads");
    const [creating, setCreating] = useState(false);
    const [picking, setPicking] = useState(false);
    const [saving, setSaving] = useState(false);
    const [errors, setErrors] = useState<Record<string, string>>({});

    const sheets = [...extra, ...(list.data ?? []).filter((x) => !extra.some((e) => e.id === x.id))];
    const label = (key: string) => settings.fields.find((f) => f.key === key)?.label ?? key;
    const unused = settings.fields.filter((f) => !columns.includes(f.key));

    // A newly loaded spreadsheet: keep the tab if it has it, else offer a new one.
    useEffect(() => {
        if (tabs.data && !tabs.data.tabs.includes(tab)) setTab(NEW_TAB);
        // eslint-disable-next-line react-hooks/exhaustive-deps -- only when the tabs arrive
    }, [tabs.data]);

    const create = async () => {
        setCreating(true);
        try {
            const sheet = await api.createSpreadsheet(newName.trim() || "CRM Leads");
            setExtra((x) => [sheet, ...x]);
            setSheetId(sheet.id);
            setTab(sheet.tabs[0] ?? NEW_TAB);
        } catch (cause) {
            toast.error(cause instanceof ApiError ? cause.message : "Could not create the spreadsheet.");
        } finally {
            setCreating(false);
        }
    };

    const pick = async () => {
        setPicking(true);
        try {
            const picked = await openPicker();
            if (picked) {
                setExtra((x) => [{ id: picked.id, name: picked.name, url: "", tabs: [] }, ...x]);
                setSheetId(picked.id);
            }
        } catch (cause) {
            toast.error(cause instanceof Error ? cause.message : "Could not open Google Drive.");
        } finally {
            setPicking(false);
        }
    };

    const move = (i: number, by: number) =>
        setColumns((cols) => {
            const next = [...cols];
            [next[i], next[i + by]] = [next[i + by], next[i]];
            return next;
        });

    const submit = async (event: FormEvent) => {
        event.preventDefault();
        setSaving(true);
        setErrors({});
        try {
            onSaved(
                await api.saveSheets({
                    spreadsheet_id: sheetId,
                    sheet_title: tab === NEW_TAB ? "" : tab,
                    new_sheet_title: tab === NEW_TAB ? newTab : "",
                    columns,
                    stages,
                    auto_sync: autoSync,
                    auto_sync_minutes: minutes,
                }),
            );
        } catch (cause) {
            if (cause instanceof ApiError && cause.details) setErrors(cause.details);
            else toast.error(cause instanceof ApiError ? cause.message : "Could not save.");
        } finally {
            setSaving(false);
        }
    };

    return (
        <Modal open onClose={onClose} title="Configure Google Sheets" description="Where leads go, and which fields." size="lg">
            <form onSubmit={submit}>
                <ModalBody>
                    <div className="space-y-5">
                        <Field id="gs-sheet" label="Spreadsheet" error={errors.spreadsheet_id}>
                            {list.loading && !list.data ? (
                                <Skeleton className="h-10 w-full" />
                            ) : (
                                <Select id="gs-sheet" value={sheetId} onChange={(e) => setSheetId(e.target.value)}>
                                    <option value="">Choose a spreadsheet…</option>
                                    {sheets.map((x) => (
                                        <option key={x.id} value={x.id}>
                                            {x.name}
                                        </option>
                                    ))}
                                </Select>
                            )}
                        </Field>
                        {list.error && <Problem text={list.error.message} />}
                        <div className="flex flex-wrap items-end gap-2 text-xs">
                            <div className="w-48">
                                <TextInput aria-label="New spreadsheet name" value={newName} onChange={(e) => setNewName(e.target.value)} />
                            </div>
                            <Button size="sm" loading={creating} onClick={create}>
                                {!creating && <Plus className="size-3.5" aria-hidden="true" />}
                                Create new spreadsheet
                            </Button>
                            {settings.picker_available && (
                                <Button size="sm" loading={picking} onClick={pick}>
                                    {!picking && <FolderOpen className="size-3.5" aria-hidden="true" />}
                                    Choose from Google Drive
                                </Button>
                            )}
                        </div>
                        <p className="-mt-3 text-xs text-slate-500 dark:text-slate-400">
                            The list shows only spreadsheets the CRM created or you chose for it
                            {settings.picker_available ? "." : ". To use another existing spreadsheet, set up the Google Drive picker on the server."}
                        </p>

                        {sheetId && (
                            <div className="grid gap-3 sm:grid-cols-2">
                                <Field id="gs-tab" label="Tab" error={errors.sheet_title}>
                                    <Select id="gs-tab" value={tab} onChange={(e) => setTab(e.target.value)} disabled={tabs.loading}>
                                        {(tabs.data?.tabs ?? []).map((t) => (
                                            <option key={t} value={t}>
                                                {t}
                                            </option>
                                        ))}
                                        <option value={NEW_TAB}>New tab…</option>
                                    </Select>
                                </Field>
                                {tab === NEW_TAB && (
                                    <Field id="gs-newtab" label="New tab name">
                                        <TextInput id="gs-newtab" value={newTab} maxLength={100} onChange={(e) => setNewTab(e.target.value)} />
                                    </Field>
                                )}
                            </div>
                        )}
                        {tabs.error && <Problem text={tabs.error.message} />}
                        <p className="-mt-3 text-xs text-slate-500 dark:text-slate-400">
                            Use an empty tab. The CRM will not write to a tab that already has other data in it.
                        </p>

                        <div>
                            <p className="mb-2 text-xs font-medium text-slate-600 dark:text-slate-300">Columns, in order</p>
                            <ol className="space-y-1">
                                {columns.map((key, i) => (
                                    <li key={key} className="flex items-center gap-2 rounded-lg border border-slate-200 px-2.5 py-1.5 text-sm dark:border-slate-700">
                                        <span className="w-5 text-xs text-slate-400">{i + 1}</span>
                                        <span className="flex-1 text-slate-800 dark:text-slate-200">{label(key)}</span>
                                        {key === "lead_id" ? (
                                            <span className="text-xs text-slate-400">always first: matches rows</span>
                                        ) : (
                                            <>
                                                <IconButton label={`Move ${label(key)} up`} disabled={i <= 1} onClick={() => move(i, -1)}>
                                                    <ArrowUp className="size-3.5" />
                                                </IconButton>
                                                <IconButton label={`Move ${label(key)} down`} disabled={i === columns.length - 1} onClick={() => move(i, 1)}>
                                                    <ArrowDown className="size-3.5" />
                                                </IconButton>
                                                <IconButton label={`Remove ${label(key)}`} onClick={() => setColumns((c) => c.filter((k) => k !== key))}>
                                                    ×
                                                </IconButton>
                                            </>
                                        )}
                                    </li>
                                ))}
                            </ol>
                            {unused.length > 0 && (
                                <div className="mt-2 flex flex-wrap gap-1.5">
                                    {unused.map((f) => (
                                        <button
                                            key={f.key}
                                            type="button"
                                            onClick={() => setColumns((c) => [...c, f.key])}
                                            className="rounded-full border border-dashed border-slate-300 px-2.5 py-0.5 text-xs text-slate-600 hover:border-brand-400 dark:border-slate-600 dark:text-slate-300"
                                        >
                                            + {f.label}
                                        </button>
                                    ))}
                                </div>
                            )}
                            {errors.columns && <p className="mt-1 text-xs text-rose-600">{errors.columns}</p>}
                            <p className="mt-1 text-xs text-slate-500 dark:text-slate-400">
                                Notes are left out unless you add them: they can hold personal details.
                            </p>
                        </div>

                        <div>
                            <p className="mb-2 text-xs font-medium text-slate-600 dark:text-slate-300">Which leads</p>
                            <div className="flex flex-wrap gap-1.5">
                                {(Object.keys(STAGE_STYLES) as Stage[]).map((st) => (
                                    <label
                                        key={st}
                                        className={cx(
                                            "flex cursor-pointer items-center gap-1.5 rounded-full border px-2.5 py-1 text-xs",
                                            stages.includes(st)
                                                ? "border-brand-300 bg-brand-50 text-brand-800 dark:border-brand-700 dark:bg-brand-950 dark:text-brand-200"
                                                : "border-slate-200 text-slate-600 dark:border-slate-700 dark:text-slate-300",
                                        )}
                                    >
                                        <input
                                            type="checkbox"
                                            className="size-3 accent-brand-600"
                                            checked={stages.includes(st)}
                                            onChange={() => setStages((s) => (s.includes(st) ? s.filter((x) => x !== st) : [...s, st]))}
                                        />
                                        {STAGE_STYLES[st].label}
                                    </label>
                                ))}
                            </div>
                            <p className="mt-1 text-xs text-slate-500 dark:text-slate-400">
                                {stages.length === 0 ? "None ticked: every lead is exported." : "Only leads in the ticked stages are exported."}
                            </p>
                        </div>

                        <div className="flex flex-wrap items-center gap-3">
                            <label className="flex items-center gap-2 text-sm text-slate-700 dark:text-slate-200">
                                <input type="checkbox" className="size-4 accent-brand-600" checked={autoSync} onChange={(e) => setAutoSync(e.target.checked)} />
                                Sync new and changed leads automatically, every
                            </label>
                            <Select
                                aria-label="How often"
                                value={minutes}
                                onChange={(e) => setMinutes(Number(e.target.value))}
                                className="h-8 w-auto text-xs"
                                disabled={!autoSync}
                            >
                                {settings.auto_sync_options.map((m) => (
                                    <option key={m} value={m}>
                                        {m} minutes
                                    </option>
                                ))}
                            </Select>
                        </div>
                    </div>
                </ModalBody>
                <ModalFooter>
                    <Button variant="ghost" onClick={onClose}>
                        Cancel
                    </Button>
                    <Button type="submit" variant="primary" loading={saving} disabled={!sheetId}>
                        Save
                    </Button>
                </ModalFooter>
            </form>
        </Modal>
    );
}

function IconButton({
    label,
    disabled,
    onClick,
    children,
}: {
    label: string;
    disabled?: boolean;
    onClick: () => void;
    children: React.ReactNode;
}) {
    return (
        <button
            type="button"
            aria-label={label}
            disabled={disabled}
            onClick={onClick}
            className="flex size-6 items-center justify-center rounded text-slate-500 hover:bg-slate-100 disabled:opacity-30 dark:hover:bg-slate-800"
        >
            {children}
        </button>
    );
}

// ---------------------------------------------------------------------------
// Google's file picker
// ---------------------------------------------------------------------------

/* Google's picker script has no types here; just the parts used. */
// eslint-disable-next-line @typescript-eslint/no-explicit-any
type Loose = any;

let pickerScript: Promise<void> | null = null;

function loadPicker(): Promise<void> {
    pickerScript ??= new Promise((resolve, reject) => {
        const script = document.createElement("script");
        script.src = "https://apis.google.com/js/api.js";
        script.async = true;
        script.onload = () => (window as Loose).gapi.load("picker", { callback: resolve, onerror: reject });
        script.onerror = () => {
            pickerScript = null;
            reject(new Error("Could not load Google's file picker. Check your connection."));
        };
        document.head.appendChild(script);
    });
    return pickerScript;
}

/**
 * Opens Google's picker for one spreadsheet. Picking it is what gives the
 * CRM access to it (drive.file). The access token comes from the server for
 * this and is kept only in memory while the picker is open.
 */
async function openPicker(): Promise<{ id: string; name: string } | null> {
    const [{ access_token, api_key, app_id }] = await Promise.all([api.sheetsPickerToken(), loadPicker()]);
    const google = (window as Loose).google;
    return new Promise((resolve) => {
        const picker = new google.picker.PickerBuilder()
            .addView(new google.picker.DocsView(google.picker.ViewId.SPREADSHEETS).setIncludeFolders(true))
            .setOAuthToken(access_token)
            .setDeveloperKey(api_key)
            .setAppId(app_id)
            .setCallback((data: Loose) => {
                if (data.action === google.picker.Action.PICKED) {
                    const doc = data.docs[0];
                    resolve({ id: doc.id, name: doc.name });
                } else if (data.action === google.picker.Action.CANCEL) {
                    resolve(null);
                }
            })
            .build();
        picker.setVisible(true);
    });
}
