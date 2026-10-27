import { useCallback, useEffect, useRef, useState } from "react";
import { useNavigate } from "react-router-dom";
import { Bell, CheckCheck, Hourglass, Loader2, MailCheck, MailX, MailMinus, RotateCw } from "lucide-react";

import { ApiError, api } from "../lib/api";
import { STAGE_STYLES, TASK_TYPE_STYLES, cx, formatClock, formatRelative } from "../lib/display";
import type { AppNotification, DealAlert, NotificationList } from "../lib/types";
import { useToast } from "../context/toast";

/** How often the bell asks for new reminders while the tab is visible. */
const POLL_MS = 30_000;

/** "Today, 3:00 PM", "Sat 10 Oct, all day". */
function eventWhen(n: AppNotification): string {
    const [year, month, day] = n.due_date.split("-").map(Number);
    const date = new Date(year, month - 1, day);
    const today = new Date();
    const isToday = date.toDateString() === today.toDateString();
    const dayText = isToday
        ? "Today"
        : date.toLocaleDateString(undefined, { weekday: "short", day: "numeric", month: "short" });
    return n.start_time ? `${dayText}, ${formatClock(n.start_time)}` : `${dayText}, all day`;
}

/**
 * The reminder bell. Reminders are raised by the server's reminder job, so
 * they arrive whether or not the app was open; this polls for them, shows the
 * unread count, and pops a toast when a new one comes in.
 *
 * Each entry also says what happened to its email -- sent, failed (with the
 * mail server's reason and a Retry), or not sent and why -- so a missing
 * email is never a mystery.
 *
 * Stuck-deal alerts (a deal past its stage's time limit) come in the same
 * poll and are listed above the reminders.
 */
export function NotificationBell() {
    const navigate = useNavigate();
    const toast = useToast();
    const [list, setList] = useState<NotificationList | null>(null);
    const [open, setOpen] = useState(false);
    const [retrying, setRetrying] = useState<number | null>(null);
    const panelRef = useRef<HTMLDivElement>(null);
    const buttonRef = useRef<HTMLButtonElement>(null);

    // Ids already seen, so only genuinely new reminders pop a toast. null until
    // the first load, which never toasts: those were waiting, not arriving.
    const seen = useRef<Set<number> | null>(null);
    const seenAlerts = useRef<Set<number> | null>(null);

    const load = useCallback(async () => {
        try {
            const next = await api.listNotifications();
            if (seen.current) {
                for (const n of next.items) {
                    if (!seen.current.has(n.id) && !n.read_at) {
                        toast.info(
                            `Reminder: ${TASK_TYPE_STYLES[n.task_type].label} with ${n.lead_name}, ${eventWhen(n)}`,
                        );
                    }
                }
            }
            if (seenAlerts.current) {
                for (const a of next.deal_alerts) {
                    if (!seenAlerts.current.has(a.id) && !a.read_at && !a.resolved) {
                        toast.info(
                            `Deal stuck: ${a.lead_name} has been in ${STAGE_STYLES[a.stage].label} for ${a.days_in_stage} days`,
                        );
                    }
                }
            }
            seen.current = new Set(next.items.map((n) => n.id));
            seenAlerts.current = new Set(next.deal_alerts.map((a) => a.id));
            setList(next);
        } catch {
            // The bell is ambient: a failed poll waits for the next one rather
            // than interrupting the page with an error.
        }
    }, [toast]);

    useEffect(() => {
        void load();
        const timer = window.setInterval(() => {
            if (document.visibilityState === "visible") void load();
        }, POLL_MS);
        const onVisible = () => {
            if (document.visibilityState === "visible") void load();
        };
        document.addEventListener("visibilitychange", onVisible);
        return () => {
            window.clearInterval(timer);
            document.removeEventListener("visibilitychange", onVisible);
        };
    }, [load]);

    // Close on Escape or a click outside the panel.
    useEffect(() => {
        if (!open) return;
        const onKey = (event: KeyboardEvent) => {
            if (event.key === "Escape") {
                setOpen(false);
                buttonRef.current?.focus();
            }
        };
        const onClick = (event: MouseEvent) => {
            const target = event.target as Node;
            if (!panelRef.current?.contains(target) && !buttonRef.current?.contains(target)) setOpen(false);
        };
        document.addEventListener("keydown", onKey);
        document.addEventListener("mousedown", onClick);
        return () => {
            document.removeEventListener("keydown", onKey);
            document.removeEventListener("mousedown", onClick);
        };
    }, [open]);

    const markLocallyRead = (ids: number[] | "all") =>
        setList((current) =>
            current
                ? {
                      ...current,
                      unread: ids === "all" ? 0 : Math.max(0, current.unread - ids.filter((id) => current.items.some((n) => n.id === id && !n.read_at)).length),
                      items: current.items.map((n) =>
                          ids === "all" || ids.includes(n.id) ? { ...n, read_at: n.read_at ?? new Date().toISOString() } : n,
                      ),
                      ...(ids === "all"
                          ? {
                                alerts_unread: 0,
                                deal_alerts: current.deal_alerts.map((a) => ({
                                    ...a,
                                    read_at: a.read_at ?? new Date().toISOString(),
                                })),
                            }
                          : {}),
                  }
                : current,
        );

    const openAlert = (a: DealAlert) => {
        if (!a.read_at) {
            setList((current) =>
                current
                    ? {
                          ...current,
                          alerts_unread: a.resolved ? current.alerts_unread : Math.max(0, current.alerts_unread - 1),
                          deal_alerts: current.deal_alerts.map((i) =>
                              i.id === a.id ? { ...i, read_at: new Date().toISOString() } : i,
                          ),
                      }
                    : current,
            );
            api.markDealAlertRead(a.id).catch(() => {});
        }
        setOpen(false);
        navigate(`/leads/${a.lead_id}`);
    };

    const openLead = (n: AppNotification) => {
        if (!n.read_at) {
            markLocallyRead([n.id]);
            api.markNotificationRead(n.id).catch(() => {});
        }
        setOpen(false);
        navigate(`/leads/${n.lead_id}`);
    };

    const markAll = async () => {
        markLocallyRead("all");
        try {
            await api.markAllNotificationsRead();
        } catch {
            void load();
        }
    };

    const retry = async (n: AppNotification) => {
        setRetrying(n.id);
        try {
            const updated = await api.retryNotificationEmail(n.id);
            setList((current) =>
                current ? { ...current, items: current.items.map((i) => (i.id === n.id ? updated : i)) } : current,
            );
            toast.success("Queued. It will be sent within 30 seconds if the event is still on.");
        } catch (cause) {
            toast.error(cause instanceof ApiError ? cause.message : "Could not retry the email.");
        } finally {
            setRetrying(null);
        }
    };

    const unread = (list?.unread ?? 0) + (list?.alerts_unread ?? 0);
    const alerts = list?.deal_alerts ?? [];
    const empty = !list || (list.items.length === 0 && alerts.length === 0);

    return (
        <div className="relative">
            <button
                ref={buttonRef}
                type="button"
                onClick={() => setOpen((value) => !value)}
                aria-expanded={open}
                aria-haspopup="dialog"
                aria-label={unread > 0 ? `Notifications, ${unread} unread` : "Notifications"}
                className="relative rounded-lg border border-slate-200 bg-white p-1.5 text-slate-600 shadow-sm transition-colors hover:bg-slate-50 dark:border-slate-700 dark:bg-slate-900 dark:text-slate-300 dark:hover:bg-slate-800"
            >
                <Bell className="size-4.5" aria-hidden="true" />
                {unread > 0 && (
                    <span className="absolute -top-1.5 -right-1.5 min-w-4.5 rounded-full bg-rose-500 px-1 text-center text-[10px] leading-4.5 font-semibold text-white">
                        {unread > 99 ? "99+" : unread}
                    </span>
                )}
            </button>

            {open && (
                <div
                    ref={panelRef}
                    role="dialog"
                    aria-label="Notifications"
                    className="animate-fade-in fixed inset-x-4 top-16 z-40 overflow-hidden rounded-xl border border-slate-200 bg-white shadow-xl sm:absolute sm:inset-x-auto sm:top-full sm:right-0 sm:mt-2 sm:w-[24rem] dark:border-slate-700 dark:bg-slate-900"
                >
                    <div className="flex items-center justify-between gap-2 border-b border-slate-200 px-4 py-3 dark:border-slate-800">
                        <p className="text-sm font-semibold text-slate-900 dark:text-slate-100">Notifications</p>
                        {unread > 0 && (
                            <button
                                type="button"
                                onClick={markAll}
                                className="inline-flex items-center gap-1 rounded-md px-2 py-1 text-xs font-medium text-brand-600 hover:bg-brand-50 dark:text-brand-400 dark:hover:bg-brand-950/60"
                            >
                                <CheckCheck className="size-3.5" aria-hidden="true" />
                                Mark all read
                            </button>
                        )}
                    </div>

                    {empty ? (
                        <div className="px-4 py-8 text-center text-sm text-slate-500 dark:text-slate-400">
                            <Bell className="mx-auto mb-2 size-6 text-slate-300 dark:text-slate-600" aria-hidden="true" />
                            Nothing yet. Reminders you set on events, and deals that get stuck, show up here.
                        </div>
                    ) : (
                        <div className="max-h-[70vh] overflow-y-auto">
                        {alerts.length > 0 && (
                            <section aria-label="Stuck deals">
                                <p className="bg-slate-50 px-4 py-1.5 text-[11px] font-semibold tracking-wide text-slate-500 uppercase dark:bg-slate-950/50 dark:text-slate-400">
                                    Stuck deals
                                </p>
                                <ul className="divide-y divide-slate-100 dark:divide-slate-800">
                                    {alerts.map((a) => (
                                        <AlertRow key={a.id} alert={a} onOpen={() => openAlert(a)} />
                                    ))}
                                </ul>
                            </section>
                        )}
                        {list.items.length > 0 && alerts.length > 0 && (
                            <p className="border-t border-slate-100 bg-slate-50 px-4 py-1.5 text-[11px] font-semibold tracking-wide text-slate-500 uppercase dark:border-slate-800 dark:bg-slate-950/50 dark:text-slate-400">
                                Reminders
                            </p>
                        )}
                        <ul className="divide-y divide-slate-100 dark:divide-slate-800">
                            {list.items.map((n) => {
                                const TypeIcon = TASK_TYPE_STYLES[n.task_type].icon;
                                const closed = n.task_status === "completed" || n.task_status === "cancelled";
                                return (
                                    <li key={n.id} className={cx("relative px-4 py-3", !n.read_at && "bg-brand-50/50 dark:bg-brand-950/30")}>
                                        {!n.read_at && (
                                            <span className="absolute top-4 left-1.5 size-1.5 rounded-full bg-brand-500" aria-label="Unread" />
                                        )}
                                        <button type="button" onClick={() => openLead(n)} className="flex w-full items-start gap-3 text-left">
                                            <span className="mt-0.5 flex size-8 shrink-0 items-center justify-center rounded-full bg-slate-100 text-slate-600 dark:bg-slate-800 dark:text-slate-300">
                                                <TypeIcon className="size-4" aria-hidden="true" />
                                            </span>
                                            <span className="min-w-0 flex-1">
                                                <span className="block text-sm font-medium text-slate-900 dark:text-slate-100">
                                                    {TASK_TYPE_STYLES[n.task_type].label} with {n.lead_name}
                                                    <span className="font-normal text-slate-500 dark:text-slate-400"> · {n.lead_company}</span>
                                                </span>
                                                <span className={cx("block truncate text-xs text-slate-600 dark:text-slate-300", closed && "line-through")}>
                                                    {n.task_title}
                                                </span>
                                                <span className="block text-xs text-slate-500 dark:text-slate-400">
                                                    {eventWhen(n)}
                                                    {closed && ` · ${n.task_status === "completed" ? "Completed" : "Cancelled"}`}
                                                    {" · "}
                                                    <span title={n.created_at}>reminded {formatRelative(n.created_at)}</span>
                                                </span>
                                            </span>
                                        </button>
                                        <EmailStatus n={n} retrying={retrying === n.id} onRetry={() => retry(n)} />
                                    </li>
                                );
                            })}
                        </ul>
                        </div>
                    )}
                </div>
            )}
        </div>
    );
}

/** One stuck-deal alert. Once the deal has moved on it stays listed, greyed out. */
function AlertRow({ alert: a, onOpen }: { alert: DealAlert; onOpen: () => void }) {
    const stage = STAGE_STYLES[a.stage].label;
    return (
        <li className={cx("relative px-4 py-3", !a.read_at && !a.resolved && "bg-orange-50/60 dark:bg-orange-950/20")}>
            {!a.read_at && !a.resolved && (
                <span className="absolute top-4 left-1.5 size-1.5 rounded-full bg-orange-500" aria-label="Unread" />
            )}
            <button type="button" onClick={onOpen} className="flex w-full items-start gap-3 text-left">
                <span
                    className={cx(
                        "mt-0.5 flex size-8 shrink-0 items-center justify-center rounded-full",
                        a.resolved
                            ? "bg-slate-100 text-slate-400 dark:bg-slate-800 dark:text-slate-500"
                            : "bg-orange-100 text-orange-700 dark:bg-orange-950 dark:text-orange-300",
                    )}
                >
                    <Hourglass className="size-4" aria-hidden="true" />
                </span>
                <span className="min-w-0 flex-1">
                    <span
                        className={cx(
                            "block text-sm font-medium",
                            a.resolved ? "text-slate-500 dark:text-slate-400" : "text-slate-900 dark:text-slate-100",
                        )}
                    >
                        {a.lead_name}
                        <span className="font-normal text-slate-500 dark:text-slate-400"> · {a.lead_company}</span>
                    </span>
                    <span className="block text-xs text-slate-600 dark:text-slate-300">
                        {a.resolved
                            ? `Was stuck in ${stage} for ${a.days_in_stage} days, then moved on`
                            : `${a.days_in_stage} days in ${stage} (limit ${a.limit_days})`}
                    </span>
                    <span className="block text-xs text-slate-500 dark:text-slate-400">
                        <span title={a.created_at}>flagged {formatRelative(a.created_at)}</span>
                    </span>
                </span>
            </button>
        </li>
    );
}

/** One line on what happened to the reminder's email, with Retry where it helps. */
function EmailStatus({ n, retrying, onRetry }: { n: AppNotification; retrying: boolean; onRetry: () => void }) {
    let icon = <MailMinus className="size-3.5 shrink-0" aria-hidden="true" />;
    let tone = "text-slate-500 dark:text-slate-400";
    let text: string;
    let canRetry = false;

    switch (n.email_status) {
        case "sent":
            icon = <MailCheck className="size-3.5 shrink-0" aria-hidden="true" />;
            tone = "text-teal-700 dark:text-teal-400";
            text = `Emailed to ${n.recipient_email ?? n.recipient_name ?? "the assignee"}`;
            break;
        case "pending":
        case "sending":
            icon = <Loader2 className="size-3.5 shrink-0 animate-spin" aria-hidden="true" />;
            text = n.email_attempts > 0 ? `Retrying email (attempt ${n.email_attempts + 1})…` : "Sending email…";
            break;
        case "failed":
            icon = <MailX className="size-3.5 shrink-0" aria-hidden="true" />;
            tone = "text-rose-700 dark:text-rose-400";
            text = n.email_next_attempt_at
                ? `Email failed, retrying automatically: ${n.email_detail}`
                : `Email failed after ${n.email_attempts} tries: ${n.email_detail}`;
            canRetry = true;
            break;
        default:
            text = `Not emailed: ${n.email_detail}`;
            canRetry = true;
    }

    return (
        <div className={cx("mt-2 ml-11 flex items-start gap-1.5 text-xs", tone)}>
            {icon}
            <span className="min-w-0 flex-1 break-words">{text}</span>
            {canRetry && (
                <button
                    type="button"
                    onClick={onRetry}
                    disabled={retrying}
                    className="inline-flex shrink-0 items-center gap-1 rounded-md px-1.5 py-0.5 font-medium text-brand-600 hover:bg-brand-50 disabled:opacity-50 dark:text-brand-400 dark:hover:bg-brand-950/60"
                >
                    <RotateCw className={cx("size-3", retrying && "animate-spin")} aria-hidden="true" />
                    Retry
                </button>
            )}
        </div>
    );
}
