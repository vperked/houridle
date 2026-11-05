import { useCallback, useEffect, useRef, useState } from "react";

const REFRESH_MS = 5000;

// useStatus polls houridle's /api/status. It pauses while the page is in
// the background (saves the phone's battery) and refreshes on return.
//
// state: "loading" | "ok" | "unauthorized" | "offline" | "stopped"
export function useStatus() {
  const [data, setData] = useState(null);
  const [state, setState] = useState("loading");
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
export function useNow() {
  const [now, setNow] = useState(Date.now());
  useEffect(() => {
    const t = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(t);
  }, []);
  return now;
}
