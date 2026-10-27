import { useState } from "react";
import { Link } from "react-router-dom";
import { CalendarCheck, Check, Copy, MessageCircle, Star } from "lucide-react";

import { ApiError, api } from "../lib/api";
import { cx, formatRelative } from "../lib/display";
import type { CustomerLink, Lead } from "../lib/types";
import { useAsync } from "../hooks/useAsync";
import { useToast } from "../context/toast";
import { Button, Card, CardHeader, Skeleton } from "./ui";

/**
 * Links to send the lead: a booking page to pick a meeting time, and, once
 * the deal is won, a thank-you page to rate it and refer a friend. Neither
 * needs the lead to log in.
 */
export function CustomerLinksCard({ lead }: { lead: Lead }) {
    const toast = useToast();
    // Refetched when the stage changes: winning the lead makes its thank-you link.
    const links = useAsync((signal) => api.customerLinks(lead.id, signal), [lead.id, lead.stage]);
    const [creating, setCreating] = useState<"booking" | "feedback" | null>(null);

    const create = async (kind: "booking" | "feedback") => {
        setCreating(kind);
        try {
            links.setData(await api.createCustomerLink(lead.id, kind));
        } catch (cause) {
            toast.error(cause instanceof ApiError ? cause.message : "Could not make the link.");
        } finally {
            setCreating(null);
        }
    };

    const firstName = lead.name.split(" ")[0];
    const data = links.data;

    return (
        <Card>
            <CardHeader title="Customer links" subtitle={`Send these to ${firstName}. No login needed.`} />
            {links.loading && !data ? (
                <div className="space-y-3 px-5 py-4">
                    <Skeleton className="h-10 w-full" />
                    <Skeleton className="h-10 w-full" />
                </div>
            ) : links.error ? (
                <p className="px-5 py-4 text-sm text-rose-600 dark:text-rose-400">{links.error.message}</p>
            ) : (
                <div className="divide-y divide-slate-100 dark:divide-slate-800">
                    <section className="space-y-2 px-5 py-4">
                        <h3 className="flex items-center gap-1.5 text-xs font-semibold text-slate-700 dark:text-slate-200">
                            <CalendarCheck className="size-3.5" aria-hidden="true" />
                            Book a meeting
                        </h3>
                        {lead.owner_id === null ? (
                            <p className="text-xs text-slate-500 dark:text-slate-400">
                                Assign this lead to someone first: the meeting goes in their calendar.
                            </p>
                        ) : data?.booking ? (
                            <LinkRow
                                link={data.booking}
                                phone={lead.phone}
                                message={`Hi ${firstName}, pick a time that suits you for a quick call: ${data.booking.url}`}
                            />
                        ) : (
                            <>
                                <p className="text-xs text-slate-500 dark:text-slate-400">
                                    {firstName} picks a free 30-minute slot, and it goes straight into the calendar.
                                </p>
                                <Button size="sm" loading={creating === "booking"} onClick={() => create("booking")}>
                                    Make booking link
                                </Button>
                            </>
                        )}
                    </section>

                    <section className="space-y-2 px-5 py-4">
                        <h3 className="flex items-center gap-1.5 text-xs font-semibold text-slate-700 dark:text-slate-200">
                            <Star className="size-3.5" aria-hidden="true" />
                            Thank you and feedback
                        </h3>
                        {data?.given_feedback ? (
                            <div className="space-y-1">
                                <Stars rating={data.given_feedback.rating} />
                                {data.given_feedback.comment && (
                                    <p className="text-sm text-slate-700 dark:text-slate-200">
                                        “{data.given_feedback.comment}”
                                    </p>
                                )}
                                <p className="text-xs text-slate-500 dark:text-slate-400">
                                    Given {formatRelative(data.given_feedback.created_at)}
                                    {data.given_feedback.referral_lead_id !== null && (
                                        <>
                                            {" · "}
                                            <Link
                                                to={`/leads/${data.given_feedback.referral_lead_id}`}
                                                className="text-brand-600 hover:underline dark:text-brand-400"
                                            >
                                                see the friend they referred
                                            </Link>
                                        </>
                                    )}
                                </p>
                            </div>
                        ) : data?.feedback ? (
                            <>
                                <LinkRow
                                    link={data.feedback}
                                    phone={lead.phone}
                                    message={`Hi ${firstName}, thank you for choosing us! Could you tell us how we did? It takes a minute: ${data.feedback.url}`}
                                />
                                <p className="text-xs text-slate-500 dark:text-slate-400">
                                    {data.feedback.emailed_at
                                        ? `Emailed to ${firstName} ${formatRelative(data.feedback.emailed_at)}. No feedback yet.`
                                        : "Not emailed (email is not set up on the server). Copy it or send it on WhatsApp."}
                                </p>
                            </>
                        ) : lead.stage === "won" ? (
                            <Button size="sm" loading={creating === "feedback"} onClick={() => create("feedback")}>
                                Make thank-you link
                            </Button>
                        ) : (
                            <p className="text-xs text-slate-500 dark:text-slate-400">
                                When this lead is marked Won, {firstName} gets a thank-you page to rate the deal and
                                refer a friend.
                            </p>
                        )}
                    </section>
                </div>
            )}
        </Card>
    );
}

/** The link, with Copy and Send on WhatsApp. */
function LinkRow({ link, phone, message }: { link: CustomerLink; phone: string | null; message: string }) {
    const [copied, setCopied] = useState(false);
    const copy = async () => {
        try {
            await navigator.clipboard.writeText(link.url);
            setCopied(true);
            window.setTimeout(() => setCopied(false), 2000);
        } catch {
            window.prompt("Copy this link:", link.url);
        }
    };
    // wa.me wants the number with its country code and digits only; a
    // 10-digit number is taken to be Indian.
    const digits = (phone ?? "").replace(/\D/g, "");
    const waNumber = digits.length === 10 ? `91${digits}` : digits;
    const whatsapp = `https://wa.me/${waNumber}?text=${encodeURIComponent(message)}`;

    return (
        <div className="space-y-2">
            <input
                readOnly
                value={link.url}
                onFocus={(e) => e.currentTarget.select()}
                aria-label={`${link.kind === "booking" ? "Booking" : "Thank-you"} link`}
                className="w-full truncate rounded-lg border border-slate-200 bg-slate-50 px-2.5 py-1.5 font-mono text-xs text-slate-700 dark:border-slate-700 dark:bg-slate-950 dark:text-slate-300"
            />
            <div className="flex flex-wrap gap-2">
                <Button size="sm" onClick={copy}>
                    {copied ? <Check className="size-3.5" aria-hidden="true" /> : <Copy className="size-3.5" aria-hidden="true" />}
                    {copied ? "Copied" : "Copy"}
                </Button>
                <a
                    href={whatsapp}
                    target="_blank"
                    rel="noreferrer"
                    className="inline-flex items-center gap-1.5 rounded-lg border border-emerald-200 bg-emerald-50 px-2.5 py-1 text-xs font-medium text-emerald-800 hover:bg-emerald-100 dark:border-emerald-900 dark:bg-emerald-950/40 dark:text-emerald-200"
                >
                    <MessageCircle className="size-3.5" aria-hidden="true" />
                    Send on WhatsApp
                </a>
            </div>
        </div>
    );
}

function Stars({ rating }: { rating: number }) {
    return (
        <span className="flex items-center gap-0.5" aria-label={`${rating} out of 5 stars`}>
            {[1, 2, 3, 4, 5].map((n) => (
                <Star
                    key={n}
                    aria-hidden="true"
                    className={cx("size-4", n <= rating ? "fill-amber-400 text-amber-400" : "text-slate-300 dark:text-slate-600")}
                />
            ))}
        </span>
    );
}
