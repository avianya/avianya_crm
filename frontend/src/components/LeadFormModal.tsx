import { useEffect, useState } from "react";

import { ApiError } from "../lib/api";
import { STAGE_ORDER, STAGE_STYLES, SOURCE_STYLES } from "../lib/display";
import type { Lead, LeadInput, Source, Stage } from "../lib/types";
import { Modal } from "./Modal";
import { Button, Field, Select, TextInput, Textarea } from "./ui";

/** An empty form, used when creating. */
const BLANK_FORM: LeadInput = {
    name: "",
    company: "",
    email: "",
    phone: "",
    source: "website",
    stage: "new",
    notes: "",
};

/** Turns an existing lead into form values. */
function toFormValues(lead: Lead): LeadInput {
    return {
        name: lead.name,
        company: lead.company,
        email: lead.email,
        // The API models "no phone" as null; an <input> needs a string.
        phone: lead.phone ?? "",
        source: lead.source,
        stage: lead.stage,
        notes: lead.notes,
    };
}

/**
 * Client-side validation, mirroring model.LeadInput.Validate on the backend.
 *
 * Duplicating the rules is deliberate, and the duplication runs one way: this
 * exists to give instant feedback, while the server stays the authority. The
 * server's per-field errors are merged in on submit, so a rule that only exists
 * there (the unique email) still lands on the right input.
 */
function validate(values: LeadInput): Record<string, string> {
    const errors: Record<string, string> = {};

    if (!values.name.trim()) errors.name = "Full name is required.";
    else if (values.name.trim().length > 120) errors.name = "Must be 120 characters or fewer.";

    if (!values.company.trim()) errors.company = "Company is required.";
    else if (values.company.trim().length > 120)
        errors.company = "Must be 120 characters or fewer.";

    const email = values.email.trim();
    if (!email) errors.email = "Email is required.";
    else if (!/^[^@\s]+@[^@\s]+\.[^@\s]+$/.test(email))
        errors.email = "Enter a valid email address.";

    const phone = values.phone.trim();
    if (phone && !/^[0-9+()./\s-]{6,24}$/.test(phone))
        errors.phone = "Use 6-24 characters: digits and + ( ) - . / space.";

    if (values.notes.length > 5000) errors.notes = "Must be 5000 characters or fewer.";

    return errors;
}

/**
 * The create/edit form.
 *
 * One component for both because the fields, the rules and the layout are
 * identical -- only the title, the button label and which API call runs differ.
 * Two near-identical forms is how the two drift apart.
 */
export function LeadFormModal({
    open,
    onClose,
    onSubmit,
    lead,
}: {
    open: boolean;
    onClose: () => void;
    /** Resolves on success; rejecting leaves the dialog open with errors shown. */
    onSubmit: (input: LeadInput) => Promise<void>;
    /** The lead being edited, or undefined when creating a new one. */
    lead?: Lead;
}) {
    const isEditing = lead !== undefined;

    const [values, setValues] = useState<LeadInput>(BLANK_FORM);
    const [errors, setErrors] = useState<Record<string, string>>({});
    const [formError, setFormError] = useState<string | null>(null);
    const [saving, setSaving] = useState(false);

    // Reset whenever the dialog opens, so a cancelled edit does not leak its
    // values into the next lead you open.
    useEffect(() => {
        if (!open) return;
        setValues(lead ? toFormValues(lead) : BLANK_FORM);
        setErrors({});
        setFormError(null);
    }, [open, lead]);

    /** Updates one field and clears any error on it as the user types. */
    const setField = <K extends keyof LeadInput>(key: K, value: LeadInput[K]) => {
        setValues((current) => ({ ...current, [key]: value }));
        setErrors((current) => {
            if (!current[key]) return current;
            const next = { ...current };
            delete next[key];
            return next;
        });
    };

    const handleSubmit = async (event: React.FormEvent) => {
        event.preventDefault();

        const found = validate(values);
        if (Object.keys(found).length > 0) {
            setErrors(found);
            return;
        }

        setSaving(true);
        setFormError(null);

        try {
            await onSubmit({
                ...values,
                name: values.name.trim(),
                company: values.company.trim(),
                email: values.email.trim(),
                phone: values.phone.trim(),
                notes: values.notes.trim(),
            });
            onClose();
        } catch (cause) {
            if (cause instanceof ApiError) {
                // 422: the server sent per-field messages, so show them on the
                // fields themselves rather than as one opaque banner.
                if (cause.isValidationError && cause.details) {
                    setErrors(cause.details);
                    setFormError("Please fix the highlighted fields.");
                } else if (cause.isConflict) {
                    // The unique-email rule only exists in the database, so this
                    // is the one error the client cannot predict.
                    setErrors({ email: "Another lead already uses this email address." });
                    setFormError(null);
                } else {
                    setFormError(cause.message);
                }
            } else {
                setFormError("Something went wrong. Please try again.");
            }
        } finally {
            setSaving(false);
        }
    };

    return (
        <Modal
            open={open}
            onClose={onClose}
            size="lg"
            title={isEditing ? "Edit lead" : "New lead"}
            description={
                isEditing
                    ? `Update the details for ${lead.name}.`
                    : "Add a lead to the pipeline."
            }
        >
            {/* noValidate turns off the browser's own bubbles so the app's
                consistent inline messages are the only ones shown. */}
            <form onSubmit={handleSubmit} noValidate>
                <div className="space-y-4 px-5 py-5">
                    <div className="grid gap-4 sm:grid-cols-2">
                        <Field id="lead-name" label="Full name" required error={errors.name}>
                            <TextInput
                                id="lead-name"
                                value={values.name}
                                invalid={Boolean(errors.name)}
                                maxLength={120}
                                autoComplete="name"
                                placeholder="Ananya Sharma"
                                onChange={(event) => setField("name", event.target.value)}
                            />
                        </Field>

                        <Field
                            id="lead-company"
                            label="Company"
                            required
                            error={errors.company}
                        >
                            <TextInput
                                id="lead-company"
                                value={values.company}
                                invalid={Boolean(errors.company)}
                                maxLength={120}
                                autoComplete="organization"
                                placeholder="Vertex Analytics"
                                onChange={(event) => setField("company", event.target.value)}
                            />
                        </Field>

                        <Field
                            id="lead-email"
                            label="Email"
                            required
                            error={errors.email}
                            hint="Must be unique across all leads."
                        >
                            <TextInput
                                id="lead-email"
                                type="email"
                                value={values.email}
                                invalid={Boolean(errors.email)}
                                maxLength={254}
                                autoComplete="email"
                                placeholder="ananya@vertexanalytics.io"
                                onChange={(event) => setField("email", event.target.value)}
                            />
                        </Field>

                        <Field
                            id="lead-phone"
                            label="Phone"
                            error={errors.phone}
                            hint="Optional."
                        >
                            <TextInput
                                id="lead-phone"
                                type="tel"
                                value={values.phone}
                                invalid={Boolean(errors.phone)}
                                maxLength={24}
                                autoComplete="tel"
                                placeholder="+91 98200 11223"
                                onChange={(event) => setField("phone", event.target.value)}
                            />
                        </Field>

                        <Field id="lead-source" label="Lead source" error={errors.source}>
                            <Select
                                id="lead-source"
                                value={values.source}
                                invalid={Boolean(errors.source)}
                                onChange={(event) =>
                                    setField("source", event.target.value as Source)
                                }
                            >
                                {/* Object.entries over the style map keeps the
                                    dropdown in step with the palette: a new source
                                    appears here the moment it is given a style. */}
                                {Object.entries(SOURCE_STYLES).map(([value, style]) => (
                                    <option key={value} value={value}>
                                        {style.label}
                                    </option>
                                ))}
                            </Select>
                        </Field>

                        <Field id="lead-stage" label="Stage" error={errors.stage}>
                            <Select
                                id="lead-stage"
                                value={values.stage}
                                invalid={Boolean(errors.stage)}
                                onChange={(event) =>
                                    setField("stage", event.target.value as Stage)
                                }
                            >
                                {STAGE_ORDER.map((stage) => (
                                    <option key={stage} value={stage}>
                                        {STAGE_STYLES[stage].label}
                                    </option>
                                ))}
                            </Select>
                        </Field>
                    </div>

                    <Field
                        id="lead-notes"
                        label="Notes"
                        error={errors.notes}
                        hint="Context that does not belong on the timeline - budget, org chart, quirks."
                    >
                        <Textarea
                            id="lead-notes"
                            rows={3}
                            value={values.notes}
                            invalid={Boolean(errors.notes)}
                            maxLength={5000}
                            placeholder="Wants SSO before they will sign. Champion is the ops lead."
                            onChange={(event) => setField("notes", event.target.value)}
                        />
                    </Field>

                    {formError && (
                        <p
                            className="rounded-lg bg-rose-50 px-3 py-2 text-sm text-rose-700 dark:bg-rose-950/50 dark:text-rose-300"
                            role="alert"
                        >
                            {formError}
                        </p>
                    )}
                </div>

                <div className="flex justify-end gap-2 border-t border-slate-200 bg-slate-50 px-5 py-3.5 dark:border-slate-800 dark:bg-slate-950/50">
                    <Button variant="secondary" onClick={onClose} disabled={saving}>
                        Cancel
                    </Button>
                    <Button type="submit" variant="primary" loading={saving}>
                        {isEditing ? "Save changes" : "Create lead"}
                    </Button>
                </div>
            </form>
        </Modal>
    );
}
