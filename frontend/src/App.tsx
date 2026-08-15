import { Link, Route, Routes } from "react-router-dom";
import { Compass } from "lucide-react";

import { AppShell } from "./components/AppShell";
import { DashboardPage } from "./pages/DashboardPage";
import { LeadDetailPage } from "./pages/LeadDetailPage";
import { LeadsPage } from "./pages/LeadsPage";
import { Card, EmptyState } from "./components/ui";

/**
 * The route table.
 *
 * AppShell is a "layout route": it renders the sidebar, header and an <Outlet/>,
 * and the nested routes render into that outlet. So the chrome is declared once
 * and every page inherits it.
 */
export default function App() {
    return (
        <Routes>
            <Route element={<AppShell />}>
                {/* index = the parent path exactly, i.e. "/" */}
                <Route index element={<DashboardPage />} />
                <Route path="/leads" element={<LeadsPage />} />
                <Route path="/leads/:id" element={<LeadDetailPage />} />

                {/* "*" catches anything unmatched. Without it, a typo'd URL
                    renders the shell around a blank page, which looks like a bug. */}
                <Route path="*" element={<NotFoundPage />} />
            </Route>
        </Routes>
    );
}

function NotFoundPage() {
    return (
        <Card>
            <EmptyState
                icon={Compass}
                title="Page not found"
                description="That URL does not match anything in this app."
                action={
                    // Styled as a button but rendered as a link, because it
                    // navigates. Nesting an <a> inside a <button> would be invalid
                    // markup and confuses both keyboards and screen readers.
                    <Link
                        to="/"
                        className="inline-flex h-10 items-center justify-center rounded-lg bg-brand-600 px-4 text-sm font-medium text-white shadow-sm transition-colors hover:bg-brand-700 dark:bg-brand-500 dark:hover:bg-brand-400"
                    >
                        Go to the dashboard
                    </Link>
                }
            />
        </Card>
    );
}
