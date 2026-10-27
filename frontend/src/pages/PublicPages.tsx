import { useState } from "react";
import type { FormEvent, ReactNode } from "react";
import { CalendarCheck, CircleCheck, Star } from "lucide-react";

import { ApiError, api } from "../lib/api";
import { cx } from "../lib/display";
import type { BookedMeeting } from "../lib/types";
import { useAsync } from "../hooks/useAsync";
import { Button, Field, Skeleton, TextInput, Textarea } from "../components/ui";

/**
 * The pages a lead opens from a link, without logging in: /booking/<token>
 * and /feedback/<token>. main.tsx shows them instead of the app, outside the
 * login.
 */
export function PublicPage({ path }: { path: string }) {
    const [, kind, token = ""] = path.split("/");
    return (
        <div className="min-h-dvh bg-slate-50 px-4 py-10 dark:bg-slate-950">
            <div className="mx-auto max-w-xl">
                {kind === "booking" ? <BookingPage token={token} /> : <FeedbackPage token={token} />}
            </div>
        </div>
    );
}

function Panel({ children }: { children: ReactNode }) {
    return (
        <div className="rounded-2xl border border-slate-200 bg-white p-6 shadow-sm dark:border-slate-800 dark:bg-slate-900">
            {children}
        </div>
    );
}

function LinkProblem({ message }: { message: string }) {
    return (
        <Panel>
            <h1 className="text-lg font-semibold text-slate-900 dark:text-slate-100">This link does not work</h1>
            <p className="mt-2 text-sm text-slate-600 dark:text-slate-300">{message}</p>
        </Panel>
    );
}

function Loading() {
    return (
        <Panel>
            <Skeleton className="mb-3 h-6 w-2/3" />
            <Skeleton className="h-24 w-full" />
        </Panel>
    );
}

/** "Tuesday 28 October", read as a calendar date with no time zone shift. */
function longDate(date: string): string {
    return new Date(`${date}T00:00:00`).toLocaleDateString(undefined, {
        weekday: "long",
        day: "numeric",
        month: "long",
    });
}

function shortDay(date: string): { weekday: string; day: string } {
    const d = new Date(`${date}T00:00:00`);
    return {
        weekday: d.toLocaleDateString(undefined, { weekday: "short" }),
        day: d.toLocaleDateString(undefined, { day: "numeric", month: "short" }),
    };
}

// ---------------------------------------------------------------------------
// Booking
// ---------------------------------------------------------------------------

function BookingPage({ token }: { token: string }) {
    const page = useAsync((signal) => api.bookingPage(token, signal), [token]);
    const [day, setDay] = useState<string | null>(null);
    const [time, setTime] = useState<string | null>(null);
    const [note, setNote] = useState("");
    const [booking, setBooking] = useState(false);
    const [error, setError] = useState("");
    const [booked, setBooked] = useState<BookedMeeting | null>(null);

    if (page.loading && !page.data) return <Loading />;
    if (page.error) return <LinkProblem message={page.error.message} />;
    const data = page.data!;
    const done = booked ?? data.booked;

    if (done) {
        return (
            <Panel>
                <CircleCheck className="size-10 text-emerald-600" aria-hidden="true" />
                <h1 className="mt-3 text-xl font-semibold text-slate-900 dark:text-slate-100">
                    {booked ? "You're booked!" : "You already have a meeting booked"}
                </h1>
                <p className="mt-2 text-slate-700 dark:text-slate-200">
                    {longDate(done.date)} at <strong>{done.time}</strong>
                    {done.owner_name && <> with {done.owner_name}</>}.
                </p>
                <p className="mt-2 text-sm text-slate-500 dark:text-slate-400">
                    To change it, just reply to the message this link came in.
                </p>
            </Panel>
        );
    }

    if (!data.owner_name) {
        return <LinkProblem message="Nobody is free to take a meeting through this link yet. Please contact us directly." />;
    }

    const chosenDay = data.days.find((d) => d.date === day) ?? data.days[0];

    const submit = async (event: FormEvent) => {
        event.preventDefault();
        if (!chosenDay || !time) return;
        setBooking(true);
        setError("");
        try {
            setBooked(await api.bookMeeting(token, { date: chosenDay.date, time, note }));
        } catch (cause) {
            setError(
                cause instanceof ApiError
                    ? (cause.details?.time ?? cause.details?.note ?? cause.message)
                    : "Could not book. Please try again.",
            );
            setTime(null);
            page.refetch(); // the slot may have just been taken
        } finally {
            setBooking(false);
        }
    };

    return (
        <Panel>
            <CalendarCheck className="size-8 text-brand-600" aria-hidden="true" />
            <h1 className="mt-3 text-xl font-semibold text-slate-900 dark:text-slate-100">
                Hi {data.lead_name}, book a meeting with {data.owner_name}
            </h1>
            <p className="mt-1 text-sm text-slate-600 dark:text-slate-300">
                Pick a day and a time. The meeting is {data.slot_minutes} minutes.
            </p>

            {data.days.length === 0 ? (
                <p className="mt-6 text-sm text-slate-600 dark:text-slate-300">
                    There are no free times in the next two weeks. Please contact us directly.
                </p>
            ) : (
                <form onSubmit={submit} className="mt-6 space-y-5">
                    <div>
                        <p className="mb-2 text-xs font-medium text-slate-600 dark:text-slate-300">Day</p>
                        <div className="-mx-1 flex gap-2 overflow-x-auto px-1 pb-1">
                            {data.days.map((d) => {
                                const { weekday, day: label } = shortDay(d.date);
                                const selected = d.date === chosenDay.date;
                                return (
                                    <button
                                        key={d.date}
                                        type="button"
                                        onClick={() => {
                                            setDay(d.date);
                                            setTime(null);
                                        }}
                                        aria-pressed={selected}
                                        className={cx(
                                            "flex min-w-16 shrink-0 flex-col items-center rounded-xl border px-3 py-2 text-sm",
                                            selected
                                                ? "border-brand-600 bg-brand-600 text-white"
                                                : "border-slate-200 text-slate-700 hover:border-brand-400 dark:border-slate-700 dark:text-slate-200",
                                        )}
                                    >
                                        <span className="text-xs opacity-80">{weekday}</span>
                                        <span className="font-medium">{label}</span>
                                    </button>
                                );
                            })}
                        </div>
                    </div>

                    <div>
                        <p className="mb-2 text-xs font-medium text-slate-600 dark:text-slate-300">
                            Time on {longDate(chosenDay.date)}
                        </p>
                        <div className="grid grid-cols-3 gap-2 sm:grid-cols-4">
                            {chosenDay.times.map((t) => (
                                <button
                                    key={t}
                                    type="button"
                                    onClick={() => setTime(t)}
                                    aria-pressed={time === t}
                                    className={cx(
                                        "rounded-lg border py-2 text-sm font-medium",
                                        time === t
                                            ? "border-brand-600 bg-brand-600 text-white"
                                            : "border-slate-200 text-slate-700 hover:border-brand-400 dark:border-slate-700 dark:text-slate-200",
                                    )}
                                >
                                    {t}
                                </button>
                            ))}
                        </div>
                    </div>

                    <Field id="booking-note" label="What would you like to talk about? (optional)">
                        <Textarea
                            id="booking-note"
                            rows={2}
                            maxLength={500}
                            value={note}
                            onChange={(e) => setNote(e.target.value)}
                        />
                    </Field>

                    {error && <p className="text-sm text-rose-600 dark:text-rose-400">{error}</p>}

                    <Button type="submit" variant="primary" loading={booking} disabled={!time}>
                        {time ? `Book ${shortDay(chosenDay.date).day} at ${time}` : "Choose a time"}
                    </Button>
                </form>
            )}
        </Panel>
    );
}

// ---------------------------------------------------------------------------
// Feedback
// ---------------------------------------------------------------------------

const RATING_WORDS = ["", "Poor", "Not great", "OK", "Good", "Excellent"];

function FeedbackPage({ token }: { token: string }) {
    const page = useAsync((signal) => api.feedbackPage(token, signal), [token]);
    const [rating, setRating] = useState(0);
    const [comment, setComment] = useState("");
    const [refer, setRefer] = useState(false);
    const [friend, setFriend] = useState({ name: "", email: "", phone: "", company: "" });
    const [errors, setErrors] = useState<Record<string, string>>({});
    const [sending, setSending] = useState(false);
    const [sent, setSent] = useState(false);

    if (page.loading && !page.data) return <Loading />;
    if (page.error) return <LinkProblem message={page.error.message} />;
    const data = page.data!;

    if (sent || data.given) {
        return (
            <Panel>
                <CircleCheck className="size-10 text-emerald-600" aria-hidden="true" />
                <h1 className="mt-3 text-xl font-semibold text-slate-900 dark:text-slate-100">
                    Thank you, {data.lead_name}!
                </h1>
                <p className="mt-2 text-slate-700 dark:text-slate-200">
                    {sent ? "Your feedback means a lot to us." : "You have already sent your feedback. Thank you!"}
                </p>
            </Panel>
        );
    }

    const submit = async (event: FormEvent) => {
        event.preventDefault();
        if (rating === 0) {
            setErrors({ rating: "Choose 1 to 5 stars." });
            return;
        }
        setSending(true);
        setErrors({});
        try {
            await api.giveFeedback(token, { rating, comment, referral: refer ? friend : null });
            setSent(true);
        } catch (cause) {
            if (cause instanceof ApiError && cause.isConflict) setSent(true);
            else if (cause instanceof ApiError && cause.details) setErrors(cause.details);
            else setErrors({ form: cause instanceof ApiError ? cause.message : "Could not send. Please try again." });
        } finally {
            setSending(false);
        }
    };

    const setFriendField = (field: keyof typeof friend) => (e: { target: { value: string } }) =>
        setFriend((f) => ({ ...f, [field]: e.target.value }));

    return (
        <Panel>
            <h1 className="text-xl font-semibold text-slate-900 dark:text-slate-100">
                Thank you, {data.lead_name}!
            </h1>
            <p className="mt-1 text-sm text-slate-600 dark:text-slate-300">
                Thank you for choosing us{data.owner_name && <>. It was a pleasure for {data.owner_name} to work with you</>}.
                How did we do?
            </p>

            <form onSubmit={submit} className="mt-6 space-y-5">
                <div>
                    <div className="flex items-center gap-1" role="radiogroup" aria-label="Your rating">
                        {[1, 2, 3, 4, 5].map((n) => (
                            <button
                                key={n}
                                type="button"
                                role="radio"
                                aria-checked={rating === n}
                                aria-label={`${n} star${n === 1 ? "" : "s"}: ${RATING_WORDS[n]}`}
                                onClick={() => setRating(n)}
                                className="rounded p-0.5"
                            >
                                <Star
                                    aria-hidden="true"
                                    className={cx(
                                        "size-9 transition-colors",
                                        n <= rating ? "fill-amber-400 text-amber-400" : "text-slate-300 dark:text-slate-600",
                                    )}
                                />
                            </button>
                        ))}
                        {rating > 0 && (
                            <span className="ml-2 text-sm font-medium text-slate-700 dark:text-slate-200">
                                {RATING_WORDS[rating]}
                            </span>
                        )}
                    </div>
                    {errors.rating && <p className="mt-1 text-sm text-rose-600 dark:text-rose-400">{errors.rating}</p>}
                </div>

                <Field id="feedback-comment" label="Anything you'd like to tell us? (optional)" error={errors.comment}>
                    <Textarea
                        id="feedback-comment"
                        rows={3}
                        maxLength={2000}
                        value={comment}
                        onChange={(e) => setComment(e.target.value)}
                    />
                </Field>

                <label className="flex items-center gap-2 text-sm text-slate-700 dark:text-slate-200">
                    <input
                        type="checkbox"
                        className="size-4 accent-brand-600"
                        checked={refer}
                        onChange={(e) => setRefer(e.target.checked)}
                    />
                    I know someone you could help too
                </label>

                {refer && (
                    <div className="grid gap-3 rounded-xl border border-slate-200 p-4 sm:grid-cols-2 dark:border-slate-700">
                        <Field id="friend-name" label="Their name" error={errors["referral.name"]}>
                            <TextInput id="friend-name" value={friend.name} onChange={setFriendField("name")} />
                        </Field>
                        <Field id="friend-email" label="Their email" error={errors["referral.email"]}>
                            <TextInput id="friend-email" type="email" value={friend.email} onChange={setFriendField("email")} />
                        </Field>
                        <Field id="friend-phone" label="Their phone (optional)" error={errors["referral.phone"]}>
                            <TextInput id="friend-phone" type="tel" value={friend.phone} onChange={setFriendField("phone")} />
                        </Field>
                        <Field id="friend-company" label="Their company (optional)" error={errors["referral.company"]}>
                            <TextInput id="friend-company" value={friend.company} onChange={setFriendField("company")} />
                        </Field>
                    </div>
                )}

                {errors.form && <p className="text-sm text-rose-600 dark:text-rose-400">{errors.form}</p>}

                <Button type="submit" variant="primary" loading={sending}>
                    Send feedback
                </Button>
            </form>
        </Panel>
    );
}
