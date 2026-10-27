import { useRef, useState } from "react";
import type { FormEvent } from "react";
import { CircleAlert, FilePlus2, Mail, Pencil, Trash2 } from "lucide-react";

import { ApiError, api } from "../lib/api";
import { formatRelative } from "../lib/display";
import { fillWithExamples, insertField, unknownFields } from "../lib/templates";
import type { EmailTemplate, EmailTemplateInput, TemplateField } from "../lib/types";
import { useAsync } from "../hooks/useAsync";
import { useToast } from "../context/toast";
import { ConfirmDialog } from "../components/ConfirmDialog";
import { FieldChips } from "../components/EmailFields";
import { Modal, ModalBody, ModalFooter } from "../components/Modal";
import {
    Button,
    Card,
    CardHeader,
    EmptyState,
    ErrorState,
    Field,
    PageHeader,
    Skeleton,
    TextInput,
    Textarea,
} from "../components/ui";

/** Limits mirrored from model/templates.go, so the form can count down. */
const MAX_NAME = 120;
const MAX_SUBJECT = 200;
const MAX_BODY = 1750;

/**
 * Reusable emails. Each has {{fields}} that fill in from the lead it is sent
 * to; they are sent from a lead's page with "Email".
 */
export function TemplatesPage() {
    const toast = useToast();
    const templates = useAsync((signal) => api.listEmailTemplates(signal), []);
    const { data: meta } = useAsync((signal) => api.meta(signal), []);
    const fields = meta?.template_fields ?? [];

    const [editing, setEditing] = useState<EmailTemplate | "new" | null>(null);
    const [deleting, setDeleting] = useState<EmailTemplate | null>(null);

    const remove = async () => {
        if (!deleting) return;
        try {
            await api.deleteEmailTemplate(deleting.id);
            toast.success(`"${deleting.name}" was deleted. Emails already sent from it stay on their timelines.`);
            templates.refetch();
        } catch (cause) {
            toast.error(cause instanceof ApiError ? cause.message : "Could not delete the template.");
            throw cause; // keeps the dialog open
        }
    };

    const list = templates.data ?? [];

    return (
        <>
            <PageHeader
                title="Email templates"
                subtitle="Reusable emails. Fields like {{first_name}} fill in from the lead you send to."
                action={
                    <Button variant="primary" onClick={() => setEditing("new")}>
                        <FilePlus2 className="size-4" aria-hidden="true" />
                        New template
                    </Button>
                }
            />

            <Card>
                <CardHeader
                    title="Templates"
                    subtitle={
                        templates.data
                            ? `${list.length} ${list.length === 1 ? "template" : "templates"}. Send one from a lead's page.`
                            : undefined
                    }
                />
                {templates.loading ? (
                    <div className="space-y-3 px-5 py-4">
                        <Skeleton className="h-14 w-full" />
                        <Skeleton className="h-14 w-full" />
                    </div>
                ) : templates.error ? (
                    <div className="p-4">
                        <ErrorState message={templates.error.message} onRetry={templates.refetch} />
                    </div>
                ) : list.length === 0 ? (
                    <EmptyState
                        icon={Mail}
                        title="No templates yet"
                        description="Write the emails you send again and again once, and send them from any lead in a couple of clicks."
                        action={
                            <Button variant="primary" onClick={() => setEditing("new")}>
                                Write the first template
                            </Button>
                        }
                    />
                ) : (
                    <ul className="divide-y divide-slate-100 dark:divide-slate-800">
                        {list.map((template) => (
                            <li key={template.id} className="flex items-start gap-3 px-5 py-3.5">
                                <span className="mt-0.5 flex size-9 shrink-0 items-center justify-center rounded-full bg-brand-50 text-brand-700 dark:bg-brand-950 dark:text-brand-300">
                                    <Mail className="size-4" aria-hidden="true" />
                                </span>
                                <div className="min-w-0 flex-1">
                                    <p className="truncate text-sm font-medium text-slate-900 dark:text-slate-100">
                                        {template.name}
                                    </p>
                                    <p className="truncate text-xs text-slate-600 dark:text-slate-300">
                                        {template.subject}
                                    </p>
                                    <p className="mt-0.5 line-clamp-2 text-xs whitespace-pre-line text-slate-500 dark:text-slate-400">
                                        {template.body}
                                    </p>
                                    <p className="mt-1 text-[11px] text-slate-400 dark:text-slate-500">
                                        Updated {formatRelative(template.updated_at)}
                                    </p>
                                </div>
                                <div className="flex shrink-0 gap-1">
                                    <Button
                                        size="sm"
                                        variant="ghost"
                                        aria-label={`Edit ${template.name}`}
                                        onClick={() => setEditing(template)}
                                    >
                                        <Pencil className="size-3.5" aria-hidden="true" />
                                    </Button>
                                    <Button
                                        size="sm"
                                        variant="ghost"
                                        aria-label={`Delete ${template.name}`}
                                        onClick={() => setDeleting(template)}
                                    >
                                        <Trash2 className="size-3.5" aria-hidden="true" />
                                    </Button>
                                </div>
                            </li>
                        ))}
                    </ul>
                )}
            </Card>

            <Modal
                open={editing !== null}
                onClose={() => setEditing(null)}
                title={editing === "new" ? "New email template" : "Edit email template"}
                description="Click a field to insert it where the cursor is. It fills in from the lead when you send."
                size="lg"
            >
                {editing !== null && (
                    <TemplateForm
                        key={editing === "new" ? "new" : editing.id}
                        template={editing === "new" ? null : editing}
                        fields={fields}
                        onCancel={() => setEditing(null)}
                        onSaved={(saved, created) => {
                            setEditing(null);
                            toast.success(created ? `"${saved.name}" created.` : `"${saved.name}" saved.`);
                            templates.refetch();
                        }}
                    />
                )}
            </Modal>

            <ConfirmDialog
                open={deleting !== null}
                onClose={() => setDeleting(null)}
                onConfirm={remove}
                title="Delete template"
                confirmLabel="Delete"
                message={
                    deleting
                        ? `Delete "${deleting.name}"? Emails already sent from it stay on their leads' timelines.`
                        : ""
                }
            />
        </>
    );
}

function TemplateForm({
    template,
    fields,
    onCancel,
    onSaved,
}: {
    template: EmailTemplate | null;
    fields: TemplateField[];
    onCancel: () => void;
    onSaved: (template: EmailTemplate, created: boolean) => void;
}) {
    const [values, setValues] = useState<EmailTemplateInput>(() => ({
        name: template?.name ?? "",
        subject: template?.subject ?? "",
        body: template?.body ?? "",
    }));
    const [errors, setErrors] = useState<Record<string, string>>({});
    const [saving, setSaving] = useState(false);

    // The field chips insert into whichever of subject and body was used last.
    const subjectRef = useRef<HTMLInputElement>(null);
    const bodyRef = useRef<HTMLTextAreaElement>(null);
    const [target, setTarget] = useState<"subject" | "body">("body");

    const insert = (key: string) => {
        if (target === "subject") {
            setValues((v) => ({ ...v, subject: insertField(subjectRef.current, v.subject, key) }));
        } else {
            setValues((v) => ({ ...v, body: insertField(bodyRef.current, v.body, key) }));
        }
    };

    const typos = fields.length > 0 ? unknownFields(`${values.subject}\n${values.body}`, fields) : [];

    const submit = async (event: FormEvent) => {
        event.preventDefault();
        const next: Record<string, string> = {};
        if (!values.name.trim()) next.name = "Give the template a name.";
        if (!values.subject.trim()) next.subject = "Enter a subject.";
        if (!values.body.trim()) next.body = "Write the email.";
        setErrors(next);
        if (Object.keys(next).length > 0) return;

        setSaving(true);
        try {
            const saved = template
                ? await api.updateEmailTemplate(template.id, values)
                : await api.createEmailTemplate(values);
            onSaved(saved, template === null);
        } catch (cause) {
            if (cause instanceof ApiError && cause.isValidationError && cause.details) {
                setErrors(cause.details);
            } else {
                setErrors({ form: cause instanceof ApiError ? cause.message : "Could not save the template." });
            }
        } finally {
            setSaving(false);
        }
    };

    return (
        <form onSubmit={submit} noValidate>
            <ModalBody>
                <Field id="template-name" label="Name" required error={errors.name} hint="Only you see this, in the template picker.">
                    <TextInput
                        id="template-name"
                        value={values.name}
                        maxLength={MAX_NAME}
                        invalid={Boolean(errors.name)}
                        onChange={(event) => setValues({ ...values, name: event.target.value })}
                        placeholder="e.g. Follow-up after a demo"
                        autoFocus
                    />
                </Field>

                <FieldChips fields={fields} onInsert={insert} label={`Insert into the ${target}`} />

                <Field id="template-subject" label="Subject" required error={errors.subject}>
                    <TextInput
                        id="template-subject"
                        ref={subjectRef}
                        value={values.subject}
                        maxLength={MAX_SUBJECT}
                        invalid={Boolean(errors.subject)}
                        onFocus={() => setTarget("subject")}
                        onChange={(event) => setValues({ ...values, subject: event.target.value })}
                    />
                </Field>

                <Field
                    id="template-body"
                    label="Body"
                    required
                    error={errors.body}
                    hint={`${values.body.length} / ${MAX_BODY} characters`}
                >
                    <Textarea
                        id="template-body"
                        ref={bodyRef}
                        rows={9}
                        value={values.body}
                        maxLength={MAX_BODY}
                        invalid={Boolean(errors.body)}
                        onFocus={() => setTarget("body")}
                        onChange={(event) => setValues({ ...values, body: event.target.value })}
                    />
                </Field>

                {typos.length > 0 && (
                    <p className="flex items-start gap-2 text-xs text-amber-700 dark:text-amber-300" role="status">
                        <CircleAlert className="mt-0.5 size-3.5 shrink-0" aria-hidden="true" />
                        <span>
                            Not a field: {typos.map((t) => `{{${t}}}`).join(", ")}. Check the spelling, or use the
                            buttons above. The template cannot be saved with it.
                        </span>
                    </p>
                )}

                {(values.subject || values.body) && (
                    <div className="rounded-xl border border-slate-200 bg-slate-50 p-4 dark:border-slate-800 dark:bg-slate-950/40">
                        <p className="text-[11px] font-medium tracking-wide text-slate-500 uppercase dark:text-slate-400">
                            Preview with a sample lead
                        </p>
                        <p className="mt-2 text-sm font-medium text-slate-900 dark:text-slate-100">
                            {fillWithExamples(values.subject, fields) || "(no subject)"}
                        </p>
                        <p className="mt-2 text-sm whitespace-pre-wrap text-slate-700 dark:text-slate-300">
                            {fillWithExamples(values.body, fields)}
                        </p>
                    </div>
                )}

                {errors.form && (
                    <p className="text-sm text-rose-600 dark:text-rose-400" role="alert">
                        {errors.form}
                    </p>
                )}
            </ModalBody>

            <ModalFooter>
                <Button onClick={onCancel}>Cancel</Button>
                <Button type="submit" variant="primary" loading={saving}>
                    {template ? "Save changes" : "Create template"}
                </Button>
            </ModalFooter>
        </form>
    );
}
