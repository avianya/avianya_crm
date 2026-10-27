import { useEffect, useState } from "react";

import { ApiError, api } from "../lib/api";
import { MAX_CLOSE_NOTE, reasonsFor } from "../lib/closeReasons";
import { STAGE_ORDER, STAGE_STYLES, SOURCE_STYLES, formatMoney, isClosedStage } from "../lib/display";
import type { Lead, LeadInput, Source, Stage } from "../lib/types";
import { useAsync } from "../hooks/useAsync";
import { useTeam } from "../hooks/useTeam";
import { Modal } from "./Modal";
import { Button, Field, Select, TextInput, Textarea } from "./ui";
import { useAuth } from "../context/auth";

/**
 * What the form holds while someone is typing.
 *
 * Identical to LeadInput except the deal value, which is kept as the raw text
 * of the input: "" means "not estimated", and a half-typed value must not be
 * coerced to a number (or to NaN) before the user has finished. The close
 * reason ("" for none) only counts while the stage is Won or Lost.
 */
type FormValues = Omit<LeadInput, "deal_value" | "close_reason" | "close_note"> & {
    deal_value: string;
    close_reason: string;
    close_note: string;
};

/** An empty form, used when creating. */
const BLANK_FORM: FormValues = {
    name: "",
    company: "",
    email: "",
    phone: "",
    source: "website",
    stage: "new",
    notes: "",
    deal_value: "",
    owner_id: null,
    close_reason: "",
    close_note: "",
};

/** Turns an existing lead into form values. */
function toFormValues(lead: Lead): FormValues {
    return {
        name: lead.name,
        company: lead.company,
        email: lead.email,
        // The API models "no phone" as null; an <input> needs a string.
        phone: lead.phone ?? "",
        source: lead.source,
        stage: lead.stage,
        notes: lead.notes,
        deal_value: lead.deal_value === null ? "" : String(lead.deal_value),
        // PUT replaces every field, so the owner must travel with the edit or
        // saving the form would quietly unassign the lead.
        owner_id: lead.owner_id,
        close_reason: lead.close_reason ?? "",
        close_note: lead.close_note ?? "",
    };
}

/** Matches model.MaxDealValue on the backend. */
const MAX_DEAL_VALUE = 1_000_000_000_000;

/**
 * Reads the deal value text: digits only, with commas allowed because people
 * type "2,50,000". Returns null for blank and undefined for anything invalid.
 */
function parseDealValue(text: string): number | null | undefined {
    const cleaned = text.replace(/[,\s]/g, "");
    if (cleaned === "") return null;
    if (!/^\d+$/.test(cleaned)) return undefined;
    const value = Number(cleaned);
    return value <= MAX_DEAL_VALUE ? value : undefined;
}

/**
 * Client-side validation, mirroring model.LeadInput.Validate on the backend.
 *
 * Duplicating the rules is deliberate, and the duplication runs one way: this
 * exists to give instant feedback, while the server stays the authority. The
 * server's per-field errors are merged in on submit, so a rule that only exists
 * there (the unique email) still lands on the right input.
 */
function validate(values: FormValues, previousStage: Stage | undefined): Record<string, string> {
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

    if (parseDealValue(values.deal_value) === undefined)
        errors.deal_value = "Enter a whole number of rupees, e.g. 250000.";

    // Losing a deal needs a reason; editing one already lost does not.
    if (values.stage === "lost" && previousStage !== "lost" && !values.close_reason)
        errors.close_reason = "Say why the deal was lost.";
    if (isClosedStage(values.stage) && values.close_reason === "other" && !values.close_note.trim())
        errors.close_note = 'Add a note to explain "Other".';
    if (isClosedStage(values.stage) && values.close_note.trim() && !values.close_reason)
        errors.close_reason = "Choose a reason for the note.";

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
    const { is } = useAuth();
    // Only admins and managers choose who a lead is assigned to; the server
    // assigns a salesperson's leads to themselves whatever is sent.
    const canAssign = is("admin", "manager");
    const isEditing = lead !== undefined;

    const [values, setValues] = useState<FormValues>(BLANK_FORM);
    const [errors, setErrors] = useState<Record<string, string>>({});
    const [formError, setFormError] = useState<string | null>(null);
    const [saving, setSaving] = useState(false);
    const team = useTeam();
    const { data: meta } = useAsync((signal) => api.meta(signal), []);
    const reasons = reasonsFor(meta, values.stage);

    // Reset whenever the dialog opens, so a cancelled edit does not leak its
    // values into the next lead you open.
    useEffect(() => {
        if (!open) return;
        setValues(lead ? toFormValues(lead) : BLANK_FORM);
        setErrors({});
        setFormError(null);
    }, [open, lead]);

    /** Updates one field and clears any error on it as the user types. */
    const setField = <K extends keyof FormValues>(key: K, value: FormValues[K]) => {
        setValues((current) => ({ ...current, [key]: value }));
        setErrors((current) => {
            if (!current[key]) return current;
            const next = { ...current };
            delete next[key];
            return next;
        });
    };

    // Echo the typed number back formatted ("₹2,50,000"), so a missing or
    // extra zero is obvious before saving.
    const parsedDealValue = parseDealValue(values.deal_value);
    const dealValuePreview =
        typeof parsedDealValue === "number" ? `= ${formatMoney(parsedDealValue)}` : undefined;

    const handleSubmit = async (event: React.FormEvent) => {
        event.preventDefault();

        const found = validate(values, lead?.stage);
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
                // validate() has already rejected anything unparseable.
                deal_value: parseDealValue(values.deal_value) ?? null,
                // A reason only travels with Won or Lost; the server rejects one
                // on an open stage.
                close_reason: isClosedStage(values.stage) ? values.close_reason || null : null,
                close_note: isClosedStage(values.stage) ? values.close_note.trim() : "",
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
                                onChange={(event) => {
                                    setField("stage", event.target.value as Stage);
                                    // Won and Lost have different reasons, so a
                                    // reason never carries over between them.
                                    setField("close_reason", "");
                                    setField("close_note", "");
                                }}
                            >
                                {STAGE_ORDER.map((stage) => (
                                    <option key={stage} value={stage}>
                                        {STAGE_STYLES[stage].label}
                                    </option>
                                ))}
                            </Select>
                        </Field>

                        <Field
                            id="lead-deal-value"
                            label="Deal value (₹)"
                            error={errors.deal_value}
                            hint={
                                dealValuePreview ??
                                "Optional. Leave blank if it has not been estimated."
                            }
                        >
                            <TextInput
                                id="lead-deal-value"
                                inputMode="numeric"
                                value={values.deal_value}
                                invalid={Boolean(errors.deal_value)}
                                maxLength={20}
                                placeholder="250000"
                                onChange={(event) => setField("deal_value", event.target.value)}
                            />
                        </Field>

                        {canAssign ? (
                        <Field
                            id="lead-owner"
                            label="Assigned to"
                            error={errors.owner_id}
                            hint="Gets the reminders for this lead's events."
                        >
                            <Select
                                id="lead-owner"
                                value={values.owner_id ?? ""}
                                invalid={Boolean(errors.owner_id)}
                                disabled={team.loading}
                                onChange={(event) =>
                                    setField("owner_id", event.target.value ? Number(event.target.value) : null)
                                }
                            >
                                <option value="">{team.loading ? "Loading team…" : "Nobody"}</option>
                                {(team.data ?? []).map((member) => (
                                    <option key={member.id} value={member.id}>
                                        {member.name}
                                    </option>
                                ))}
                            </Select>
                        </Field>
                        ) : (
                            <Field id="lead-owner" label="Assigned to" hint="Leads you add are assigned to you.">
                                <TextInput id="lead-owner" value="You" disabled />
                            </Field>
                        )}
                    </div>

                    {isClosedStage(values.stage) && (
                        <div className="grid gap-4 rounded-xl border border-slate-200 p-4 sm:grid-cols-2 dark:border-slate-800">
                            <Field
                                id="lead-close-reason"
                                label={values.stage === "lost" ? "Why was it lost?" : "Why was it won?"}
                                required={values.stage === "lost" && lead?.stage !== "lost"}
                                error={errors.close_reason}
                                hint={values.stage === "won" ? "Optional." : "Feeds the loss report."}
                            >
                                <Select
                                    id="lead-close-reason"
                                    value={values.close_reason}
                                    invalid={Boolean(errors.close_reason)}
                                    onChange={(event) => setField("close_reason", event.target.value)}
                                >
                                    <option value="">
                                        {values.stage === "lost" ? "Choose a reason" : "No reason"}
                                    </option>
                                    {reasons.map((option) => (
                                        <option key={option.value} value={option.value}>
                                            {option.label}
                                        </option>
                                    ))}
                                </Select>
                            </Field>
                            <Field
                                id="lead-close-note"
                                label="Reason note"
                                error={errors.close_note}
                                hint={values.close_reason === "other" ? "Required for Other." : "Optional."}
                            >
                                <TextInput
                                    id="lead-close-note"
                                    value={values.close_note}
                                    invalid={Boolean(errors.close_note)}
                                    maxLength={MAX_CLOSE_NOTE}
                                    placeholder={
                                        values.stage === "lost" ? "Competitor offered 20% off" : "Loved the demo"
                                    }
                                    onChange={(event) => setField("close_note", event.target.value)}
                                />
                            </Field>
                        </div>
                    )}

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
