import { useRef, useState } from "react";
import type { FormEvent } from "react";
import { CircleAlert, Pencil, Plus, Trash2, Zap } from "lucide-react";

import { ApiError, api } from "../lib/api";
import { SOURCE_STYLES, TASK_TYPES, TASK_TYPE_STYLES, cx, formatMoney, formatRelative } from "../lib/display";
import { insertField, unknownFields } from "../lib/templates";
import type { AutomationRule, AutomationRuleInput, Source, TeamMember, TemplateField } from "../lib/types";
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
    Select,
    Skeleton,
    TextInput,
} from "../components/ui";

/** Limits mirrored from model/automations.go. */
const MAX_NAME = 120;
const MAX_TITLE = 200;
const MAX_DUE_IN_DAYS = 365;

const SOURCES = Object.keys(SOURCE_STYLES) as Source[];

/** The parts of a rule the form edits and PUT sends back. */
function toInput(rule: AutomationRule): AutomationRuleInput {
    return {
        name: rule.name,
        enabled: rule.enabled,
        sources: rule.sources,
        min_deal_value: rule.min_deal_value,
        task_title: rule.task_title,
        task_type: rule.task_type,
        due_in_days: rule.due_in_days,
        assignee_id: rule.assignee_id,
    };
}

function dueText(days: number): string {
    if (days === 0) return "due the same day";
    if (days === 1) return "due the next day";
    return `due in ${days} days`;
}

/** The rule as one sentence: "When a new lead from Website comes in, plan a call …". */
function describeRule(rule: AutomationRule, team: TeamMember[]): string {
    const conditions: string[] = [];
    if (rule.sources.length > 0) {
        conditions.push(`from ${rule.sources.map((s) => SOURCE_STYLES[s].label).join(" or ")}`);
    }
    if (rule.min_deal_value !== null) {
        conditions.push(`worth at least ${formatMoney(rule.min_deal_value)}`);
    }
    const who =
        rule.assignee_id === null
            ? "the lead's owner"
            : (team.find((m) => m.id === rule.assignee_id)?.name ?? "a removed team member");
    const lead = ["a new lead", ...conditions].join(" ");
    return `When ${lead} comes in, plan a ${TASK_TYPE_STYLES[rule.task_type].label.toLowerCase()} ${dueText(rule.due_in_days)}, for ${who}.`;
}

/**
 * Automation rules: "when a new lead comes in, plan this task for it". They run
 * on the server as the lead is saved, from any page.
 */
export function AutomationsPage() {
    const toast = useToast();
    const rules = useAsync((signal) => api.listAutomationRules(signal), []);
    const { data: meta } = useAsync((signal) => api.meta(signal), []);
    const { data: teamData } = useAsync((signal) => api.listTeam(signal), []);
    const fields = meta?.template_fields ?? [];
    const team = teamData ?? [];

    const [editing, setEditing] = useState<AutomationRule | "new" | null>(null);
    const [deleting, setDeleting] = useState<AutomationRule | null>(null);
    const [toggling, setToggling] = useState<number | null>(null);

    const toggle = async (rule: AutomationRule) => {
        setToggling(rule.id);
        try {
            await api.updateAutomationRule(rule.id, { ...toInput(rule), enabled: !rule.enabled });
            toast.success(rule.enabled ? `"${rule.name}" switched off.` : `"${rule.name}" switched on.`);
            rules.refetch();
        } catch (cause) {
            toast.error(cause instanceof ApiError ? cause.message : "Could not change the rule.");
        } finally {
            setToggling(null);
        }
    };

    const remove = async () => {
        if (!deleting) return;
        try {
            await api.deleteAutomationRule(deleting.id);
            toast.success(`"${deleting.name}" was deleted. Tasks it already planned stay.`);
            rules.refetch();
        } catch (cause) {
            toast.error(cause instanceof ApiError ? cause.message : "Could not delete the rule.");
            throw cause; // keeps the dialog open
        }
    };

    const list = rules.data ?? [];
    const onCount = list.filter((r) => r.enabled).length;

    return (
        <>
            <PageHeader
                title="Automations"
                subtitle="Plan follow-ups for new leads automatically, so none is left waiting."
                action={
                    <Button variant="primary" onClick={() => setEditing("new")}>
                        <Plus className="size-4" aria-hidden="true" />
                        New rule
                    </Button>
                }
            />

            <RotationCard team={team} />

            <Card>
                <CardHeader
                    title="Rules"
                    subtitle={
                        rules.data
                            ? `${onCount} of ${list.length} switched on. They run when a lead is created, in this order.`
                            : undefined
                    }
                />
                {rules.loading ? (
                    <div className="space-y-3 px-5 py-4">
                        <Skeleton className="h-14 w-full" />
                        <Skeleton className="h-14 w-full" />
                    </div>
                ) : rules.error ? (
                    <div className="p-4">
                        <ErrorState message={rules.error.message} onRetry={rules.refetch} />
                    </div>
                ) : list.length === 0 ? (
                    <EmptyState
                        icon={Zap}
                        title="No rules yet"
                        description="For example: when a new lead comes in from the website, plan a call with them for the next day."
                        action={
                            <Button variant="primary" onClick={() => setEditing("new")}>
                                Create the first rule
                            </Button>
                        }
                    />
                ) : (
                    <ul className="divide-y divide-slate-100 dark:divide-slate-800">
                        {list.map((rule) => (
                            <li key={rule.id} className="flex items-start gap-3 px-5 py-3.5">
                                <span
                                    className={cx(
                                        "mt-0.5 flex size-9 shrink-0 items-center justify-center rounded-full",
                                        rule.enabled
                                            ? "bg-amber-100 text-amber-700 dark:bg-amber-950 dark:text-amber-300"
                                            : "bg-slate-100 text-slate-400 dark:bg-slate-800 dark:text-slate-500",
                                    )}
                                >
                                    <Zap className="size-4" aria-hidden="true" />
                                </span>
                                <div className="min-w-0 flex-1">
                                    <p className="truncate text-sm font-medium text-slate-900 dark:text-slate-100">
                                        {rule.name}
                                    </p>
                                    <p className="text-xs text-slate-600 dark:text-slate-300">
                                        {describeRule(rule, team)}
                                    </p>
                                    <p className="mt-0.5 truncate text-xs text-slate-500 dark:text-slate-400">
                                        Task: {rule.task_title}
                                    </p>
                                    <p className="mt-1 text-[11px] text-slate-400 dark:text-slate-500">
                                        {rule.run_count === 0
                                            ? "Not run yet"
                                            : `Ran for ${rule.run_count} ${rule.run_count === 1 ? "lead" : "leads"}, last ${formatRelative(rule.last_run_at!)}`}
                                    </p>
                                </div>
                                <div className="flex shrink-0 items-center gap-1">
                                    <button
                                        type="button"
                                        role="switch"
                                        aria-checked={rule.enabled}
                                        aria-label={`${rule.enabled ? "Switch off" : "Switch on"} ${rule.name}`}
                                        disabled={toggling === rule.id}
                                        onClick={() => toggle(rule)}
                                        className={cx(
                                            "relative mr-1 inline-flex h-5 w-9 shrink-0 cursor-pointer items-center rounded-full transition-colors disabled:opacity-50",
                                            rule.enabled ? "bg-brand-600" : "bg-slate-300 dark:bg-slate-700",
                                        )}
                                    >
                                        <span
                                            className={cx(
                                                "inline-block size-4 rounded-full bg-white shadow transition-transform",
                                                rule.enabled ? "translate-x-4.5" : "translate-x-0.5",
                                            )}
                                        />
                                    </button>
                                    <Button
                                        size="sm"
                                        variant="ghost"
                                        aria-label={`Edit ${rule.name}`}
                                        onClick={() => setEditing(rule)}
                                    >
                                        <Pencil className="size-3.5" aria-hidden="true" />
                                    </Button>
                                    <Button
                                        size="sm"
                                        variant="ghost"
                                        aria-label={`Delete ${rule.name}`}
                                        onClick={() => setDeleting(rule)}
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
                title={editing === "new" ? "New automation rule" : "Edit automation rule"}
                description="Runs for every new lead that matches. Leads created before the rule are left alone."
                size="lg"
            >
                {editing !== null && (
                    <RuleForm
                        key={editing === "new" ? "new" : editing.id}
                        rule={editing === "new" ? null : editing}
                        fields={fields}
                        team={team}
                        onCancel={() => setEditing(null)}
                        onSaved={(saved, created) => {
                            setEditing(null);
                            toast.success(created ? `"${saved.name}" created.` : `"${saved.name}" saved.`);
                            rules.refetch();
                        }}
                    />
                )}
            </Modal>

            <ConfirmDialog
                open={deleting !== null}
                onClose={() => setDeleting(null)}
                onConfirm={remove}
                title="Delete rule"
                confirmLabel="Delete"
                message={
                    deleting ? `Delete "${deleting.name}"? Tasks it already planned stay on their leads.` : ""
                }
            />
        </>
    );
}

function RuleForm({
    rule,
    fields,
    team,
    onCancel,
    onSaved,
}: {
    rule: AutomationRule | null;
    fields: TemplateField[];
    team: TeamMember[];
    onCancel: () => void;
    onSaved: (rule: AutomationRule, created: boolean) => void;
}) {
    const [values, setValues] = useState<AutomationRuleInput>(() =>
        rule
            ? toInput(rule)
            : {
                  name: "",
                  enabled: true,
                  sources: [],
                  min_deal_value: null,
                  task_title: "",
                  task_type: "call",
                  due_in_days: 1,
                  assignee_id: null,
              },
    );
    // Kept as typed, so the field can be cleared; turned into numbers on save.
    const [minValue, setMinValue] = useState(rule?.min_deal_value?.toString() ?? "");
    const [dueDays, setDueDays] = useState(String(rule?.due_in_days ?? 1));
    const [errors, setErrors] = useState<Record<string, string>>({});
    const [saving, setSaving] = useState(false);
    const titleRef = useRef<HTMLInputElement>(null);

    const typos = fields.length > 0 ? unknownFields(values.task_title, fields) : [];

    const toggleSource = (source: Source) =>
        setValues((v) => ({
            ...v,
            sources: v.sources.includes(source) ? v.sources.filter((s) => s !== source) : [...v.sources, source],
        }));

    const submit = async (event: FormEvent) => {
        event.preventDefault();
        const next: Record<string, string> = {};
        if (!values.name.trim()) next.name = "Give the rule a name.";
        if (!values.task_title.trim()) next.task_title = "Enter the task's title.";

        const min = minValue.trim() === "" ? null : Number(minValue);
        if (min !== null && (!Number.isInteger(min) || min < 0)) next.min_deal_value = "Enter whole rupees, or leave it empty.";
        const days = Number(dueDays);
        if (dueDays.trim() === "" || !Number.isInteger(days) || days < 0 || days > MAX_DUE_IN_DAYS) {
            next.due_in_days = `Enter 0 to ${MAX_DUE_IN_DAYS} days.`;
        }
        setErrors(next);
        if (Object.keys(next).length > 0) return;

        const input = { ...values, min_deal_value: min, due_in_days: days };
        setSaving(true);
        try {
            const saved = rule
                ? await api.updateAutomationRule(rule.id, input)
                : await api.createAutomationRule(input);
            onSaved(saved, rule === null);
        } catch (cause) {
            if (cause instanceof ApiError && cause.isValidationError && cause.details) {
                setErrors(cause.details);
            } else {
                setErrors({ form: cause instanceof ApiError ? cause.message : "Could not save the rule." });
            }
        } finally {
            setSaving(false);
        }
    };

    return (
        <form onSubmit={submit} noValidate>
            <ModalBody>
                <Field id="rule-name" label="Name" required error={errors.name}>
                    <TextInput
                        id="rule-name"
                        value={values.name}
                        maxLength={MAX_NAME}
                        invalid={Boolean(errors.name)}
                        onChange={(event) => setValues({ ...values, name: event.target.value })}
                        placeholder="e.g. Intro call for website leads"
                        autoFocus
                    />
                </Field>

                <section className="space-y-3 rounded-xl border border-slate-200 p-4 dark:border-slate-800">
                    <p className="text-xs font-semibold tracking-wide text-slate-500 uppercase dark:text-slate-400">
                        When a new lead comes in
                    </p>
                    <div className="space-y-1.5">
                        <p className="text-xs font-medium text-slate-700 dark:text-slate-300">From these sources</p>
                        <div className="flex flex-wrap gap-1.5">
                            {SOURCES.map((source) => {
                                const on = values.sources.includes(source);
                                return (
                                    <button
                                        key={source}
                                        type="button"
                                        aria-pressed={on}
                                        onClick={() => toggleSource(source)}
                                        className={cx(
                                            "rounded-full px-3 py-1 text-xs font-medium ring-1 transition-colors ring-inset",
                                            on
                                                ? "bg-brand-600 text-white ring-brand-600"
                                                : "bg-white text-slate-700 ring-slate-300 hover:bg-slate-50 dark:bg-slate-900 dark:text-slate-300 dark:ring-slate-700",
                                        )}
                                    >
                                        {SOURCE_STYLES[source].label}
                                    </button>
                                );
                            })}
                        </div>
                        <p className="text-xs text-slate-500 dark:text-slate-400">
                            {errors.sources ?? (values.sources.length === 0 ? "None picked: any source." : "")}
                        </p>
                    </div>
                    <Field
                        id="rule-min-value"
                        label="Deal value at least (₹)"
                        error={errors.min_deal_value}
                        hint="Leave empty for any deal value, including leads with none."
                    >
                        <TextInput
                            id="rule-min-value"
                            inputMode="numeric"
                            value={minValue}
                            invalid={Boolean(errors.min_deal_value)}
                            onChange={(event) => setMinValue(event.target.value)}
                            placeholder="e.g. 100000"
                        />
                    </Field>
                </section>

                <section className="space-y-3 rounded-xl border border-slate-200 p-4 dark:border-slate-800">
                    <p className="text-xs font-semibold tracking-wide text-slate-500 uppercase dark:text-slate-400">
                        Then plan this task
                    </p>
                    <FieldChips
                        fields={fields}
                        label="Insert into the title"
                        onInsert={(key) =>
                            setValues((v) => ({ ...v, task_title: insertField(titleRef.current, v.task_title, key) }))
                        }
                    />
                    <Field id="rule-title" label="Title" required error={errors.task_title}>
                        <TextInput
                            id="rule-title"
                            ref={titleRef}
                            value={values.task_title}
                            maxLength={MAX_TITLE}
                            invalid={Boolean(errors.task_title)}
                            onChange={(event) => setValues({ ...values, task_title: event.target.value })}
                            placeholder="e.g. Intro call with {{first_name}}"
                        />
                    </Field>
                    {typos.length > 0 && (
                        <p className="flex items-start gap-2 text-xs text-amber-700 dark:text-amber-300" role="status">
                            <CircleAlert className="mt-0.5 size-3.5 shrink-0" aria-hidden="true" />
                            <span>
                                Not a field: {typos.map((t) => `{{${t}}}`).join(", ")}. Check the spelling, or use the
                                buttons above.
                            </span>
                        </p>
                    )}
                    <div className="grid gap-3 sm:grid-cols-2">
                        <Field id="rule-type" label="Type" error={errors.task_type}>
                            <Select
                                id="rule-type"
                                value={values.task_type}
                                onChange={(event) =>
                                    setValues({ ...values, task_type: event.target.value as AutomationRuleInput["task_type"] })
                                }
                            >
                                {TASK_TYPES.map((type) => (
                                    <option key={type} value={type}>
                                        {TASK_TYPE_STYLES[type].label}
                                    </option>
                                ))}
                            </Select>
                        </Field>
                        <Field
                            id="rule-due"
                            label="Due after (days)"
                            error={errors.due_in_days}
                            hint="0 means the day the lead comes in."
                        >
                            <TextInput
                                id="rule-due"
                                inputMode="numeric"
                                value={dueDays}
                                invalid={Boolean(errors.due_in_days)}
                                onChange={(event) => setDueDays(event.target.value)}
                            />
                        </Field>
                    </div>
                    <Field
                        id="rule-assignee"
                        label="For"
                        error={errors.assignee_id}
                        hint="They get their usual reminder for it."
                    >
                        <Select
                            id="rule-assignee"
                            value={values.assignee_id ?? ""}
                            onChange={(event) =>
                                setValues({
                                    ...values,
                                    assignee_id: event.target.value === "" ? null : Number(event.target.value),
                                })
                            }
                        >
                            <option value="">The lead's owner</option>
                            {team.map((member) => (
                                <option key={member.id} value={member.id}>
                                    {member.name}
                                </option>
                            ))}
                        </Select>
                    </Field>
                </section>

                <label className="flex items-start gap-2.5 text-sm text-slate-700 dark:text-slate-300">
                    <input
                        type="checkbox"
                        checked={values.enabled}
                        onChange={(event) => setValues({ ...values, enabled: event.target.checked })}
                        className="mt-0.5 size-4 rounded border-slate-300 text-brand-600 dark:border-slate-600"
                    />
                    <span>
                        Switched on
                        <span className="block text-xs text-slate-500 dark:text-slate-400">
                            Off keeps the rule but stops it running.
                        </span>
                    </span>
                </label>

                {errors.form && (
                    <p className="text-sm text-rose-600 dark:text-rose-400" role="alert">
                        {errors.form}
                    </p>
                )}
            </ModalBody>

            <ModalFooter>
                <Button onClick={onCancel}>Cancel</Button>
                <Button type="submit" variant="primary" loading={saving}>
                    {rule ? "Save changes" : "Create rule"}
                </Button>
            </ModalFooter>
        </form>
    );
}

/** Who the rotation gives the next lead to: the first active member after the last one, wrapping round. */
function nextInRotation(memberIds: number[], lastId: number | null, team: TeamMember[]): TeamMember | undefined {
    const inTurn = team.filter((m) => m.active && memberIds.includes(m.id)).sort((a, b) => a.id - b.id);
    return inTurn.find((m) => lastId === null || m.id > lastId) ?? inTurn[0];
}

/**
 * Round-robin assignment: new leads nobody was chosen for go to these people
 * in turns. Runs on the server as the lead is saved, before the rules below.
 */
function RotationCard({ team }: { team: TeamMember[] }) {
    const toast = useToast();
    const setting = useAsync((signal) => api.getLeadAssignment(signal), []);
    const [chosen, setChosen] = useState<number[] | null>(null);
    const [saving, setSaving] = useState(false);

    const saved = setting.data;
    const memberIds = chosen ?? saved?.member_ids ?? [];
    const dirty = chosen !== null && saved != null && !sameIds(chosen, saved.member_ids);

    const save = async (enabled: boolean, ids: number[]) => {
        setSaving(true);
        try {
            await api.saveLeadAssignment({ enabled, member_ids: ids });
            toast.success(
                enabled === saved?.enabled
                    ? "Saved who takes turns."
                    : enabled
                      ? "New leads are now shared out in turns."
                      : "New leads are no longer shared out automatically.",
            );
            setChosen(null);
            setting.refetch();
        } catch (cause) {
            toast.error(cause instanceof ApiError ? (cause.details?.member_ids ?? cause.message) : "Could not save.");
        } finally {
            setSaving(false);
        }
    };

    const toggleMember = (id: number) =>
        setChosen(memberIds.includes(id) ? memberIds.filter((m) => m !== id) : [...memberIds, id]);

    const next = saved?.enabled ? nextInRotation(saved.member_ids, saved.last_member_id, team) : undefined;
    // Switched-off people can stay chosen (they are skipped), but are only offered if already chosen.
    const offered = team.filter((m) => m.active || memberIds.includes(m.id));

    return (
        <Card className="mb-6">
            <CardHeader
                title="Share out new leads in turns"
                subtitle="A new lead with nobody assigned goes to the next person below, one after another. Leads a salesperson adds stay theirs."
                action={
                    saved && (
                        <button
                            type="button"
                            role="switch"
                            aria-checked={saved.enabled}
                            aria-label={saved.enabled ? "Switch off sharing out leads" : "Switch on sharing out leads"}
                            disabled={saving || (!saved.enabled && memberIds.length === 0)}
                            title={!saved.enabled && memberIds.length === 0 ? "Choose who takes turns first" : undefined}
                            onClick={() => save(!saved.enabled, memberIds)}
                            className={cx(
                                "relative inline-flex h-5 w-9 shrink-0 cursor-pointer items-center rounded-full transition-colors disabled:cursor-not-allowed disabled:opacity-50",
                                saved.enabled ? "bg-brand-600" : "bg-slate-300 dark:bg-slate-700",
                            )}
                        >
                            <span
                                className={cx(
                                    "inline-block size-4 rounded-full bg-white shadow transition-transform",
                                    saved.enabled ? "translate-x-4.5" : "translate-x-0.5",
                                )}
                            />
                        </button>
                    )
                }
            />
            {setting.loading ? (
                <div className="px-5 py-4">
                    <Skeleton className="h-10 w-full" />
                </div>
            ) : setting.error ? (
                <div className="p-4">
                    <ErrorState message={setting.error.message} onRetry={setting.refetch} />
                </div>
            ) : (
                <div className="space-y-3 px-5 py-4">
                    <p className="text-xs font-medium text-slate-600 dark:text-slate-300">Who takes turns</p>
                    {offered.length === 0 ? (
                        <p className="text-sm text-slate-500 dark:text-slate-400">Add people on the Team page first.</p>
                    ) : (
                        <div className="flex flex-wrap gap-2">
                            {offered.map((m) => (
                                <label
                                    key={m.id}
                                    className={cx(
                                        "flex cursor-pointer items-center gap-2 rounded-full border px-3 py-1.5 text-sm",
                                        memberIds.includes(m.id)
                                            ? "border-brand-300 bg-brand-50 text-brand-800 dark:border-brand-700 dark:bg-brand-950 dark:text-brand-200"
                                            : "border-slate-200 text-slate-700 dark:border-slate-700 dark:text-slate-300",
                                    )}
                                >
                                    <input
                                        type="checkbox"
                                        className="size-3.5 accent-brand-600"
                                        checked={memberIds.includes(m.id)}
                                        onChange={() => toggleMember(m.id)}
                                    />
                                    {m.name}
                                    {!m.active && <span className="text-xs text-slate-400">(switched off, skipped)</span>}
                                </label>
                            ))}
                        </div>
                    )}
                    <div className="flex flex-wrap items-center gap-3">
                        {dirty && (
                            <Button
                                size="sm"
                                variant="primary"
                                loading={saving}
                                disabled={saved!.enabled && memberIds.length === 0}
                                onClick={() => save(saved!.enabled, memberIds)}
                            >
                                Save
                            </Button>
                        )}
                        <p className="text-xs text-slate-500 dark:text-slate-400">
                            {!saved?.enabled
                                ? "Off: new leads stay unassigned until someone assigns them."
                                : next
                                  ? `On. The next new lead goes to ${next.name}.`
                                  : "On, but everyone chosen is switched off, so new leads stay unassigned."}
                        </p>
                    </div>
                </div>
            )}
        </Card>
    );
}

function sameIds(a: number[], b: number[]): boolean {
    return a.length === b.length && a.every((id) => b.includes(id));
}
