import { useCallback, useEffect, useRef, useState } from "react";
import type { Status } from "../lib/types.ts";

const REFRESH_MS = 5000;

// useStatus polls houridle's /api/status. It pauses while the page is in
// the background (saves the phone's battery) and refreshes on return.
//
// state: "loading" | "ok" | "unauthorized" | "offline" | "stopped"
export type LoadState = "loading" | "ok" | "unauthorized" | "offline" | "stopped";

export interface StatusHook {
  data: Status | null;
  state: LoadState;
  fetchedAt: number;
  refresh: () => Promise<void>;
  markStopped: () => void;
}

export function useStatus(): StatusHook {
  const [data, setData] = useState<Status | null>(null);
  const [state, setState] = useState<LoadState>("loading");
  const [fetchedAt, setFetchedAt] = useState(0);
  const stopped = useRef(false);

  const refresh = useCallback(async () => {
    if (stopped.current) return;
    try {
      const r = await fetch("/api/status", { cache: "no-store" });
      if (r.status === 401) {
        setState("unauthorized");
        return;
      }
      if (!r.ok) throw new Error(`HTTP ${r.status}`);
      setData(await r.json());
      setFetchedAt(Date.now());
      setState("ok");
    } catch {
      if (!stopped.current) setState("offline");
    }
  }, []);

  useEffect(() => {
    refresh();
    let timer = setInterval(refresh, REFRESH_MS);
    const onVisible = () => {
      clearInterval(timer);
      if (document.visibilityState === "visible") {
        refresh();
        timer = setInterval(refresh, REFRESH_MS);
      }
    };
    document.addEventListener("visibilitychange", onVisible);
    return () => {
      clearInterval(timer);
      document.removeEventListener("visibilitychange", onVisible);
    };
  }, [refresh]);

  const markStopped = useCallback(() => {
    stopped.current = true;
    setState("stopped");
  }, []);

  return { data, state, fetchedAt, refresh, markStopped };
}

// useNow re-renders every second, so running clocks tick between refreshes.
// It rests while the page is in the background, and catches up on return.
export function useNow(): number {
  const [now, setNow] = useState(Date.now());
  useEffect(() => {
    let t = 0;
    const start = () => {
      clearInterval(t);
      if (document.visibilityState !== "visible") return;
      setNow(Date.now());
      t = setInterval(() => setNow(Date.now()), 1000);
    };
    start();
    document.addEventListener("visibilitychange", start);
    return () => {
      clearInterval(t);
      document.removeEventListener("visibilitychange", start);
    };
  }, []);
  return now;
}
