// Calls to houridle's control API. Changes carry the page's own header,
// which a page on another site can't send.
import type { OK } from "./types.ts";

async function request<T>(method: "GET" | "POST", path: string, body?: unknown): Promise<T> {
  const r = await fetch(path, {
    method,
    cache: "no-store",
    headers: method === "GET" ? {} : { "Content-Type": "application/json", "X-Houridle": "1" },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  let data: unknown = null;
  try {
    data = await r.json();
  } catch {
    // not JSON
  }
  if (!r.ok) {
    if (r.status === 401) throw new Error("This page's key is missing or old. Open the link from houridle web again.");
    throw new Error(errorField(data) || `houridle answered HTTP ${r.status}`);
  }
  return data as T; // the shape is the endpoint's: see types.ts
}

// errorField is the "error" houridle answers a failed request with.
function errorField(data: unknown): string {
  return data && typeof data === "object" && "error" in data && typeof data.error === "string" ? data.error : "";
}

// errorText is what went wrong, for showing (an Error's message; "" for
// anything else thrown).
export function errorText(e: unknown): string {
  return e instanceof Error ? e.message : "";
}

export const api = {
  get: <T,>(path: string) => request<T>("GET", path),
  post: <T = OK,>(path: string, body: unknown = {}) => request<T>("POST", path, body),
};
