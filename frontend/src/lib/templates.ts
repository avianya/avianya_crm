/**
 * Helpers for writing emails with {{fields}}, shared by the template editor
 * and the send-email dialog.
 *
 * Filling fields in for real happens on the server (model.Render), so what is
 * previewed for a lead is exactly what it receives. fillWithExamples here is
 * only for the template editor, which has no lead to fill in from.
 */

import type { TemplateField } from "./types";

/** Matches {{ field }} the way the server does: anything in double braces. */
const FIELD_PATTERN = /\{\{\s*([^{}]*?)\s*\}\}/g;

/** Fills in each known field with its example, leaving unknown ones as written. */
export function fillWithExamples(text: string, fields: TemplateField[]): string {
    return text.replace(FIELD_PATTERN, (match, key: string) => {
        const field = fields.find((f) => f.key === key);
        return field ? field.example : match;
    });
}

/** The {{...}} in text that are not fields, once each: typos to flag while typing. */
export function unknownFields(text: string, fields: TemplateField[]): string[] {
    const unknown: string[] = [];
    for (const match of text.matchAll(FIELD_PATTERN)) {
        const key = match[1];
        if (!fields.some((f) => f.key === key) && !unknown.includes(key)) unknown.push(key);
    }
    return unknown;
}

/**
 * Inserts a field token at the caret of an input or textarea, replacing any
 * selection, and returns the new value. The caret is put after the token once
 * React has re-rendered.
 */
export function insertField(
    element: HTMLInputElement | HTMLTextAreaElement | null,
    value: string,
    key: string,
): string {
    const token = `{{${key}}}`;
    if (!element) return value + token;

    const start = element.selectionStart ?? value.length;
    const end = element.selectionEnd ?? value.length;
    const next = value.slice(0, start) + token + value.slice(end);

    const caret = start + token.length;
    requestAnimationFrame(() => {
        element.focus();
        element.setSelectionRange(caret, caret);
    });
    return next;
}
