import { useCallback, useEffect, useRef, useState } from "react";
import { keep } from "../lib/keep.ts";
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
  const busy = useRef<Promise<void> | null>(null);
  const again = useRef<Promise<void> | null>(null);

  // One request at a time: if houridle is slow, polls wait for the answer
  // instead of piling up, and an old answer never lands after a newer one.
  // A refresh asked for meanwhile (say, after a button press) runs once the
  // current one is back, so it sees the change.
  const refresh = useCallback((): Promise<void> => {
    if (busy.current) {
      again.current ??= busy.current.then(() => {
        again.current = null;
        return refresh();
      });
      return again.current;
    }
    busy.current = load().finally(() => (busy.current = null));
    return busy.current;
  }, []); // load only uses refs and setters

  async function load() {
    if (stopped.current) return;
    try {
      const r = await fetch("/api/status", { cache: "no-store" });
      if (r.status === 401) {
        setState("unauthorized");
        return;
      }
      if (!r.ok) throw new Error(`HTTP ${r.status}`);
      const next: Status = await r.json();
      setData((prev) => keep(prev, next));
      setFetchedAt(Date.now());
      setState("ok");
    } catch {
      if (!stopped.current) setState("offline");
    }
  }

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

// useNow re-renders every second, so running clocks tick between refreshes;
// with nothing ticking (fast false), only every 30 s. It rests while the
// page is in the background, and catches up on return.
export function useNow(fast = true): number {
  const [now, setNow] = useState(Date.now());
  useEffect(() => {
    let t = 0;
    const start = () => {
      clearInterval(t);
      if (document.visibilityState !== "visible") return;
      setNow(Date.now());
      t = setInterval(() => setNow(Date.now()), fast ? 1000 : 30_000);
    };
    start();
    document.addEventListener("visibilitychange", start);
    return () => {
      clearInterval(t);
      document.removeEventListener("visibilitychange", start);
    };
  }, [fast]);
  return now;
}
