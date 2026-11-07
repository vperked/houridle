import { createContext, useCallback, useContext, useEffect, useRef, useState } from "react";
import type { ReactNode } from "react";
import { errorText } from "../lib/api.ts";
import { buzz, celebrate } from "../lib/fx.ts";
import type { EventKind, StatusEvent } from "../lib/types.ts";

// Notifications: a small stack at the top, each with an icon, a title, an
// optional detail, a close button and a bar counting down to when it goes.
// Hovering (or touching) one keeps it up.

export type Tone = "good" | "info" | "warn" | "bad";

// ShowToast shows a notification and returns its ID.
export type ShowToast = (title: string, tone?: Tone, detail?: string) => number | undefined;

// ToastMessage is a notification an action shows when it's done.
export interface ToastMessage {
  title: string;
  tone?: Tone;
  detail?: string;
  celebrate?: boolean;
}

interface ToastItem {
  id: number;
  title: string;
  tone: Tone;
  detail: string;
  leaving?: boolean;
}

const ToastContext = createContext<ShowToast>(() => undefined);

const MAX = 4;
// How long a toast takes to go: it fades (animate-toast-out in index.css),
// then the gap closes (the grid row below).
const LEAVE_MS = 300;
const DURATION: Record<Tone, number> = { good: 4000, info: 5000, warn: 7000, bad: 9000 };

const TONES: Record<Tone, { icon: string; bar: string; path: string }> = {
  good: { icon: "text-good", bar: "bg-good", path: "M5 12.5l4.5 4.5L19 7.5" },
  info: { icon: "text-accent", bar: "bg-accent", path: "M12 11v6M12 7.5v.01" },
  warn: { icon: "text-warn", bar: "bg-warn", path: "M12 8v5M12 16.5v.01" },
  bad: { icon: "text-bad", bar: "bg-bad", path: "M8 8l8 8M16 8l-8 8" },
};

let nextID = 1;

export function ToastProvider({ children }: { children: ReactNode }) {
  const [toasts, setToasts] = useState<ToastItem[]>([]);
  // A dismissed toast fades away while the ones below close up the gap,
  // then it's dropped.
  const dismiss = useCallback((id: number) => {
    setToasts((list) => list.map((t) => (t.id === id ? { ...t, leaving: true } : t)));
    setTimeout(() => setToasts((list) => list.filter((t) => t.id !== id)), LEAVE_MS);
  }, []);

  // show(title, tone?, detail?) — tone is "good", "info", "warn" or "bad".
  const show = useCallback((title: string, tone: Tone = "good", detail = "") => {
    const t: ToastItem = { id: nextID++, title, tone: TONES[tone] ? tone : "info", detail };
    setToasts((list) => [...list.slice(-(MAX - 1)), t]);
    return t.id;
  }, []);

  return (
    <ToastContext.Provider value={show}>
      {children}
      <div
        aria-live="polite"
        className="pointer-events-none fixed inset-x-0 top-[max(0.75rem,env(safe-area-inset-top))] z-[60] flex flex-col items-center px-4 lg:top-5 lg:items-end lg:px-6"
      >
        {toasts.map((t) => (
          <Toast key={t.id} toast={t} onClose={() => dismiss(t.id)} />
        ))}
      </div>
    </ToastContext.Provider>
  );
}

function Toast({ toast, onClose }: { toast: ToastItem; onClose: () => void }) {
  const tone = TONES[toast.tone];
  const duration = DURATION[toast.tone];
  const [paused, setPaused] = useState(false);
  const left = useRef(duration);
  const started = useRef(Date.now());

  useEffect(() => {
    if (paused || toast.leaving) return;
    started.current = Date.now();
    const t = setTimeout(onClose, left.current);
    return () => {
      clearTimeout(t);
      left.current -= Date.now() - started.current;
    };
  }, [paused, toast.leaving, onClose]);

  return (
    // The grid row shrinks to nothing as the toast leaves, so the stack
    // closes up smoothly instead of jumping.
    <div className={`grid w-full max-w-sm transition-[grid-template-rows] delay-75 duration-200 ease-in-out ${toast.leaving ? "grid-rows-[0fr]" : "grid-rows-[1fr]"}`}>
      <div className="min-h-0">
        <div
          role={toast.tone === "bad" ? "alert" : "status"}
          onMouseEnter={() => setPaused(true)}
          onMouseLeave={() => setPaused(false)}
          onTouchStart={() => setPaused(true)}
          onTouchEnd={() => setPaused(false)}
          className={`${toast.leaving ? "animate-toast-out pointer-events-none" : "animate-toast pointer-events-auto"} relative mb-2 w-full overflow-hidden rounded-2xl border border-line bg-surface/95 shadow-[0_10px_30px_-10px_rgb(0_0_0/0.35)] backdrop-blur-xl`}
        >
          <div className="flex items-start gap-3 p-3.5 pr-10">
            <span className={`mt-px grid size-5 shrink-0 place-items-center ${tone.icon}`}>
              <svg viewBox="0 0 24 24" className="size-4" fill="none" stroke="currentColor" strokeWidth="2.6" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
                <path d={tone.path} />
              </svg>
            </span>
            <div className="min-w-0">
              <p className="text-[15px] font-semibold">{toast.title}</p>
              {toast.detail && <p className="sub mt-0.5 text-[14px] break-words">{toast.detail}</p>}
            </div>
          </div>
          <button
            type="button"
            onClick={onClose}
            aria-label="Dismiss"
            className="faint absolute top-2.5 right-2.5 grid size-7 place-items-center rounded-full active:bg-fill"
          >
            <svg viewBox="0 0 24 24" className="size-4" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" aria-hidden="true">
              <path d="M7 7l10 10M17 7L7 17" />
            </svg>
          </button>
          <span
            aria-hidden="true"
            className={`absolute bottom-0 left-0 h-0.5 w-full origin-left animate-countdown opacity-60 ${tone.bar}`}
            style={{ animationDuration: `${duration}ms`, animationPlayState: paused || toast.leaving ? "paused" : "running" }}
          />
        </div>
      </div>
    </div>
  );
}

export function useToast(): ShowToast {
  return useContext(ToastContext);
}

// useAction wraps an async action: busy while it runs, a notification when
// it's done (done: text, or a function of the result) or when it fails.
export type Done<T> = string | ToastMessage | null | ((out: T) => string | ToastMessage | null | undefined);
export type RunAction = <T>(name: string, fn: () => T | Promise<T>, done?: Done<T>) => Promise<T | undefined>;

export function useAction(): [busy: string | null, run: RunAction] {
  const toast = useToast();
  const [busy, setBusy] = useState<string | null>(null);
  const run: RunAction = useCallback(
    async <T,>(name: string, fn: () => T | Promise<T>, done?: Done<T>) => {
      setBusy(name);
      try {
        const out = await fn();
        if (done) {
          const msg = typeof done === "function" ? done(out) : done;
          if (msg) typeof msg === "string" ? toast(msg, "good") : toast(msg.title, msg.tone ?? "good", msg.detail);
          if (msg && typeof msg !== "string" && msg.celebrate) celebrate();
        }
        return out;
      } catch (e) {
        toast("Couldn't do that", "bad", errorText(e) || "Something went wrong");
        buzz([30, 60, 30]);
        return undefined;
      } finally {
        setBusy(null);
      }
    },
    [toast],
  );
  return [busy, run];
}

// useEventNotifications pops up houridle's own events (signed in, problems,
// free games added) as they happen while the page is open. Events from
// before the page was opened aren't repeated.
export function useEventNotifications(events: StatusEvent[] | undefined): void {
  const toast = useToast();
  const seen = useRef<string | null>(null); // newest event time already shown
  useEffect(() => {
    if (!events?.length) return;
    const newest = events[events.length - 1].at;
    if (seen.current === null) {
      seen.current = newest; // first load: just remember where we are
      return;
    }
    const last = seen.current;
    const fresh = events.filter((e) => e.at > last);
    seen.current = newest;
    for (const e of fresh.slice(-3)) {
      if (e.level === "info") continue; // routine; it's in the activity list
      toast(EVENT_TITLE[e.level], e.level, e.text);
      if (CELEBRATE.has(e.kind)) celebrate();
    }
  }, [events, toast]);
}

// Events worth a burst of confetti, by kind.
const CELEBRATE = new Set<EventKind | undefined>(["drop", "craft", "all-unlocked"]);

const EVENT_TITLE: Record<Exclude<Tone, "info">, string> = { good: "houridle", warn: "Heads up", bad: "Problem" };
