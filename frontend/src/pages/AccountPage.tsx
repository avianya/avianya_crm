import { useState } from "react";
import type { FormEvent } from "react";

import { ApiError, api } from "../lib/api";
import { ROLE_LABELS, useAuth } from "../context/auth";
import { useToast } from "../context/toast";
import { Button, Card, CardHeader, Field, PageHeader, TextInput } from "../components/ui";

/** Your own account: who you are, and changing your password. */
export function AccountPage() {
    const { user } = useAuth();
    const toast = useToast();
    const [current, setCurrent] = useState("");
    const [next, setNext] = useState("");
    const [repeat, setRepeat] = useState("");
    const [showPasswords, setShowPasswords] = useState(false);
    const [errors, setErrors] = useState<Record<string, string>>({});
    const [busy, setBusy] = useState(false);

    const submit = async (event: FormEvent) => {
        event.preventDefault();
        if (next !== repeat) {
            setErrors({ repeat: "The two new passwords are not the same." });
            return;
        }
        setBusy(true);
        setErrors({});
        try {
            await api.changePassword(current, next);
            setCurrent("");
            setNext("");
            setRepeat("");
            toast.success("Password changed. You are logged out everywhere else.");
        } catch (cause) {
            if (cause instanceof ApiError && cause.details) setErrors(cause.details);
            else toast.error(cause instanceof ApiError ? cause.message : "Could not change the password.");
        } finally {
            setBusy(false);
        }
    };

    return (
        <>
            <PageHeader title="My account" subtitle={`${user.name} · ${user.email} · ${ROLE_LABELS[user.role]}`} />
            <Card className="max-w-lg">
                <CardHeader title="Change password" subtitle="At least 10 characters. Other browsers you are logged in on are logged out." />
                <form onSubmit={submit} className="space-y-4 p-5">
                    <Field id="current-password" label="Current password" error={errors.current_password}>
                        <TextInput
                            id="current-password"
                            type={showPasswords ? "text" : "password"}
                            autoComplete="current-password"
                            value={current}
                            onChange={(e) => setCurrent(e.target.value)}
                            invalid={!!errors.current_password}
                            required
                        />
                    </Field>
                    <Field id="new-password" label="New password" error={errors.new_password}>
                        <TextInput
                            id="new-password"
                            type={showPasswords ? "text" : "password"}
                            autoComplete="new-password"
                            value={next}
                            onChange={(e) => setNext(e.target.value)}
                            invalid={!!errors.new_password}
                            required
                        />
                    </Field>
                    <Field id="repeat-password" label="New password again" error={errors.repeat}>
                        <TextInput
                            id="repeat-password"
                            type={showPasswords ? "text" : "password"}
                            autoComplete="new-password"
                            value={repeat}
                            onChange={(e) => setRepeat(e.target.value)}
                            invalid={!!errors.repeat}
                            required
                        />
                    </Field>
                    <label className="-mt-2 flex items-center gap-2 text-xs text-slate-500 dark:text-slate-400">
                        <input
                            type="checkbox"
                            className="size-3.5 accent-brand-600"
                            checked={showPasswords}
                            onChange={(e) => setShowPasswords(e.target.checked)}
                        />
                        Show passwords
                    </label>
                    <Button type="submit" variant="primary" loading={busy}>
                        Change password
                    </Button>
                </form>
            </Card>
        </>
    );
}
