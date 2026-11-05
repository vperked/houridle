import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

// Builds into ../backend/web/dist, which backend/web/server.go embeds into the houridle binary.
// `npm run dev` serves the page with hot reload and sends /api to a running
// houridle: HOURIDLE_URL (default http://127.0.0.1:8095), signed in with its
// key from HOURIDLE_KEY (see `houridle web`).
export default defineConfig({
  plugins: [react(), tailwindcss()],
  build: {
    outDir: "../backend/web/dist",
    emptyOutDir: true,
    assetsInlineLimit: 0, // keep everything in files: the page allows no inline code
    modulePreload: { polyfill: false },
  },
  server: {
    proxy: {
      "/api": {
        target: process.env.HOURIDLE_URL || "http://127.0.0.1:8095",
        headers: { Cookie: `houridle_key=${process.env.HOURIDLE_KEY || ""}` },
      },
    },
  },
});
