import { defineConfig } from "vite";
import type { PluginOption } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import { isScenario, mockApi } from "./mock/plugin.ts";

// Builds into ../backend/views/web/page/dist, which backend/views/web/server.go embeds into the houridle binary.
//
// `npm run dev` serves the page with hot reload. By default /api answers
// with test data (mock/), so no houridle is needed; MOCK picks a scenario
// (panel, status, empty, offline, unauthorized). With HOURIDLE_URL set,
// /api goes to that running houridle instead, signed in with its key from
// HOURIDLE_KEY (see `houridle web`).
export default defineConfig(({ command }) => {
  const live = process.env.HOURIDLE_URL;
  const plugins: PluginOption[] = [react(), tailwindcss()];
  if (command === "serve") {
    if (live) {
      console.log(`houridle: live data from ${live}`);
    } else {
      const scenario = isScenario(process.env.MOCK) ? process.env.MOCK : "panel";
      if (process.env.MOCK && process.env.MOCK !== scenario) console.log(`houridle: MOCK=${process.env.MOCK} isn't a scenario (panel, status, empty, offline, unauthorized)`);
      console.log(`houridle: test data, scenario "${scenario}" (MOCK=… or ?mock=… to change; HOURIDLE_URL=… for a real houridle)`);
      plugins.push(mockApi(scenario));
    }
  }
  return {
    plugins,
    build: {
      outDir: "../backend/views/web/page/dist",
      emptyOutDir: true,
      assetsInlineLimit: 0, // keep everything in files: the page allows no inline code
      modulePreload: { polyfill: false },
    },
    server: {
      // An explicit address, not "localhost": that can resolve to ::1 while
      // something else holds 127.0.0.1 on the same port, and browsers that
      // try 127.0.0.1 first would hang. Taken ports move to the next one.
      host: "127.0.0.1",
      proxy: live
        ? {
            "/api": {
              target: live,
              headers: { Cookie: `houridle_key=${process.env.HOURIDLE_KEY || ""}` },
            },
          }
        : undefined,
    },
    preview: { host: "127.0.0.1" },
  };
});
