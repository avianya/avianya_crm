import { Link, Route, Routes, useParams } from "react-router-dom";
import { Compass } from "lucide-react";

import { AppShell } from "./components/AppShell";
import { AccountPage } from "./pages/AccountPage";
import { AutomationsPage } from "./pages/AutomationsPage";
import { DashboardPage } from "./pages/DashboardPage";
import { DealsPage } from "./pages/DealsPage";
import { FollowUpsPage } from "./pages/FollowUpsPage";
import { IntegrationsPage } from "./pages/IntegrationsPage";
import { LeadDetailPage } from "./pages/LeadDetailPage";
import { LeadsPage } from "./pages/LeadsPage";
import { MetaCapiPage } from "./pages/MetaCapiPage";
import { MetaLeadsPage } from "./pages/MetaLeadsPage";
import { PipelinePage } from "./pages/PipelinePage";
import { ReportsPage } from "./pages/ReportsPage";
import { SalesIntelligencePage } from "./pages/SalesIntelligencePage";
import { SchedulePage } from "./pages/SchedulePage";
import { TasksPage } from "./pages/TasksPage";
import { TeamPage } from "./pages/TeamPage";
import { TemplatesPage } from "./pages/TemplatesPage";
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
                <Route path="/follow-ups" element={<FollowUpsPage />} />
                <Route path="/leads" element={<LeadsPage />} />
                <Route path="/leads/:id" element={<LeadDetailRoute />} />
                <Route path="/deals" element={<DealsPage />} />
                <Route path="/pipeline" element={<PipelinePage />} />
                <Route path="/sales-intelligence" element={<SalesIntelligencePage />} />
                <Route path="/tasks" element={<TasksPage />} />
                <Route path="/schedule" element={<SchedulePage />} />
                <Route path="/reports" element={<ReportsPage />} />
                <Route path="/templates" element={<TemplatesPage />} />
                <Route path="/automations" element={<AutomationsPage />} />
                <Route path="/integrations" element={<IntegrationsPage />} />
                <Route path="/integrations/meta-capi" element={<MetaCapiPage />} />
                <Route path="/meta-leads" element={<MetaLeadsPage />} />
                <Route path="/team" element={<TeamPage />} />
                <Route path="/account" element={<AccountPage />} />

                {/* "*" catches anything unmatched. Without it, a typo'd URL
                    renders the shell around a blank page, which looks like a bug. */}
                <Route path="*" element={<NotFoundPage />} />
            </Route>
        </Routes>
    );
}

/**
 * Keys the detail page by its id, so going from one lead straight to another
 * mounts a fresh page instead of showing the old lead until the new one loads.
 */
function LeadDetailRoute() {
    const { id } = useParams<{ id: string }>();
    return <LeadDetailPage key={id} />;
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
