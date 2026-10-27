import { useState } from "react";
import { Link, NavLink, Outlet } from "react-router-dom";
import {
    BrainCircuit,
    CalendarDays,
    ChartColumn,
    Handshake,
    LayoutDashboard,
    ListTodo,
    LogOut,
    Mail,
    Megaphone,
    Menu,
    Monitor,
    Moon,
    PhoneForwarded,
    Plug,
    SquareKanban,
    Sun,
    Target,
    Users,
    UsersRound,
    X,
    Zap,
} from "lucide-react";

import { useTheme } from "../context/theme";
import { ROLE_LABELS, useAuth } from "../context/auth";
import type { Role } from "../lib/types";
import { NotificationBell } from "./NotificationBell";
import { cx } from "../lib/display";
import type { ThemePreference } from "../context/theme";

/**
 * The persistent frame around every page: navigation, branding, theme toggle.
 *
 * Renders <Outlet/>, which react-router replaces with whichever page matches the
 * current URL -- so the sidebar and header are mounted once and survive
 * navigation instead of being re-rendered per page.
 */
export function AppShell() {
    const [mobileNavOpen, setMobileNavOpen] = useState(false);

    return (
        <div className="min-h-dvh">
            {/* Skip link: the first thing a keyboard or screen-reader user reaches,
                letting them jump past the nav straight to the page content. It is
                invisible until focused. */}
            <a
                href="#main"
                className="sr-only-focusable fixed top-3 left-3 z-100 rounded-lg bg-brand-600 px-3 py-2 text-sm font-medium text-white shadow-lg"
            >
                Skip to content
            </a>

            {/* --- Desktop sidebar (lg and up) --- */}
            <aside className="fixed inset-y-0 left-0 z-30 hidden w-60 border-r border-slate-200 bg-white lg:flex lg:flex-col dark:border-slate-800 dark:bg-slate-900">
                <Brand />
                <NavLinks />
                <SidebarFooter />
            </aside>

            {/* --- Mobile drawer --- */}
            {mobileNavOpen && (
                <div className="fixed inset-0 z-40 lg:hidden">
                    <div
                        className="absolute inset-0 bg-slate-900/40 backdrop-blur-sm animate-fade-in"
                        onClick={() => setMobileNavOpen(false)}
                        aria-hidden="true"
                    />
                    <aside className="relative flex h-full w-64 flex-col border-r border-slate-200 bg-white animate-slide-in-right dark:border-slate-800 dark:bg-slate-900">
                        <div className="flex items-center justify-between pr-2">
                            <Brand />
                            <button
                                type="button"
                                onClick={() => setMobileNavOpen(false)}
                                className="rounded-lg p-2 text-slate-500 hover:bg-slate-100 dark:hover:bg-slate-800"
                                aria-label="Close navigation"
                            >
                                <X className="size-5" aria-hidden="true" />
                            </button>
                        </div>
                        {/* Closing on navigate: leaving the drawer open over the
                            page you just navigated to would hide it. */}
                        <NavLinks onNavigate={() => setMobileNavOpen(false)} />
                        <SidebarFooter />
                    </aside>
                </div>
            )}

            {/* --- Main column --- */}
            <div className="lg:pl-60">
                {/* Sticky header: reminders and the theme toggle; on mobile it
                    also holds the hamburger. */}
                <header className="sticky top-0 z-20 flex h-14 items-center justify-between gap-3 border-b border-slate-200 bg-slate-50/85 px-4 backdrop-blur-md sm:px-6 dark:border-slate-800 dark:bg-slate-950/85">
                    <button
                        type="button"
                        onClick={() => setMobileNavOpen(true)}
                        className="-ml-1 rounded-lg p-2 text-slate-600 hover:bg-slate-200/60 lg:hidden dark:text-slate-300 dark:hover:bg-slate-800"
                        aria-label="Open navigation"
                    >
                        <Menu className="size-5" aria-hidden="true" />
                    </button>

                    <span className="text-sm font-medium text-slate-400 lg:hidden dark:text-slate-500">
                        Pipeline CRM
                    </span>

                    <div className="ml-auto flex items-center gap-2">
                        <NotificationBell />
                        <ThemeToggle />
                        <UserMenu />
                    </div>
                </header>

                <main id="main" className="px-4 py-6 sm:px-6 lg:px-8 lg:py-8">
                    <div className="mx-auto max-w-7xl">
                        <Outlet />
                    </div>
                </main>
            </div>
        </div>
    );
}

function Brand() {
    return (
        <div className="flex h-14 items-center gap-2.5 px-5">
            <div className="flex size-8 items-center justify-center rounded-lg bg-brand-600 text-white shadow-sm">
                <Target className="size-4.5" aria-hidden="true" />
            </div>
            <div className="leading-tight">
                <p className="text-sm font-semibold text-slate-900 dark:text-slate-50">
                    Pipeline
                </p>
                <p className="text-[10px] font-medium tracking-wide text-slate-400 uppercase dark:text-slate-500">
                    Lead CRM
                </p>
            </div>
        </div>
    );
}

const ADMIN: Role[] = ["admin"];

/** roles: only shown to these (the server refuses the rest anyway). */
const NAV_ITEMS: { to: string; label: string; icon: typeof Users; end: boolean; roles?: Role[] }[] = [
    { to: "/", label: "Dashboard", icon: LayoutDashboard, end: true },
    { to: "/follow-ups", label: "Follow-ups", icon: PhoneForwarded, end: false },
    { to: "/leads", label: "Leads", icon: Users, end: false },
    { to: "/meta-leads", label: "Meta Leads", icon: Megaphone, end: false },
    { to: "/deals", label: "Deals", icon: Handshake, end: false },
    { to: "/pipeline", label: "Pipeline", icon: SquareKanban, end: false },
    { to: "/sales-intelligence", label: "Intelligence", icon: BrainCircuit, end: false },
    { to: "/tasks", label: "Tasks", icon: ListTodo, end: false },
    { to: "/schedule", label: "Schedule", icon: CalendarDays, end: false },
    { to: "/reports", label: "Reports", icon: ChartColumn, end: false },
    { to: "/templates", label: "Templates", icon: Mail, end: false },
    { to: "/automations", label: "Automations", icon: Zap, end: false, roles: ADMIN },
    { to: "/integrations", label: "Integrations", icon: Plug, end: false, roles: ADMIN },
    { to: "/team", label: "Team", icon: UsersRound, end: false },
];

function NavLinks({ onNavigate }: { onNavigate?: () => void }) {
    const { is } = useAuth();
    return (
        <nav className="flex-1 space-y-1 px-3 py-4" aria-label="Main">
            {NAV_ITEMS.filter((item) => !item.roles || is(...item.roles)).map((item) => (
                <NavLink
                    key={item.to}
                    to={item.to}
                    // `end` stops "/" from matching every route -- without it the
                    // Dashboard link would stay highlighted on /leads too.
                    end={item.end}
                    onClick={onNavigate}
                    // NavLink hands the render function the active state, which is
                    // how the current page gets highlighted without any manual
                    // location comparison.
                    className={({ isActive }) =>
                        cx(
                            "flex items-center gap-3 rounded-lg px-3 py-2 text-sm font-medium transition-colors",
                            isActive
                                ? "bg-brand-50 text-brand-700 dark:bg-brand-950/60 dark:text-brand-300"
                                : "text-slate-600 hover:bg-slate-100 hover:text-slate-900 dark:text-slate-400 dark:hover:bg-slate-800 dark:hover:text-slate-100",
                        )
                    }
                >
                    <item.icon className="size-4.5 shrink-0" aria-hidden="true" />
                    {item.label}
                </NavLink>
            ))}
        </nav>
    );
}

function SidebarFooter() {
    const { user, is } = useAuth();
    return (
        <div className="border-t border-slate-200 px-5 py-4 dark:border-slate-800">
            <p className="text-[11px] leading-relaxed text-slate-400 dark:text-slate-500">
                Logged in as {ROLE_LABELS[user.role]}
                <br />
                {is("sales") ? "You see the leads assigned to you." : "You see every lead."}
            </p>
        </div>
    );
}

/** Who is logged in, and the way out. */
function UserMenu() {
    const { user, logout } = useAuth();
    return (
        <div className="flex items-center gap-2 border-l border-slate-200 pl-3 dark:border-slate-800">
            <Link to="/account" className="hidden text-right leading-tight hover:opacity-80 sm:block" title="My account">
                <p className="text-xs font-medium text-slate-800 dark:text-slate-100">{user.name}</p>
                <p className="text-[11px] text-slate-500 dark:text-slate-400">{ROLE_LABELS[user.role]}</p>
            </Link>
            <button
                type="button"
                onClick={() => void logout()}
                className="flex size-9 items-center justify-center rounded-lg text-slate-500 transition-colors hover:bg-slate-100 hover:text-slate-900 dark:text-slate-400 dark:hover:bg-slate-800 dark:hover:text-slate-100"
                aria-label="Log out"
                title="Log out"
            >
                <LogOut className="size-4.5" aria-hidden="true" />
            </button>
        </div>
    );
}

/**
 * Cycles light → dark → system.
 *
 * A three-way control rather than a switch, because "follow my OS" is a real
 * preference and a two-state toggle cannot express it. The icon shows the current
 * choice, and the tooltip names what a click will do next.
 */
function ThemeToggle() {
    const { preference, setPreference } = useTheme();

    const options: { value: ThemePreference; icon: typeof Sun; label: string }[] = [
        { value: "light", icon: Sun, label: "Light" },
        { value: "dark", icon: Moon, label: "Dark" },
        { value: "system", icon: Monitor, label: "System" },
    ];

    const currentIndex = options.findIndex((option) => option.value === preference);
    const current = options[currentIndex] ?? options[2];
    const next = options[(currentIndex + 1) % options.length];
    const Icon = current.icon;

    return (
        <button
            type="button"
            onClick={() => setPreference(next.value)}
            title={`Theme: ${current.label}. Switch to ${next.label}.`}
            aria-label={`Theme: ${current.label}. Switch to ${next.label}.`}
            className="inline-flex items-center gap-2 rounded-lg border border-slate-200 bg-white px-2.5 py-1.5 text-xs font-medium text-slate-600 shadow-sm transition-colors hover:bg-slate-50 dark:border-slate-700 dark:bg-slate-900 dark:text-slate-300 dark:hover:bg-slate-800"
        >
            <Icon className="size-4" aria-hidden="true" />
            <span className="hidden sm:inline">{current.label}</span>
        </button>
    );
}
