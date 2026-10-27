import { useState } from "react";
import type { FormEvent } from "react";
import {
    BellOff,
    CircleAlert,
    CircleCheck,
    KeyRound,
    Mail,
    Newspaper,
    Pencil,
    Send,
    Trash2,
    UserPlus,
    UsersRound,
} from "lucide-react";

import { ApiError, api } from "../lib/api";
import { avatarColor, cx, formatClock, formatRelative, formatReminder, initials } from "../lib/display";
import type { DigestRecord, TeamMember, TeamMemberInput } from "../lib/types";
import { useAsync } from "../hooks/useAsync";
import { invalidateTeam, useTeam } from "../hooks/useTeam";
import { useToast } from "../context/toast";
import { ROLE_LABELS, useAuth } from "../context/auth";
import type { Role } from "../lib/types";
import { ConfirmDialog } from "../components/ConfirmDialog";
import { ReminderSelect } from "../components/EventFields";
import { Modal, ModalBody, ModalFooter } from "../components/Modal";
import { Badge, Button, Card, CardHeader, EmptyState, ErrorState, Field, PageHeader, Select, Skeleton, TextInput } from "../components/ui";

/**
 * The people leads are assigned to and events are for, how their reminders
 * reach them, and their logins. Everyone can see the list; only admins change
 * it -- add people, give them a role and a password, or switch a login off.
 */
export function TeamPage() {
    const toast = useToast();
    const team = useTeam();
    const settings = useAsync((signal) => api.reminderSettings(signal), []);
    const { is, user } = useAuth();
    const isAdmin = is("admin");
    const [passwordFor, setPasswordFor] = useState<TeamMember | null>(null);

    const changeAccount = async (member: TeamMember, change: { role?: Role; active?: boolean }) => {
        try {
            await api.updateMemberAccount(member.id, change);
            toast.success(
                change.role
                    ? `${member.name} is now ${ROLE_LABELS[change.role]}.`
                    : change.active
                      ? `${member.name} can log in again.`
                      : `${member.name} can no longer log in.`,
            );
            reload();
        } catch (cause) {
            toast.error(cause instanceof ApiError ? cause.message : "Could not change the account.");
        }
    };

    const [editing, setEditing] = useState<TeamMember | "new" | null>(null);
    const [deleting, setDeleting] = useState<TeamMember | null>(null);
    const [testingId, setTestingId] = useState<number | null>(null);
    const [digestId, setDigestId] = useState<number | null>(null);

    const reload = () => {
        invalidateTeam();
        team.refetch();
    };

    const sendTest = async (member: TeamMember) => {
        setTestingId(member.id);
        try {
            const result = await api.sendTestEmail(member.id);
            toast.success(`Test email sent to ${result.sent_to}. Check the inbox (and spam).`);
        } catch (cause) {
            toast.error(cause instanceof ApiError ? cause.message : "Could not send the test email.");
        } finally {
            setTestingId(null);
        }
    };

    const sendDigest = async (member: TeamMember) => {
        setDigestId(member.id);
        try {
            const result = await api.sendDigestNow(member.id);
            toast.success(`Today's summary sent to ${result.sent_to}.`);
        } catch (cause) {
            toast.error(cause instanceof ApiError ? cause.message : "Could not send the summary.");
        } finally {
            setDigestId(null);
        }
    };

    const remove = async () => {
        if (!deleting) return;
        try {
            await api.deleteTeamMember(deleting.id);
            toast.success(`${deleting.name} was removed. Their leads and events are now unassigned.`);
            reload();
        } catch (cause) {
            toast.error(cause instanceof ApiError ? cause.message : "Could not remove the team member.");
            throw cause;
        }
    };

    const emailOn = settings.data?.email_configured ?? false;
    const members = team.data ?? [];

    return (
        <>
            <PageHeader
                title="Team"
                subtitle="Who leads are assigned to, their logins and roles, and how their reminders reach them."
                action={
                    isAdmin && (
                        <Button variant="primary" onClick={() => setEditing("new")}>
                            <UserPlus className="size-4" aria-hidden="true" />
                            Add member
                        </Button>
                    )
                }
            />

            <div className="grid gap-6 lg:grid-cols-3">
                <Card className="lg:col-span-2">
                    <CardHeader
                        title="Members"
                        subtitle={team.data ? `${members.length} ${members.length === 1 ? "person" : "people"}` : undefined}
                    />
                    {team.loading ? (
                        <div className="space-y-3 px-5 py-4">
                            <Skeleton className="h-10 w-full" />
                            <Skeleton className="h-10 w-full" />
                        </div>
                    ) : team.error ? (
                        <div className="p-4">
                            <ErrorState message={team.error.message} onRetry={reload} />
                        </div>
                    ) : members.length === 0 ? (
                        <EmptyState
                            icon={UsersRound}
                            title="No team members yet"
                            description="Add the people who work your leads. Make them lead owners and their events' reminders go to them."
                            action={
                                <Button variant="primary" onClick={() => setEditing("new")}>
                                    Add the first member
                                </Button>
                            }
                        />
                    ) : (
                        <ul className="divide-y divide-slate-100 dark:divide-slate-800">
                            {members.map((member) => (
                                <li key={member.id} className="flex flex-wrap items-center gap-x-3 gap-y-2 px-5 py-3.5">
                                    <span
                                        className={cx(
                                            "flex size-9 shrink-0 items-center justify-center rounded-full text-sm font-semibold",
                                            avatarColor(member.name),
                                        )}
                                        aria-hidden="true"
                                    >
                                        {initials(member.name)}
                                    </span>
                                    <div className="min-w-0 flex-1 basis-48">
                                        <p className="truncate text-sm font-medium text-slate-900 dark:text-slate-100">
                                            {member.name}
                                            {member.id === user.id && <span className="ml-1 text-xs text-slate-400">(you)</span>}
                                        </p>
                                        <p className="truncate text-xs text-slate-500 dark:text-slate-400">{member.email}</p>
                                        <div className="mt-1 flex flex-wrap items-center gap-1.5">
                                            <Badge tone="bg-indigo-50 text-indigo-700 ring-indigo-200 dark:bg-indigo-950 dark:text-indigo-300 dark:ring-indigo-800">
                                                {ROLE_LABELS[member.role]}
                                            </Badge>
                                            <span className="text-[11px] text-slate-500 dark:text-slate-400">
                                                {member.signup_pending
                                                    ? "Signed up: waiting for your approval"
                                                    : !member.active
                                                    ? "Login switched off"
                                                    : !member.has_login
                                                      ? "No password yet: cannot log in"
                                                      : member.last_login_at
                                                        ? `Last login ${formatRelative(member.last_login_at)}`
                                                        : "Has a login, never used"}
                                            </span>
                                        </div>
                                    </div>
                                    <div className="flex flex-wrap items-center gap-1.5 text-xs">
                                        {member.email_reminders ? (
                                            <span className="inline-flex items-center gap-1 rounded-full bg-teal-50 px-2 py-0.5 font-medium text-teal-700 dark:bg-teal-950 dark:text-teal-300">
                                                <Mail className="size-3" aria-hidden="true" />
                                                Email reminders
                                            </span>
                                        ) : (
                                            <span className="inline-flex items-center gap-1 rounded-full bg-slate-100 px-2 py-0.5 font-medium text-slate-600 dark:bg-slate-800 dark:text-slate-300">
                                                <BellOff className="size-3" aria-hidden="true" />
                                                In-app only
                                            </span>
                                        )}
                                        {member.daily_digest && (
                                            <span className="inline-flex items-center gap-1 rounded-full bg-brand-50 px-2 py-0.5 font-medium text-brand-700 dark:bg-brand-950 dark:text-brand-300">
                                                <Newspaper className="size-3" aria-hidden="true" />
                                                Daily summary
                                            </span>
                                        )}
                                        <span className="text-slate-500 dark:text-slate-400">
                                            Default:{" "}
                                            {member.default_reminder_minutes
                                                ? formatReminder(member.default_reminder_minutes)
                                                : "no reminder"}
                                        </span>
                                    </div>
                                    {isAdmin && (
                                    <div className="ml-auto flex flex-wrap items-center gap-1">
                                        <Select
                                            aria-label={`Role of ${member.name}`}
                                            value={member.role}
                                            onChange={(e) => void changeAccount(member, { role: e.target.value as Role })}
                                            className="h-8 w-28 text-xs"
                                        >
                                            {(["admin", "manager", "sales"] as const).map((r) => (
                                                <option key={r} value={r}>
                                                    {ROLE_LABELS[r]}
                                                </option>
                                            ))}
                                        </Select>
                                        <Button size="sm" variant="ghost" onClick={() => setPasswordFor(member)}>
                                            <KeyRound className="size-3.5" aria-hidden="true" />
                                            {member.has_login ? "Reset password" : "Give login"}
                                        </Button>
                                        {member.has_login && member.id !== user.id && (
                                            <Button
                                                size="sm"
                                                variant={member.signup_pending ? "primary" : "ghost"}
                                                onClick={() => void changeAccount(member, { active: !member.active })}
                                            >
                                                {member.signup_pending ? "Approve" : member.active ? "Switch off" : "Switch on"}
                                            </Button>
                                        )}
                                        <Button
                                            size="sm"
                                            variant="ghost"
                                            loading={testingId === member.id}
                                            disabled={!emailOn || testingId !== null}
                                            title={emailOn ? `Send a test email to ${member.email}` : "Email is not set up on the server"}
                                            onClick={() => sendTest(member)}
                                        >
                                            {testingId !== member.id && <Send className="size-3.5" aria-hidden="true" />}
                                            Test email
                                        </Button>
                                        <Button
                                            size="sm"
                                            variant="ghost"
                                            loading={digestId === member.id}
                                            disabled={!emailOn || digestId !== null}
                                            title={
                                                emailOn
                                                    ? `Email ${member.name} today's summary now`
                                                    : "Email is not set up on the server"
                                            }
                                            onClick={() => sendDigest(member)}
                                        >
                                            {digestId !== member.id && <Newspaper className="size-3.5" aria-hidden="true" />}
                                            Send summary
                                        </Button>
                                        <Button size="sm" variant="ghost" aria-label={`Edit ${member.name}`} onClick={() => setEditing(member)}>
                                            <Pencil className="size-3.5" aria-hidden="true" />
                                        </Button>
                                        <Button size="sm" variant="ghost" aria-label={`Remove ${member.name}`} onClick={() => setDeleting(member)}>
                                            <Trash2 className="size-3.5" aria-hidden="true" />
                                        </Button>
                                    </div>
                                    )}
                                    {member.daily_digest && member.last_digest && (
                                        <LastDigest record={member.last_digest} />
                                    )}
                                </li>
                            ))}
                        </ul>
                    )}
                </Card>

                <EmailSetupCard
                    loading={settings.loading}
                    configured={emailOn}
                    from={settings.data?.from ?? ""}
                    timezone={settings.data?.timezone ?? ""}
                    digestHour={settings.data?.digest_hour ?? 8}
                />
            </div>

            <MemberModal
                member={editing}
                onClose={() => setEditing(null)}
                onSaved={(saved, created) => {
                    setEditing(null);
                    toast.success(created ? `${saved.name} added.` : `${saved.name} updated.`);
                    reload();
                }}
            />

            <PasswordModal
                member={passwordFor}
                onClose={() => setPasswordFor(null)}
                onSaved={(member) => {
                    setPasswordFor(null);
                    toast.success(`${member.name} can log in with the new password. Tell them it privately.`);
                    reload();
                }}
            />

            <ConfirmDialog
                open={deleting !== null}
                onClose={() => setDeleting(null)}
                onConfirm={remove}
                title="Remove team member"
                confirmLabel="Remove"
                message={
                    deleting
                        ? `Remove ${deleting.name}? Leads they own and events assigned to them stay, but become unassigned, and their reminders will not be emailed to anyone.`
                        : ""
                }
            />
        </>
    );
}

/** Whether reminder emails can go out, and how to turn them on. */
/** How the member's latest daily summary went, in one quiet line. */
function LastDigest({ record }: { record: DigestRecord }) {
    const when = formatRelative(record.updated_at);
    let text: string;
    let tone = "text-slate-500 dark:text-slate-400";
    switch (record.status) {
        case "sent":
            text = `Last summary sent ${when}.`;
            break;
        case "skipped":
            text = `No summary ${when}: nothing needed them.`;
            break;
        case "failed":
            tone = "text-rose-600 dark:text-rose-400";
            text = `Last summary failed (${record.attempts} ${record.attempts === 1 ? "try" : "tries"}): ${record.detail}`;
            break;
        default:
            text = "Today's summary is being sent…";
    }
    return <p className={cx("w-full pl-12 text-xs", tone)}>{text}</p>;
}

function EmailSetupCard({
    loading,
    configured,
    from,
    timezone,
    digestHour,
}: {
    loading: boolean;
    configured: boolean;
    from: string;
    timezone: string;
    digestHour: number;
}) {
    const digestTime = formatClock(`${String(digestHour).padStart(2, "0")}:00`);
    return (
        <Card className="self-start">
            <CardHeader title="Reminder emails" />
            <div className="space-y-3 px-5 py-4 text-sm">
                {loading ? (
                    <Skeleton className="h-16 w-full" />
                ) : configured ? (
                    <>
                        <p className="flex items-start gap-2 text-teal-800 dark:text-teal-300">
                            <CircleCheck className="mt-0.5 size-4 shrink-0" aria-hidden="true" />
                            <span>
                                On. Reminders are emailed from <strong className="font-medium break-all">{from}</strong>.
                            </span>
                        </p>
                        <p className="text-xs text-slate-500 dark:text-slate-400">
                            Use <em>Test email</em> on a member to check delivery. Times in emails are in {timezone || "the server's time zone"}.
                        </p>
                        <p className="text-xs text-slate-500 dark:text-slate-400">
                            Daily summaries go out each morning from {digestTime}, to members who have them on, on
                            days something needs them. Change the time with <code>DIGEST_HOUR</code> in{" "}
                            <code>backend/.env</code>.
                        </p>
                    </>
                ) : (
                    <>
                        <p className="flex items-start gap-2 text-amber-800 dark:text-amber-300">
                            <CircleAlert className="mt-0.5 size-4 shrink-0" aria-hidden="true" />
                            <span>
                                Not set up. Reminders show in the app (the bell) but are not emailed, and no daily
                                summaries go out.
                            </span>
                        </p>
                        <div className="text-xs text-slate-600 dark:text-slate-300">
                            <p className="font-medium">To send through Gmail:</p>
                            <ol className="mt-1 list-decimal space-y-1 pl-4">
                                <li>Turn on 2-Step Verification for the Google account.</li>
                                <li>Create an app password (Google Account → Security → App passwords).</li>
                                <li>
                                    Add to <code className="rounded bg-slate-100 px-1 dark:bg-slate-800">backend/.env</code>:
                                    <pre className="mt-1 overflow-x-auto rounded-md bg-slate-100 p-2 text-[11px] leading-relaxed dark:bg-slate-800">
{`SMTP_HOST=smtp.gmail.com
SMTP_PORT=587
SMTP_USERNAME=you@gmail.com
SMTP_PASSWORD=your app password
SMTP_FROM=Pipeline CRM <you@gmail.com>`}
                                    </pre>
                                </li>
                                <li>Restart the backend, then use Test email here.</li>
                            </ol>
                            <p className="mt-2 text-slate-500 dark:text-slate-400">
                                The password stays on the server; it is never sent to the browser.
                            </p>
                        </div>
                    </>
                )}
            </div>
        </Card>
    );
}

function MemberModal({
    member,
    onClose,
    onSaved,
}: {
    member: TeamMember | "new" | null;
    onClose: () => void;
    onSaved: (member: TeamMember, created: boolean) => void;
}) {
    const isNew = member === "new";
    return (
        <Modal
            open={member !== null}
            onClose={onClose}
            title={isNew ? "Add team member" : "Edit team member"}
            description="Reminders for their events go to this email address."
        >
            {member !== null && (
                <MemberForm
                    key={isNew ? "new" : member.id}
                    member={isNew ? null : member}
                    onCancel={onClose}
                    onSaved={onSaved}
                />
            )}
        </Modal>
    );
}

function MemberForm({
    member,
    onCancel,
    onSaved,
}: {
    member: TeamMember | null;
    onCancel: () => void;
    onSaved: (member: TeamMember, created: boolean) => void;
}) {
    const [values, setValues] = useState<TeamMemberInput>(() => ({
        name: member?.name ?? "",
        email: member?.email ?? "",
        email_reminders: member?.email_reminders ?? true,
        default_reminder_minutes: member ? member.default_reminder_minutes : 15,
        daily_digest: member?.daily_digest ?? true,
    }));
    const [errors, setErrors] = useState<Record<string, string>>({});
    const [saving, setSaving] = useState(false);

    const submit = async (event: FormEvent) => {
        event.preventDefault();
        const next: Record<string, string> = {};
        if (!values.name.trim()) next.name = "Enter a name.";
        if (!/^[^@\s]+@[^@\s]+\.[^@\s]+$/.test(values.email.trim())) next.email = "Enter a valid email address.";
        setErrors(next);
        if (Object.keys(next).length > 0) return;

        setSaving(true);
        try {
            const input = { ...values, name: values.name.trim(), email: values.email.trim() };
            const saved = member ? await api.updateTeamMember(member.id, input) : await api.createTeamMember(input);
            onSaved(saved, member === null);
        } catch (cause) {
            if (cause instanceof ApiError && cause.isValidationError && cause.details) {
                setErrors(cause.details);
            } else if (cause instanceof ApiError && cause.isConflict) {
                setErrors({ email: "Another team member already uses this email address." });
            } else {
                setErrors({ form: cause instanceof ApiError ? cause.message : "Could not save." });
            }
        } finally {
            setSaving(false);
        }
    };

    return (
        <form onSubmit={submit} noValidate>
            <ModalBody>
                <Field id="member-name" label="Name" required error={errors.name}>
                    <TextInput
                        id="member-name"
                        value={values.name}
                        maxLength={120}
                        invalid={Boolean(errors.name)}
                        onChange={(event) => setValues({ ...values, name: event.target.value })}
                        autoFocus
                    />
                </Field>
                <Field id="member-email" label="Email" required error={errors.email}>
                    <TextInput
                        id="member-email"
                        type="email"
                        value={values.email}
                        maxLength={254}
                        invalid={Boolean(errors.email)}
                        onChange={(event) => setValues({ ...values, email: event.target.value })}
                    />
                </Field>
                <label className="flex items-start gap-2.5 text-sm text-slate-700 dark:text-slate-300">
                    <input
                        type="checkbox"
                        checked={values.email_reminders}
                        onChange={(event) => setValues({ ...values, email_reminders: event.target.checked })}
                        className="mt-0.5 size-4 rounded border-slate-300 text-brand-600 dark:border-slate-600"
                    />
                    <span>
                        Email me reminders
                        <span className="block text-xs text-slate-500 dark:text-slate-400">
                            Off means reminders only appear in the app.
                        </span>
                    </span>
                </label>
                <label className="flex items-start gap-2.5 text-sm text-slate-700 dark:text-slate-300">
                    <input
                        type="checkbox"
                        checked={values.daily_digest}
                        onChange={(event) => setValues({ ...values, daily_digest: event.target.checked })}
                        className="mt-0.5 size-4 rounded border-slate-300 text-brand-600 dark:border-slate-600"
                    />
                    <span>
                        Email me a daily summary
                        <span className="block text-xs text-slate-500 dark:text-slate-400">
                            Each morning: overdue and today's follow-ups, stuck deals and leads going cold. Not sent on
                            days with nothing to report.
                        </span>
                    </span>
                </label>
                <Field
                    id="member-default-reminder"
                    label="Default reminder for new events"
                    error={errors.default_reminder_minutes}
                >
                    <ReminderSelect
                        id="member-default-reminder"
                        value={values.default_reminder_minutes}
                        onChange={(minutes) => setValues({ ...values, default_reminder_minutes: minutes })}
                    />
                </Field>

                {errors.form && (
                    <p className="text-sm text-rose-600 dark:text-rose-400" role="alert">
                        {errors.form}
                    </p>
                )}
            </ModalBody>
            <ModalFooter>
                <Button onClick={onCancel}>Cancel</Button>
                <Button type="submit" variant="primary" loading={saving}>
                    {member ? "Save changes" : "Add member"}
                </Button>
            </ModalFooter>
        </form>
    );
}

/**
 * An admin setting someone's password: to give them a login, or after they
 * forgot it. Their sessions end. The password is sent once and never shown
 * again; tell it to them privately, and they can change it after logging in.
 */
function PasswordModal({
    member,
    onClose,
    onSaved,
}: {
    member: TeamMember | null;
    onClose: () => void;
    onSaved: (member: TeamMember) => void;
}) {
    const [password, setPassword] = useState("");
    const [showPassword, setShowPassword] = useState(false);
    const [error, setError] = useState("");
    const [saving, setSaving] = useState(false);

    const close = () => {
        setPassword("");
        setShowPassword(false);
        setError("");
        onClose();
    };
    const save = async (event: FormEvent) => {
        event.preventDefault();
        if (!member) return;
        setSaving(true);
        setError("");
        try {
            const saved = await api.setMemberPassword(member.id, password);
            setPassword("");
            onSaved(saved);
        } catch (cause) {
            setError(cause instanceof ApiError ? (cause.details?.password ?? cause.message) : "Could not set the password.");
        } finally {
            setSaving(false);
        }
    };

    return (
        <Modal
            open={member !== null}
            onClose={close}
            title={member?.has_login ? `Reset ${member?.name}'s password` : `Give ${member?.name ?? ""} a login`}
            description={`They log in with ${member?.email ?? ""} and this password. Any session they have now ends.`}
        >
            <form onSubmit={save}>
                <ModalBody>
                    <Field id="new-member-password" label="Password" error={error} hint="At least 10 characters.">
                        <TextInput
                            id="new-member-password"
                            type={showPassword ? "text" : "password"}
                            autoComplete="new-password"
                            value={password}
                            onChange={(e) => setPassword(e.target.value)}
                            invalid={!!error}
                            required
                        />
                    </Field>
                    <label className="mt-2 flex items-center gap-2 text-xs text-slate-500 dark:text-slate-400">
                        <input
                            type="checkbox"
                            className="size-3.5 accent-brand-600"
                            checked={showPassword}
                            onChange={(e) => setShowPassword(e.target.checked)}
                        />
                        Show password
                    </label>
                </ModalBody>
                <ModalFooter>
                    <Button variant="ghost" onClick={close}>
                        Cancel
                    </Button>
                    <Button type="submit" variant="primary" loading={saving}>
                        Set password
                    </Button>
                </ModalFooter>
            </form>
        </Modal>
    );
}
