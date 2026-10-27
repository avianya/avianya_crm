import type { ReactNode } from "react";
import { ShieldAlert } from "lucide-react";

import { ROLE_LABELS, useAuth } from "../context/auth";
import type { Role } from "../lib/types";
import { Card, EmptyState } from "./ui";

/**
 * Shows its children only to the given roles. The server refuses the same
 * requests to anyone else; this only saves them from a page of errors.
 */
export function RoleGate({ roles, what, children }: { roles: Role[]; what: string; children: ReactNode }) {
    const { is } = useAuth();
    if (is(...roles)) return <>{children}</>;
    return (
        <Card>
            <EmptyState
                icon={ShieldAlert}
                title="Not available to your role"
                description={`${what} is for ${roles.map((r) => ROLE_LABELS[r]).join(" or ")} users.`}
            />
        </Card>
    );
}
