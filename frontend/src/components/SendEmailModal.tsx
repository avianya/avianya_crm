import { useEffect, useRef, useState } from "react";
import { Link } from "react-router-dom";
import { CircleAlert, ExternalLink, Send } from "lucide-react";

import { ApiError, api } from "../lib/api";
import type { EmailTemplate, Lead, LeadEmailResult, RenderedEmail } from "../lib/types";
import { useAsync } from "../hooks/useAsync";
import { useDebouncedValue } from "../hooks/useDebouncedValue";
import { insertField } from "../lib/templates";
import { FieldChips } from "./EmailFields";
import { Modal, ModalBody, ModalFooter } from "./Modal";
import { Button, Field, Select, Spinner, TextInput, Textarea } from "./ui";

/**
 * Writes an email to a lead, from a template or from scratch, and either
 * sends it from the server or opens it in the person's own email app.
 *
 * The preview is filled in by the server for this lead, so it is exactly what
 * will be sent. Either way the email ends up on the timeline as an Email
 * activity, which counts as contact for the lead's health and score.
 */
export function SendEmailModal({
    open,
    lead,
    onClose,
    onDone,
}: {
    open: boolean;
    lead: Lead;
    onClose: () => void;
    onDone: (result: LeadEmailResult) => void;
}) {
    return (
        <Modal
            open={open}
            onClose={onClose}
            title={`Email ${lead.name}`}
            description={`To ${lead.email}. Fields like {{first_name}} fill in from this lead.`}
            size="lg"
        >
            {/* Remounted on each open, so a new email starts blank. */}
            {open && <EmailForm lead={lead} onCancel={onClose} onDone={onDone} />}
        </Modal>
    );
}

function EmailForm({
    lead,
    onCancel,
    onDone,
}: {
    lead: Lead;
    onCancel: () => void;
    onDone: (result: LeadEmailResult) => void;
}) {
    const templates = useAsync((signal) => api.listEmailTemplates(signal), []);
    const { data: meta } = useAsync((signal) => api.meta(signal), []);
    const settings = useAsync((signal) => api.reminderSettings(signal), []);
    const fields = meta?.template_fields ?? [];
    const canSend = settings.data?.email_configured ?? false;

    const [templateId, setTemplateId] = useState("");
    const [subject, setSubject] = useState("");
    const [body, setBody] = useState("");
    const [errors, setErrors] = useState<Record<string, string>>({});
    const [busy, setBusy] = useState<"send" | "log" | null>(null);
    // Set once the email has been handed to the person's email app, so the
    // next step offered is logging it.
    const [openedInApp, setOpenedInApp] = useState(false);

    const subjectRef = useRef<HTMLInputElement>(null);
    const bodyRef = useRef<HTMLTextAreaElement>(null);
    const [target, setTarget] = useState<"subject" | "body">("body");

    const chooseTemplate = (id: string) => {
        setTemplateId(id);
        const template = templates.data?.find((t: EmailTemplate) => String(t.id) === id);
        if (template) {
            setSubject(template.subject);
            setBody(template.body);
            setErrors({});
            setOpenedInApp(false);
        }
    };

    const insert = (key: string) => {
        if (target === "subject") setSubject((v) => insertField(subjectRef.current, v, key));
        else setBody((v) => insertField(bodyRef.current, v, key));
    };

    // --- the server's preview, a moment after typing stops -------------------
    const debouncedSubject = useDebouncedValue(subject, 400);
    const debouncedBody = useDebouncedValue(body, 400);
    const [preview, setPreview] = useState<RenderedEmail | null>(null);
    const [previewError, setPreviewError] = useState<Record<string, string>>({});
    const [previewing, setPreviewing] = useState(false);

    useEffect(() => {
        if (!debouncedSubject.trim() || !debouncedBody.trim()) {
            setPreview(null);
            setPreviewError({});
            setPreviewing(false);
            return;
        }
        const controller = new AbortController();
        setPreviewing(true);
        api.previewLeadEmail(lead.id, { subject: debouncedSubject, body: debouncedBody }, controller.signal)
            .then((rendered) => {
                setPreview(rendered);
                setPreviewError({});
            })
            .catch((cause) => {
                if (controller.signal.aborted) return;
                setPreview(null);
                setPreviewError(
                    cause instanceof ApiError && cause.details
                        ? cause.details
                        : { form: cause instanceof ApiError ? cause.message : "Could not fill in the preview." },
                );
            })
            .finally(() => {
                if (!controller.signal.aborted) setPreviewing(false);
            });
        return () => controller.abort();
    }, [lead.id, debouncedSubject, debouncedBody]);

    // The preview only counts once it has caught up with what is typed.
    const upToDate = preview !== null && debouncedSubject === subject && debouncedBody === body;
    const shownErrors = { ...previewError, ...errors };

    const submit = async (send: boolean) => {
        const next: Record<string, string> = {};
        if (!subject.trim()) next.subject = "Enter a subject.";
        if (!body.trim()) next.body = "Write the email.";
        setErrors(next);
        if (Object.keys(next).length > 0) return;

        setBusy(send ? "send" : "log");
        try {
            const result = await api.sendLeadEmail(lead.id, { subject, body, send });
            onDone(result);
        } catch (cause) {
            if (cause instanceof ApiError && cause.isValidationError && cause.details) {
                setErrors(cause.details);
            } else {
                setErrors({ form: cause instanceof ApiError ? cause.message : "Could not save the email." });
            }
        } finally {
            setBusy(null);
        }
    };

    /** Hands the filled-in email to the person's default email app. */
    const openInApp = () => {
        if (!preview) return;
        const params = new URLSearchParams({ subject: preview.subject, body: preview.body });
        // URLSearchParams writes spaces as "+", which some mail apps show
        // literally; %20 works everywhere.
        const query = params.toString().replace(/\+/g, "%20");
        window.location.href = `mailto:${encodeURIComponent(preview.to)}?${query}`;
        setOpenedInApp(true);
    };

    const list = templates.data ?? [];

    return (
        <>
            <ModalBody>
                <Field
                    id="email-template"
                    label="Template"
                    hint={list.length === 0 && !templates.loading ? undefined : "Choosing one replaces the subject and body below."}
                >
                    {list.length === 0 && !templates.loading ? (
                        <p className="text-xs text-slate-500 dark:text-slate-400">
                            No templates yet.{" "}
                            <Link to="/templates" className="font-medium text-brand-600 hover:underline dark:text-brand-400">
                                Write one
                            </Link>{" "}
                            or type the email below.
                        </p>
                    ) : (
                        <Select
                            id="email-template"
                            value={templateId}
                            disabled={templates.loading}
                            onChange={(event) => chooseTemplate(event.target.value)}
                        >
                            <option value="">{templates.loading ? "Loading templates…" : "Start from blank"}</option>
                            {list.map((template) => (
                                <option key={template.id} value={template.id}>
                                    {template.name}
                                </option>
                            ))}
                        </Select>
                    )}
                </Field>

                <FieldChips fields={fields} onInsert={insert} label={`Insert into the ${target}`} />

                <Field id="email-subject" label="Subject" required error={shownErrors.subject}>
                    <TextInput
                        id="email-subject"
                        ref={subjectRef}
                        value={subject}
                        maxLength={200}
                        invalid={Boolean(shownErrors.subject)}
                        onFocus={() => setTarget("subject")}
                        onChange={(event) => {
                            setSubject(event.target.value);
                            setOpenedInApp(false);
                        }}
                    />
                </Field>

                <Field id="email-body" label="Body" required error={shownErrors.body}>
                    <Textarea
                        id="email-body"
                        ref={bodyRef}
                        rows={8}
                        value={body}
                        maxLength={1750}
                        invalid={Boolean(shownErrors.body)}
                        onFocus={() => setTarget("body")}
                        onChange={(event) => {
                            setBody(event.target.value);
                            setOpenedInApp(false);
                        }}
                    />
                </Field>

                {/* --- what the lead will receive --- */}
                <div className="rounded-xl border border-slate-200 bg-slate-50 p-4 dark:border-slate-800 dark:bg-slate-950/40">
                    <p className="flex items-center gap-2 text-[11px] font-medium tracking-wide text-slate-500 uppercase dark:text-slate-400">
                        What {lead.name} will receive
                        {previewing && <Spinner className="size-3" />}
                    </p>
                    {preview ? (
                        <>
                            <p className="mt-2 text-sm font-medium text-slate-900 dark:text-slate-100">{preview.subject}</p>
                            <p className="mt-2 text-sm whitespace-pre-wrap text-slate-700 dark:text-slate-300">{preview.body}</p>
                            {preview.missing.length > 0 && (
                                <p className="mt-3 flex items-start gap-2 text-xs text-amber-700 dark:text-amber-300" role="status">
                                    <CircleAlert className="mt-0.5 size-3.5 shrink-0" aria-hidden="true" />
                                    <span>
                                        Left blank, as this lead has none:{" "}
                                        {preview.missing
                                            .map((key) => fields.find((f) => f.key === key)?.label ?? key)
                                            .join(", ")}
                                        . Edit the text, or fill it in on the lead first.
                                    </span>
                                </p>
                            )}
                        </>
                    ) : (
                        <p className="mt-2 text-sm text-slate-400 dark:text-slate-500">
                            Choose a template or write a subject and body to see the email filled in.
                        </p>
                    )}
                </div>

                {!settings.loading && !canSend && (
                    <p className="text-xs text-slate-500 dark:text-slate-400">
                        Email is not set up on the server, so it cannot send from here. Open it in your email app, send it
                        there, then log it.{" "}
                        <Link to="/team" className="font-medium text-brand-600 hover:underline dark:text-brand-400">
                            How to set up email
                        </Link>
                    </p>
                )}

                {shownErrors.form && (
                    <p className="text-sm text-rose-600 dark:text-rose-400" role="alert">
                        {shownErrors.form}
                    </p>
                )}

            </ModalBody>
            <ModalFooter>
                <Button onClick={onCancel}>Cancel</Button>
                {openedInApp ? (
                    <Button variant={canSend ? "secondary" : "primary"} loading={busy === "log"} disabled={busy !== null} onClick={() => submit(false)}>
                        Log as sent
                    </Button>
                ) : (
                    <Button
                        variant={canSend ? "secondary" : "primary"}
                        disabled={!upToDate || busy !== null}
                        onClick={openInApp}
                        title="Opens your email app with this email filled in"
                    >
                        <ExternalLink className="size-4" aria-hidden="true" />
                        Open in email app
                    </Button>
                )}
                {canSend && (
                    <Button
                        variant="primary"
                        loading={busy === "send"}
                        disabled={!upToDate || busy !== null}
                        onClick={() => submit(true)}
                    >
                        {busy !== "send" && <Send className="size-4" aria-hidden="true" />}
                        Send
                    </Button>
                )}
            </ModalFooter>
        </>
    );
}
