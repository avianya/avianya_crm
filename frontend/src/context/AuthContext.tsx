import { useCallback, useEffect, useMemo, useState } from "react";
import type { FormEvent, ReactNode } from "react";
import { Target } from "lucide-react";

import { ApiError, SESSION_ENDED, api } from "../lib/api";
import type { Role, Viewer } from "../lib/types";
import { AuthContext } from "./auth";
import { Button, Field, Spinner, TextInput } from "../components/ui";

/**
 * Shows the login until someone is logged in, then the app.
 *
 * On load it asks the API who the session cookie belongs to. Any request that
 * comes back 401 later (the session expired, or was ended elsewhere) brings the
 * login back.
 */
export function AuthProvider({ children }: { children: ReactNode }) {
    const [user, setUser] = useState<Viewer | null>(null);
    const [checking, setChecking] = useState(true);

    useEffect(() => {
        const controller = new AbortController();
        api.me(controller.signal)
            .then(setUser)
            .catch(() => setUser(null))
            .finally(() => {
                if (!controller.signal.aborted) setChecking(false);
            });
        const ended = () => setUser(null);
        window.addEventListener(SESSION_ENDED, ended);
        return () => {
            controller.abort();
            window.removeEventListener(SESSION_ENDED, ended);
        };
    }, []);

    const logout = useCallback(async () => {
        try {
            await api.logout();
        } finally {
            setUser(null);
        }
    }, []);

    const value = useMemo(
        () => (user ? { user, logout, is: (...roles: Role[]) => roles.includes(user.role) } : null),
        [user, logout],
    );

    if (checking) {
        return (
            <div className="flex min-h-screen items-center justify-center">
                <Spinner className="size-6" />
            </div>
        );
    }
    if (!value) return <LoginPage onLoggedIn={setUser} />;
    return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}

type Screen = "login" | "signup" | "forgot" | "reset";

/** Login, sign up, forgotten password and the emailed reset link. */
function LoginPage({ onLoggedIn }: { onLoggedIn: (v: Viewer) => void }) {
    // The emailed link is /reset-password?token=...
    const resetToken = new URLSearchParams(window.location.search).get("token") ?? "";
    const [screen, setScreen] = useState<Screen>(
        window.location.pathname === "/reset-password" && resetToken ? "reset" : "login",
    );
    const [name, setName] = useState("");
    const [email, setEmail] = useState("");
    const [password, setPassword] = useState("");
    const [showPassword, setShowPassword] = useState(false);
    const [error, setError] = useState("");
    const [notice, setNotice] = useState("");
    const [busy, setBusy] = useState(false);

    const go = (next: Screen) => {
        setScreen(next);
        setError("");
        setPassword("");
        if (next !== "reset" && window.location.pathname === "/reset-password") {
            window.history.replaceState(null, "", "/");
        }
    };

    const submit = async (event: FormEvent) => {
        event.preventDefault();
        setBusy(true);
        setError("");
        setNotice("");
        try {
            if (screen === "login") {
                onLoggedIn(await api.login(email, password));
                return;
            }
            if (screen === "signup") {
                onLoggedIn(await api.signup(name, email, password));
                return;
            } else if (screen === "forgot") {
                const r = await api.forgotPassword(email);
                go("login");
                setNotice(r.message);
            } else {
                await api.resetPassword(resetToken, password);
                go("login");
                setNotice("Your password is set. Log in with it now.");
            }
        } catch (cause) {
            if (cause instanceof ApiError && cause.details) {
                setError(Object.entries(cause.details).map(([k, v]) => `${k} ${v}`).join("; "));
            } else {
                setError(cause instanceof ApiError ? cause.message : "Something went wrong.");
            }
            setPassword("");
        } finally {
            setBusy(false);
        }
    };

    const titles: Record<Screen, [string, string]> = {
        login: ["Log in to continue", "Log in"],
        signup: ["Create your account", "Create account"],
        forgot: ["We'll email you a link to set a new password", "Send the link"],
        reset: ["Choose a new password", "Set password"],
    };
    const needsPassword = screen !== "forgot";
    const needsEmail = screen !== "reset";

    return (
        <div className="flex min-h-screen items-center justify-center bg-slate-50 px-4 dark:bg-slate-950">
            <form
                onSubmit={submit}
                className="w-full max-w-sm space-y-5 rounded-2xl border border-slate-200 bg-white p-8 shadow-sm dark:border-slate-800 dark:bg-slate-900"
            >
                <div className="flex items-center gap-2.5">
                    <div className="flex size-9 items-center justify-center rounded-lg bg-brand-600 text-white">
                        <Target className="size-5" aria-hidden="true" />
                    </div>
                    <div>
                        <h1 className="text-lg font-semibold text-slate-900 dark:text-slate-50">Pipeline CRM</h1>
                        <p className="text-xs text-slate-500 dark:text-slate-400">{titles[screen][0]}</p>
                    </div>
                </div>

                {notice && (
                    <p className="rounded-lg border border-emerald-200 bg-emerald-50 px-3 py-2 text-xs text-emerald-800 dark:border-emerald-900 dark:bg-emerald-950/50 dark:text-emerald-200">
                        {notice}
                    </p>
                )}

                {screen === "signup" && (
                    <Field id="signup-name" label="Your name">
                        <TextInput id="signup-name" value={name} onChange={(e) => setName(e.target.value)} required autoFocus />
                    </Field>
                )}
                {needsEmail && (
                    <Field id="login-email" label="Email">
                        <TextInput
                            id="login-email"
                            type="email"
                            autoComplete="username"
                            value={email}
                            onChange={(e) => setEmail(e.target.value)}
                            required
                            autoFocus={screen !== "signup"}
                        />
                    </Field>
                )}
                {needsPassword && (
                    <>
                        <Field
                            id="login-password"
                            label={screen === "login" ? "Password" : "New password"}
                            error={error}
                            hint={screen === "login" ? undefined : "At least 10 characters."}
                        >
                            <TextInput
                                id="login-password"
                                type={showPassword ? "text" : "password"}
                                autoComplete={screen === "login" ? "current-password" : "new-password"}
                                value={password}
                                onChange={(e) => setPassword(e.target.value)}
                                invalid={!!error}
                                required
                            />
                        </Field>
                        <label className="-mt-3 flex items-center gap-2 text-xs text-slate-500 dark:text-slate-400">
                            <input
                                type="checkbox"
                                className="size-3.5 accent-brand-600"
                                checked={showPassword}
                                onChange={(e) => setShowPassword(e.target.checked)}
                            />
                            Show password
                        </label>
                    </>
                )}
                {!needsPassword && error && <p className="text-xs text-rose-600 dark:text-rose-400">{error}</p>}

                <Button type="submit" variant="primary" loading={busy} className="w-full">
                    {titles[screen][1]}
                </Button>

                <div className="flex justify-between text-xs">
                    {screen === "login" ? (
                        <>
                            <LinkButton onClick={() => go("forgot")}>Forgot password?</LinkButton>
                            <LinkButton onClick={() => go("signup")}>Create account</LinkButton>
                        </>
                    ) : (
                        <LinkButton onClick={() => go("login")}>Back to log in</LinkButton>
                    )}
                </div>
                {screen === "signup" && (
                    <p className="text-center text-xs text-slate-400 dark:text-slate-500">
                        New accounts see the leads an admin or manager assigns to them.
                    </p>
                )}
            </form>
        </div>
    );
}

function LinkButton({ onClick, children }: { onClick: () => void; children: ReactNode }) {
    return (
        <button type="button" onClick={onClick} className="font-medium text-brand-600 hover:underline dark:text-brand-400">
            {children}
        </button>
    );
}
