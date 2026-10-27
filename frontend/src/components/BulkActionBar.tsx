import type { ReactNode } from "react";
import { Download, Trash2, X } from "lucide-react";

import { SOURCE_STYLES, STAGE_ORDER, STAGE_STYLES } from "../lib/display";
import type { Source, Stage, TeamMember } from "../lib/types";
import { Button, Spinner } from "./ui";

const SOURCES = Object.keys(SOURCE_STYLES) as Source[];

/** The value the "nobody" owner option carries; owner ids are positive. */
const NO_OWNER = "none";

/**
 * The bar shown while leads are ticked on the Leads list: what to do to all
 * of them at once. Each picker acts as soon as a value is chosen, then resets.
 */
export function BulkActionBar({
    count,
    team,
    busy,
    onOwner,
    onStage,
    onSource,
    onExport,
    onDelete,
    onClear,
}: {
    count: number;
    team: TeamMember[];
    busy: boolean;
    /** Left out for someone who may not reassign leads. */
    onOwner?: (ownerId: number | null) => void;
    onStage: (stage: Stage) => void;
    onSource: (source: Source) => void;
    onExport: () => void;
    /** Left out for someone who may not delete leads. */
    onDelete?: () => void;
    onClear: () => void;
}) {
    return (
        <div
            role="region"
            aria-label="Bulk actions"
            className="sticky bottom-4 z-20 mx-auto mt-4 flex w-full max-w-4xl flex-wrap items-center gap-2 rounded-xl border border-slate-200 bg-white px-3 py-2.5 shadow-lg dark:border-slate-700 dark:bg-slate-900"
        >
            <p className="flex items-center gap-2 pr-1 text-sm font-medium text-slate-900 dark:text-slate-100">
                {busy && <Spinner className="size-4" />}
                {count} selected
            </p>

            {onOwner && (
                <BarSelect
                    label="Assign to"
                    disabled={busy}
                    onPick={(value) => onOwner(value === NO_OWNER ? null : Number(value))}
                >
                    <option value={NO_OWNER}>Nobody (unassigned)</option>
                    {team.map((member) => (
                        <option key={member.id} value={member.id}>
                            {member.name}
                        </option>
                    ))}
                </BarSelect>
            )}

            <BarSelect label="Change stage" disabled={busy} onPick={(value) => onStage(value as Stage)}>
                {STAGE_ORDER.map((stage) => (
                    <option key={stage} value={stage}>
                        {STAGE_STYLES[stage].label}
                    </option>
                ))}
            </BarSelect>

            <BarSelect label="Change source" disabled={busy} onPick={(value) => onSource(value as Source)}>
                {SOURCES.map((source) => (
                    <option key={source} value={source}>
                        {SOURCE_STYLES[source].label}
                    </option>
                ))}
            </BarSelect>

            <Button size="sm" onClick={onExport} disabled={busy}>
                <Download className="size-3.5" aria-hidden="true" />
                Export
            </Button>
{onDelete && (
            <Button size="sm" variant="danger" onClick={onDelete} disabled={busy}>
                <Trash2 className="size-3.5" aria-hidden="true" />
                Delete
            </Button>
            )}

            <button
                type="button"
                onClick={onClear}
                disabled={busy}
                className="ml-auto inline-flex items-center gap-1 rounded-lg px-2 py-1 text-xs font-medium text-slate-500 hover:bg-slate-100 hover:text-slate-800 disabled:opacity-50 dark:text-slate-400 dark:hover:bg-slate-800 dark:hover:text-slate-200"
            >
                <X className="size-3.5" aria-hidden="true" />
                Clear selection
            </button>
        </div>
    );
}

/** A compact select whose first option is its label; picking anything else acts and resets it. */
function BarSelect({
    label,
    disabled,
    onPick,
    children,
}: {
    label: string;
    disabled: boolean;
    onPick: (value: string) => void;
    children: ReactNode;
}) {
    return (
        <select
            aria-label={label}
            value=""
            disabled={disabled}
            onChange={(event) => {
                if (event.target.value) onPick(event.target.value);
            }}
            className="select-chevron h-8 cursor-pointer appearance-none rounded-lg border border-slate-300 bg-white pr-8 pl-2.5 text-xs font-medium text-slate-700 shadow-sm hover:bg-slate-50 focus:border-brand-500 focus:ring-2 focus:ring-brand-500/20 focus:outline-none disabled:cursor-not-allowed disabled:opacity-50 dark:border-slate-700 dark:bg-slate-950 dark:text-slate-200 dark:hover:bg-slate-900"
        >
            <option value="" disabled>
                {label}…
            </option>
            {children}
        </select>
    );
}
