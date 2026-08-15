import { ChevronLeft, ChevronRight } from "lucide-react";

import { cx } from "../lib/display";
import type { PageMeta } from "../lib/types";

/**
 * Pagination controls for the leads table.
 *
 * Shows a windowed range of page numbers rather than every page, so the control
 * stays the same width at 3 pages and at 300.
 */
export function Pagination({
    pagination,
    onPageChange,
    disabled = false,
}: {
    pagination: PageMeta;
    onPageChange: (page: number) => void;
    disabled?: boolean;
}) {
    const { page, page_size, total_items, total_pages } = pagination;

    // Nothing to paginate.
    if (total_pages <= 1) {
        return (
            <div className="px-5 py-3 text-xs text-slate-500 dark:text-slate-400">
                {total_items} {total_items === 1 ? "lead" : "leads"}
            </div>
        );
    }

    const firstOnPage = (page - 1) * page_size + 1;
    const lastOnPage = Math.min(page * page_size, total_items);

    return (
        <div className="flex flex-col gap-3 px-5 py-3 sm:flex-row sm:items-center sm:justify-between">
            <p className="text-xs text-slate-500 dark:text-slate-400">
                Showing <span className="font-medium tabular">{firstOnPage}</span>–
                <span className="font-medium tabular">{lastOnPage}</span> of{" "}
                <span className="font-medium tabular">{total_items}</span> leads
            </p>

            <nav className="flex items-center gap-1" aria-label="Pagination">
                <PageButton
                    onClick={() => onPageChange(page - 1)}
                    disabled={disabled || page <= 1}
                    label="Previous page"
                >
                    <ChevronLeft className="size-4" aria-hidden="true" />
                </PageButton>

                {pageWindow(page, total_pages).map((entry, index) =>
                    entry === "gap" ? (
                        <span
                            key={`gap-${index}`}
                            className="px-1.5 text-xs text-slate-400 select-none"
                            aria-hidden="true"
                        >
                            …
                        </span>
                    ) : (
                        <PageButton
                            key={entry}
                            onClick={() => onPageChange(entry)}
                            disabled={disabled}
                            active={entry === page}
                            label={`Page ${entry}`}
                            // aria-current tells a screen reader which page it is
                            // on; colour alone would not.
                            current={entry === page}
                        >
                            <span className="tabular">{entry}</span>
                        </PageButton>
                    ),
                )}

                <PageButton
                    onClick={() => onPageChange(page + 1)}
                    disabled={disabled || page >= total_pages}
                    label="Next page"
                >
                    <ChevronRight className="size-4" aria-hidden="true" />
                </PageButton>
            </nav>
        </div>
    );
}

function PageButton({
    onClick,
    disabled,
    active,
    current,
    label,
    children,
}: {
    onClick: () => void;
    disabled?: boolean;
    active?: boolean;
    current?: boolean;
    label: string;
    children: React.ReactNode;
}) {
    return (
        <button
            type="button"
            onClick={onClick}
            disabled={disabled}
            aria-label={label}
            aria-current={current ? "page" : undefined}
            className={cx(
                "inline-flex size-8 items-center justify-center rounded-lg text-xs font-medium transition-colors",
                "disabled:pointer-events-none disabled:opacity-40",
                active
                    ? "bg-brand-600 text-white dark:bg-brand-500"
                    : "text-slate-600 hover:bg-slate-100 dark:text-slate-300 dark:hover:bg-slate-800",
            )}
        >
            {children}
        </button>
    );
}

/**
 * Builds the visible page numbers: always the first and last page, the current
 * page with a neighbour either side, and "gap" markers for what is skipped.
 *
 * e.g. page 7 of 20 -> [1, gap, 6, 7, 8, gap, 20]
 */
function pageWindow(current: number, total: number): (number | "gap")[] {
    // Few enough pages to show them all.
    if (total <= 7) {
        return Array.from({ length: total }, (_, index) => index + 1);
    }

    const pages = new Set<number>([1, total, current]);
    if (current - 1 > 1) pages.add(current - 1);
    if (current + 1 < total) pages.add(current + 1);

    const sorted = [...pages].sort((a, b) => a - b);

    const result: (number | "gap")[] = [];
    for (let i = 0; i < sorted.length; i++) {
        // A jump of more than one page means something was skipped.
        if (i > 0 && sorted[i] - sorted[i - 1] > 1) result.push("gap");
        result.push(sorted[i]);
    }

    return result;
}
