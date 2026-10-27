/**
 * Auth context, types and the `useAuth` hook.
 *
 * Split from AuthContext.tsx like ./toast.ts, so that file exports only
 * components and React Fast Refresh keeps working.
 */

import { createContext, useContext } from "react";

import type { Role, Viewer } from "../lib/types";

export interface AuthContextValue {
    /** Who is logged in. The session itself is an HttpOnly cookie the page never sees. */
    user: Viewer;
    logout: () => Promise<void>;
    /** True for these roles. The server checks the role too; this only hides what would be refused. */
    is: (...roles: Role[]) => boolean;
}

export const AuthContext = createContext<AuthContextValue | null>(null);

export function useAuth(): AuthContextValue {
    const value = useContext(AuthContext);
    if (!value) throw new Error("useAuth must be used inside <AuthProvider>");
    return value;
}

export const ROLE_LABELS: Record<Role, string> = {
    admin: "Admin",
    manager: "Manager",
    sales: "Sales",
};
