import { useRef, useState } from "react";
import { CircleAlert, CircleCheck, Copy, Download, FileDown, FileUp } from "lucide-react";

import { ApiError, api, saveFile } from "../lib/api";
import { cx } from "../lib/display";
import type { ImportProblem, ImportResult } from "../lib/types";
import { Modal, ModalBody, ModalFooter } from "./Modal";
import { Button, Select, Spinner } from "./ui";

/** Mirrors model.MaxImportBytes, so a too-big file is refused before upload. */
const MAX_BYTES = 2 * 1024 * 1024;

/** How many problem rows are listed; the rest are counted. */
const MAX_LISTED = 100;

/** A byte order mark, so Excel opens a downloaded file as UTF-8. */
const BOM = String.fromCharCode(0xfeff);

/** A file to fill in, with every column the import reads. */
const SAMPLE_CSV =
    "Name,Company,Email,Phone,Source,Stage,Deal value,Owner email,Close reason,Close note,Notes\n" +
    "Asha Rao,Kestrel Labs,asha@kestrel.in,'+91 98765 43210,Website,New,250000,,,,Asked for a demo\n" +
    "Vikram Iyer,Iyer & Sons,vikram@iyer.example,,Referral,Qualified,\"1,20,000\",,,,\n";

/** A file chosen for import, and the dry run of it. */
interface Checked {
    fileName: string;
    csv: string;
    result: ImportResult;
}

type Step =
    | { kind: "pick" }
    | { kind: "checking"; fileName: string }
    | ({ kind: "preview"; rechecking: boolean } & Checked)
    | { kind: "importing"; fileName: string }
    | { kind: "done"; fileName: string; result: ImportResult };

/**
 * Import leads from a CSV file: pick a file, check what would happen (and
 * which column is which), then import. Nothing is saved before the last step.
 */
export function ImportLeadsModal({
    open,
    onClose,
    onImported,
}: {
    open: boolean;
    onClose: () => void;
    onImported: (count: number) => void;
}) {
    return (
        <Modal
            open={open}
            onClose={onClose}
            title="Import leads"
            description="From a CSV file, such as one saved from Excel or Google Sheets, or exported from here."
            size="lg"
        >
            {open && <ImportFlow onClose={onClose} onImported={onImported} />}
        </Modal>
    );
}

/** The field chosen for each column, as the API takes it. */
const columnsOf = (result: ImportResult) => result.headers.map((h) => h.field);

function ImportFlow({ onClose, onImported }: { onClose: () => void; onImported: (count: number) => void }) {
    const [step, setStep] = useState<Step>({ kind: "pick" });
    const [runAutomations, setRunAutomations] = useState(false);
    const [error, setError] = useState<string | null>(null);
    const inputRef = useRef<HTMLInputElement>(null);

    const check = async (file: File) => {
        setError(null);
        if (file.size > MAX_BYTES) {
            setError("The file is larger than 2 MB. Split it into smaller files.");
            return;
        }
        setStep({ kind: "checking", fileName: file.name });
        try {
            const csv = await file.text();
            const result = await api.importLeads(csv, { dryRun: true, runAutomations });
            setStep({ kind: "preview", rechecking: false, fileName: file.name, csv, result });
        } catch (cause) {
            setStep({ kind: "pick" });
            setError(explain(cause));
        }
    };

    /** Checks the file again with one column's field changed. */
    const remap = async (checked: Checked, column: number, field: string) => {
        setError(null);
        const columns = columnsOf(checked.result).map((f, i) => (i === column ? field : f));
        setStep({ kind: "preview", rechecking: true, ...checked });
        try {
            const result = await api.importLeads(checked.csv, { dryRun: true, runAutomations, columns });
            setStep({ kind: "preview", rechecking: false, ...checked, result });
        } catch (cause) {
            setStep({ kind: "preview", rechecking: false, ...checked });
            setError(explain(cause));
        }
    };

    const runImport = async (checked: Checked) => {
        setError(null);
        setStep({ kind: "importing", fileName: checked.fileName });
        try {
            // The columns exactly as previewed, whether guessed or chosen.
            const result = await api.importLeads(checked.csv, {
                dryRun: false,
                runAutomations,
                columns: columnsOf(checked.result),
            });
            setStep({ kind: "done", fileName: checked.fileName, result });
            if (result.imported > 0) onImported(result.imported);
        } catch (cause) {
            setStep({ kind: "preview", rechecking: false, ...checked });
            setError(explain(cause));
        }
    };

    if (step.kind === "done") {
        const { result } = step;
        return (
            <>
                <ModalBody>
                    <p className="flex items-center gap-2 text-sm font-medium text-emerald-700 dark:text-emerald-300">
                        <CircleCheck className="size-4" aria-hidden="true" />
                        {result.imported === 1 ? "1 lead imported." : `${result.imported} leads imported.`}
                    </p>
                    <ProblemLists result={result} fileName={step.fileName} />
                </ModalBody>
                <ModalFooter>
                    <Button variant="primary" onClick={onClose}>
                        Done
                    </Button>
                </ModalFooter>
            </>
        );
    }

    if (step.kind === "preview") {
        const { result, rechecking } = step;
        const blocked = result.missing.length > 0;
        return (
            <>
                <ModalBody>
                    <p className="text-sm text-slate-600 dark:text-slate-300">
                        <span className="font-medium text-slate-900 dark:text-slate-100">{step.fileName}</span>:{" "}
                        {result.total_rows} {result.total_rows === 1 ? "row" : "rows"}. Nothing has been saved yet.
                    </p>

                    <ColumnPicker
                        result={result}
                        disabled={rechecking}
                        onChange={(column, field) => remap(step, column, field)}
                    />

                    {blocked ? (
                        <p
                            className="flex items-start gap-2 rounded-lg bg-amber-50 px-3 py-2 text-sm text-amber-800 dark:bg-amber-950/40 dark:text-amber-200"
                            role="status"
                        >
                            <CircleAlert className="mt-0.5 size-4 shrink-0" aria-hidden="true" />
                            Choose which column is the {result.missing.join(", ")} above. The rows are checked once
                            it is.
                        </p>
                    ) : (
                        <div className={cx("grid grid-cols-3 gap-2", rechecking && "opacity-50")}>
                            <Tile label="Ready to import" value={result.ready} tone="good" />
                            <Tile label="Duplicates, skipped" value={result.duplicates.length} tone="muted" />
                            <Tile label="With errors, skipped" value={result.errors.length} tone="bad" />
                        </div>
                    )}

                    {!blocked && <ProblemLists result={result} fileName={step.fileName} />}
                    <AutomationsCheckbox checked={runAutomations} onChange={setRunAutomations} />
                    {error && <ErrorLine message={error} />}
                </ModalBody>
                <ModalFooter>
                    <Button onClick={() => setStep({ kind: "pick" })}>Choose another file</Button>
                    <Button
                        variant="primary"
                        disabled={rechecking || blocked || result.ready === 0}
                        onClick={() => runImport(step)}
                    >
                        {result.ready === 1 ? "Import 1 lead" : `Import ${result.ready} leads`}
                    </Button>
                </ModalFooter>
            </>
        );
    }

    const busy = step.kind === "checking" || step.kind === "importing";
    return (
        <>
            <ModalBody>
                <button
                    type="button"
                    disabled={busy}
                    onClick={() => inputRef.current?.click()}
                    onDragOver={(event) => event.preventDefault()}
                    onDrop={(event) => {
                        event.preventDefault();
                        const file = event.dataTransfer.files[0];
                        if (file) void check(file);
                    }}
                    className="flex w-full flex-col items-center gap-2 rounded-xl border-2 border-dashed border-slate-300 px-4 py-8 text-center transition-colors hover:border-brand-400 hover:bg-brand-50/40 disabled:cursor-wait dark:border-slate-700 dark:hover:bg-brand-950/30"
                >
                    <FileUp className="size-6 text-slate-400" aria-hidden="true" />
                    <span className="text-sm font-medium text-slate-900 dark:text-slate-100">
                        {step.kind === "checking"
                            ? `Checking ${step.fileName}…`
                            : step.kind === "importing"
                              ? `Importing ${step.fileName}…`
                              : "Choose a CSV file, or drop it here"}
                    </span>
                    <span className="text-xs text-slate-500 dark:text-slate-400">
                        Up to 5,000 leads or 2 MB. You'll see what will happen before anything is saved.
                    </span>
                </button>
                <input
                    ref={inputRef}
                    type="file"
                    accept=".csv,text/csv"
                    className="hidden"
                    onChange={(event) => {
                        const file = event.target.files?.[0];
                        event.target.value = ""; // so picking the same file again still fires
                        if (file) void check(file);
                    }}
                />

                <div className="space-y-1 text-xs text-slate-600 dark:text-slate-300">
                    <p>
                        <span className="font-medium">Needed:</span> a name, company and email for each lead.{" "}
                        <span className="font-medium">Optional:</span> phone, source, stage, deal value, owner (a
                        team member's name or email), close reason, close note, notes. Columns are matched by their
                        headings, and you can change the match before importing.
                    </p>
                    <p className="text-slate-500 dark:text-slate-400">
                        Leads whose email is already in the CRM are skipped, not changed. Lost leads need a close
                        reason.
                    </p>
                </div>

                <Button
                    size="sm"
                    variant="ghost"
                    onClick={() => saveFile(new Blob([BOM + SAMPLE_CSV], { type: "text/csv" }), "leads-sample.csv")}
                >
                    <Download className="size-3.5" aria-hidden="true" />
                    Download a sample file
                </Button>

                <AutomationsCheckbox checked={runAutomations} onChange={setRunAutomations} />
                {error && <ErrorLine message={error} />}
            </ModalBody>
            <ModalFooter>
                <Button onClick={onClose}>Cancel</Button>
            </ModalFooter>
        </>
    );
}

/**
 * Which field each column of the file goes into. Open by default while a
 * required field has no column, or when some column is not imported, since
 * that is when a person most likely needs to look.
 */
function ColumnPicker({
    result,
    disabled,
    onChange,
}: {
    result: ImportResult;
    disabled: boolean;
    onChange: (column: number, field: string) => void;
}) {
    const used = new Set(result.headers.map((h) => h.field).filter(Boolean));
    const matched = result.headers.filter((h) => h.field).length;
    const startOpen = result.missing.length > 0 || matched < result.headers.length;

    return (
        <details open={startOpen} className="group rounded-lg border border-slate-200 dark:border-slate-800">
            <summary className="flex cursor-pointer items-center justify-between gap-2 px-3 py-2 text-xs font-medium text-slate-700 select-none dark:text-slate-300">
                <span>
                    Columns: {matched} of {result.headers.length} imported
                </span>
                <span className="flex items-center gap-2 text-brand-700 dark:text-brand-300">
                    {disabled && <Spinner className="size-3.5" />}
                    <span className="group-open:hidden">Change</span>
                    <span className="hidden group-open:inline">Hide</span>
                </span>
            </summary>
            <ul className="max-h-64 divide-y divide-slate-100 overflow-y-auto border-t border-slate-200 dark:divide-slate-800 dark:border-slate-800">
                {result.headers.map((header, i) => {
                    const sample = result.sample[i]?.trim();
                    return (
                        <li key={i} className="grid grid-cols-[minmax(0,1fr)_11rem] items-center gap-3 px-3 py-2">
                            <div className="min-w-0">
                                <p className="truncate text-sm text-slate-900 dark:text-slate-100">
                                    {header.name || <span className="text-slate-400">(no heading)</span>}
                                </p>
                                <p className="truncate text-xs text-slate-500 dark:text-slate-400">
                                    {sample ? `e.g. ${sample}` : "Empty in the first row"}
                                </p>
                            </div>
                            <Select
                                aria-label={`Field for the column "${header.name}"`}
                                value={header.field}
                                disabled={disabled}
                                onChange={(event) => onChange(i, event.target.value)}
                                className="h-9 text-xs"
                            >
                                <option value="">Don't import</option>
                                {result.fields.map((field) => (
                                    <option
                                        key={field.key}
                                        value={field.key}
                                        // One column per field; the one this column has stays choosable.
                                        disabled={used.has(field.key) && field.key !== header.field}
                                    >
                                        {field.label}
                                        {field.required ? " *" : ""}
                                    </option>
                                ))}
                            </Select>
                        </li>
                    );
                })}
            </ul>
        </details>
    );
}

function explain(cause: unknown): string {
    if (cause instanceof ApiError) {
        return cause.details?.file ? `This file can't be imported: ${cause.details.file}.` : cause.message;
    }
    return "The file could not be read.";
}

function AutomationsCheckbox({ checked, onChange }: { checked: boolean; onChange: (checked: boolean) => void }) {
    return (
        <label className="flex items-start gap-2.5 text-sm text-slate-700 dark:text-slate-300">
            <input
                type="checkbox"
                checked={checked}
                onChange={(event) => onChange(event.target.checked)}
                className="mt-0.5 size-4 rounded border-slate-300 text-brand-600 dark:border-slate-600"
            />
            <span>
                Also run automation rules on these leads
                <span className="block text-xs text-slate-500 dark:text-slate-400">
                    Off: no tasks are planned for them. On: each one is treated like a new lead added by hand.
                </span>
            </span>
        </label>
    );
}

function ErrorLine({ message }: { message: string }) {
    return (
        <p className="flex items-start gap-2 text-sm text-rose-600 dark:text-rose-400" role="alert">
            <CircleAlert className="mt-0.5 size-4 shrink-0" aria-hidden="true" />
            {message}
        </p>
    );
}

function Tile({ label, value, tone }: { label: string; value: number; tone: "good" | "muted" | "bad" }) {
    return (
        <div
            className={cx(
                "rounded-lg border px-3 py-2",
                tone === "good" && "border-emerald-200 bg-emerald-50 dark:border-emerald-900 dark:bg-emerald-950/40",
                tone === "muted" && "border-slate-200 bg-slate-50 dark:border-slate-800 dark:bg-slate-900",
                tone === "bad" && "border-rose-200 bg-rose-50 dark:border-rose-900 dark:bg-rose-950/40",
            )}
        >
            <p className="text-lg font-semibold text-slate-900 tabular-nums dark:text-slate-100">{value}</p>
            <p className="text-[11px] text-slate-600 dark:text-slate-400">{label}</p>
        </div>
    );
}

// ---------------------------------------------------------------------------
// Problem rows
// ---------------------------------------------------------------------------

/** One CSV cell, quoted when it has to be. */
function csvCell(value: string): string {
    return /[",\r\n]/.test(value) ? `"${value.replace(/"/g, '""')}"` : value;
}

/**
 * The rows with errors as a CSV to fix and import again: the file's own
 * columns, as written, plus an Error column saying what to fix.
 */
function errorRowsCSV(result: ImportResult, problems: ImportProblem[]): string {
    const width = result.headers.length;
    const lines = [[...result.headers.map((h) => h.name), "Error"]];
    for (const p of problems) {
        const cells = Array.from({ length: width }, (_, i) => p.cells[i] ?? "");
        lines.push([...cells, p.messages.join("; ")]);
    }
    return BOM + lines.map((line) => line.map(csvCell).join(",")).join("\r\n") + "\r\n";
}

/** "leads.csv" becomes "leads-errors.csv". */
function errorFileName(fileName: string): string {
    const base = fileName.replace(/\.csv$/i, "") || "import";
    return `${base}-errors.csv`;
}

function ProblemLists({ result, fileName }: { result: ImportResult; fileName: string }) {
    return (
        <>
            <ProblemList
                title="Errors"
                problems={result.errors}
                onDownload={() =>
                    saveFile(
                        new Blob([errorRowsCSV(result, result.errors)], { type: "text/csv" }),
                        errorFileName(fileName),
                    )
                }
            />
            <ProblemList title="Duplicates" problems={result.duplicates} />
        </>
    );
}

function ProblemList({
    title,
    problems,
    onDownload,
}: {
    title: string;
    problems: ImportProblem[];
    /** Offered for rows worth fixing and importing again. */
    onDownload?: () => void;
}) {
    if (problems.length === 0) return null;

    // Plain text of every row, to paste into a note or a spreadsheet.
    const copy = () => {
        const text = problems
            .map((p) => `Row ${p.line}\t${p.name}\t${p.email}\t${p.messages.join("; ")}`)
            .join("\n");
        void navigator.clipboard?.writeText(text);
    };

    return (
        <div>
            <div className="mb-1 flex flex-wrap items-center justify-between gap-1">
                <p className="text-xs font-semibold text-slate-700 dark:text-slate-300">
                    {title} ({problems.length})
                </p>
                <div className="flex gap-1">
                    {onDownload && (
                        <Button
                            size="sm"
                            variant="ghost"
                            onClick={onDownload}
                            title="A CSV of these rows with an Error column. Fix them and import that file."
                        >
                            <FileDown className="size-3.5" aria-hidden="true" />
                            Download these rows
                        </Button>
                    )}
                    <Button size="sm" variant="ghost" onClick={copy}>
                        <Copy className="size-3.5" aria-hidden="true" />
                        Copy list
                    </Button>
                </div>
            </div>
            <ul className="max-h-48 divide-y divide-slate-100 overflow-y-auto rounded-lg border border-slate-200 text-xs dark:divide-slate-800 dark:border-slate-800">
                {problems.slice(0, MAX_LISTED).map((p) => (
                    <li key={`${p.line}-${p.email}`} className="px-3 py-2">
                        <p className="font-medium text-slate-900 dark:text-slate-100">
                            Row {p.line}
                            {(p.name || p.email) && (
                                <span className="font-normal text-slate-500 dark:text-slate-400">
                                    {" "}
                                    · {[p.name, p.email].filter(Boolean).join(" · ")}
                                </span>
                            )}
                        </p>
                        {p.messages.map((m) => (
                            <p key={m} className="text-slate-600 dark:text-slate-300">
                                {m}
                            </p>
                        ))}
                    </li>
                ))}
                {problems.length > MAX_LISTED && (
                    <li className="px-3 py-2 text-slate-500 dark:text-slate-400">
                        …and {problems.length - MAX_LISTED} more. Download or copy the list to see them all.
                    </li>
                )}
            </ul>
        </div>
    );
}
