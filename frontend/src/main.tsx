import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { BrowserRouter } from "react-router-dom";

import App from "./App";
import { ThemeProvider } from "./context/ThemeContext";
import { ToastProvider } from "./context/ToastContext";
import { AuthProvider } from "./context/AuthContext";
import { PublicPage } from "./pages/PublicPages";
import "./index.css";

/**
 * Application entry point.
 *
 * The provider order matters, outermost first:
 *
 *   StrictMode   -- development-only double-rendering that surfaces impure
 *                   renders and missing effect cleanups. It is why useAsync
 *                   aborts its requests properly.
 *   ThemeProvider -- owns the `dark` class on <html>, so it wraps everything.
 *   BrowserRouter -- provides routing context; every Link and useParams below
 *                    depends on it.
 *   ToastProvider -- inside the router, so a toast can be raised from any page.
 *   AuthProvider -- shows the login until someone is logged in.
 */
const rootElement = document.getElementById("root");

if (!rootElement) {
    // A clear failure beats React's terser error if index.html ever loses #root.
    throw new Error('Could not find the #root element to mount into.');
}

// A booking or thank-you link a lead opened: shown on its own, with no login.
const publicPath = /^\/(booking|feedback)\/[^/]+\/?$/.test(window.location.pathname)
    ? window.location.pathname
    : null;

createRoot(rootElement).render(
    <StrictMode>
        <ThemeProvider>
            {publicPath ? (
                <PublicPage path={publicPath} />
            ) : (
                <BrowserRouter>
                    <ToastProvider>
                        <AuthProvider>
                            <App />
                        </AuthProvider>
                    </ToastProvider>
                </BrowserRouter>
            )}
        </ThemeProvider>
    </StrictMode>,
);
