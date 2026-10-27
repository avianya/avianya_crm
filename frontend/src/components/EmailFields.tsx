import { Braces } from "lucide-react";

import type { TemplateField } from "../lib/types";

/**
 * A row of chips, one per {{field}}; clicking one inserts it where the caret
 * is (see insertField in lib/templates.ts). Used by the template editor and
 * the send-email dialog.
 */
export function FieldChips({
    fields,
    onInsert,
    label = "Insert a field",
}: {
    fields: TemplateField[];
    onInsert: (key: string) => void;
    label?: string;
}) {
    if (fields.length === 0) return null;

    return (
        <div className="flex flex-wrap items-center gap-1.5" role="group" aria-label={label}>
            <span className="inline-flex items-center gap-1 text-xs text-slate-500 dark:text-slate-400">
                <Braces className="size-3.5" aria-hidden="true" />
                {label}:
            </span>
            {fields.map((field) => (
                <button
                    key={field.key}
                    type="button"
                    // Cancelling mousedown keeps focus, and so the caret, in the
                    // field being typed in; the click then inserts the token.
                    onMouseDown={(event) => event.preventDefault()}
                    onClick={() => onInsert(field.key)}
                    title={`Inserts {{${field.key}}}, e.g. "${field.example}"`}
                    className="rounded-full bg-slate-100 px-2 py-0.5 font-mono text-[11px] text-slate-700 transition-colors hover:bg-brand-50 hover:text-brand-700 dark:bg-slate-800 dark:text-slate-300 dark:hover:bg-brand-950 dark:hover:text-brand-300"
                >
                    {field.label}
                </button>
            ))}
        </div>
    );
}
