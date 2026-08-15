import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

// https://vite.dev/config/
export default defineConfig({
    // Tailwind v4 runs as a Vite plugin -- no postcss.config.js and no
    // tailwind.config.js. All theme configuration lives in src/index.css.
    plugins: [react(), tailwindcss()],

    server: {
        port: 5173,
        // Fail loudly if 5173 is taken instead of quietly moving to 5174, which
        // would then be blocked by the backend's CORS allow-list.
        strictPort: true,
    },

    preview: {
        port: 4173,
        strictPort: true,
    },
});
