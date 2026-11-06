// A pretend houridle for `npm run dev`: answers the page's /api requests
// with test data, so the page can be worked on without a real one. It's a
// dev-server plugin only (apply: "serve"); nothing here reaches the build.
import type { IncomingMessage, ServerResponse } from "node:http";
import type { Plugin } from "vite";
import { HttpError, routes } from "./routes.ts";
import type { Body } from "./routes.ts";
import { SCENARIOS, createState } from "./state.ts";
import type { MockState, Scenario } from "./state.ts";

const DELAY_MS = 300; // long enough to see the loading screen and busy buttons
const RESTART_MS = 10_000; // after Stop or Quit, the test houridle comes back

export function isScenario(x: string | null | undefined): x is Scenario {
  return SCENARIOS.some((s) => s === x);
}

// mockApi serves /api with test data. The scenario is `fallback` (from
// MOCK), or ?mock=<scenario> on the page's address, read from each request's
// Referer, so scenarios can be switched without a restart.
export function mockApi(fallback: Scenario): Plugin {
  const states = new Map<Scenario, MockState>();
  const stateFor = (scenario: Scenario) => {
    let s = states.get(scenario);
    if (!s) states.set(scenario, (s = createState(scenario)));
    return s;
  };
  return {
    name: "houridle-mock-api",
    apply: "serve",
    configureServer(server) {
      server.middlewares.use((req, res, next) => {
        const url = new URL(req.url ?? "/", "http://localhost");
        if (!url.pathname.startsWith("/api/")) return next();
        let scenario = fallback;
        try {
          const asked = new URL(req.headers.referer ?? "").searchParams.get("mock");
          if (isScenario(asked)) scenario = asked;
        } catch {
          // no Referer: the default scenario
        }
        setTimeout(() => {
          answer(req, res, url, scenario, stateFor).catch((e: unknown) => {
            server.config.logger.error(`mock api: ${req.method} ${url.pathname}: ${e instanceof Error ? e.stack : String(e)}`);
            send(res, 500, { error: "the test houridle broke: see the dev server's output" });
          });
        }, DELAY_MS);
        // Stop and Quit end the test houridle for a moment, then it's back.
        res.on("finish", () => {
          const s = states.get(scenario);
          if (s?.stopped && !s.restarting) {
            s.restarting = true;
            setTimeout(() => states.delete(scenario), RESTART_MS);
          }
        });
      });
    },
  };
}

function send(res: ServerResponse, code: number, body?: unknown): void {
  res.statusCode = code;
  res.setHeader("Cache-Control", "no-store");
  if (body === undefined) {
    res.end();
  } else if (typeof body === "string") {
    res.setHeader("Content-Type", "text/plain; charset=utf-8");
    res.end(body + "\n");
  } else {
    res.setHeader("Content-Type", "application/json");
    res.end(JSON.stringify(body) + "\n");
  }
}

function readBody(req: IncomingMessage): Promise<string> {
  return new Promise((resolve, reject) => {
    let text = "";
    req.setEncoding("utf8");
    req.on("data", (chunk: string) => (text += chunk));
    req.on("end", () => resolve(text));
    req.on("error", reject);
  });
}

async function answer(req: IncomingMessage, res: ServerResponse, url: URL, scenario: Scenario, stateFor: (s: Scenario) => MockState): Promise<void> {
  if (scenario === "unauthorized") return send(res, 401, "unauthorized");
  const s = stateFor(scenario);
  if (scenario === "offline" || s.stopped) {
    // houridle isn't there: the page shows it can't reach it.
    req.socket.destroy();
    return;
  }
  const method = req.method ?? "GET";
  const handler = routes[`${method} ${url.pathname}`];
  if (!handler) {
    const other = Object.keys(routes).some((k) => k.endsWith(` ${url.pathname}`));
    return send(res, other ? 405 : 404, other ? "Method Not Allowed" : "404 page not found");
  }
  let body: Body = {};
  if (method === "POST") {
    // Like the real one: changes need the page's own header.
    const h = req.headers["x-houridle"];
    if (h !== "1" && h !== "stop") return send(res, 403, "missing X-Houridle header");
    const text = await readBody(req);
    if (text.trim()) {
      let parsed: unknown;
      try {
        parsed = JSON.parse(text);
      } catch (e) {
        return send(res, 400, { error: `couldn't read the request: ${e instanceof Error ? e.message : ""}` });
      }
      if (parsed && typeof parsed === "object" && !Array.isArray(parsed)) body = Object.fromEntries(Object.entries(parsed));
    }
  }
  try {
    const out = handler({ s, body, query: url.searchParams });
    if (url.pathname === "/api/stop") return send(res, 202);
    send(res, 200, out === undefined ? { ok: true } : out); // null stays null, like a nil slice
  } catch (e) {
    if (e instanceof HttpError) return send(res, e.code, { error: e.message });
    throw e;
  }
}
