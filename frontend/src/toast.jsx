import { createContext, useCallback, useContext, useEffect, useRef, useState } from "react";
import { buzz, celebrate } from "./fx.js";

// Notifications: a small stack at the top, each with an icon, a title, an
// optional detail, a close button and a bar counting down to when it goes.
// Hovering (or touching) one keeps it up.

const ToastContext = createContext(() => {});

const MAX = 4;
const DURATION = { good: 4000, info: 5000, warn: 7000, bad: 9000 };

const TONES = {
  good: { icon: "text-good", bar: "bg-good", path: "M5 12.5l4.5 4.5L19 7.5" },
  info: { icon: "text-accent", bar: "bg-accent", path: "M12 11v6M12 7.5v.01" },
  warn: { icon: "text-warn", bar: "bg-warn", path: "M12 8v5M12 16.5v.01" },
  bad: { icon: "text-bad", bar: "bg-bad", path: "M8 8l8 8M16 8l-8 8" },
};

let nextID = 1;

export function ToastProvider({ children }) {
  const [toasts, setToasts] = useState([]);
  const dismiss = useCallback((id) => setToasts((list) => list.filter((t) => t.id !== id)), []);

  // show(title, tone?, detail?) — tone is "good", "info", "warn" or "bad".
  const show = useCallback((title, tone = "good", detail = "") => {
    const t = { id: nextID++, title, tone: TONES[tone] ? tone : "info", detail };
    setToasts((list) => [...list.slice(-(MAX - 1)), t]);
    return t.id;
  }, []);

  return (
    <ToastContext.Provider value={show}>
      {children}
      <div
        aria-live="polite"
        className="pointer-events-none fixed inset-x-0 top-[max(0.75rem,env(safe-area-inset-top))] z-[60] flex flex-col items-center gap-2 px-4 lg:top-5 lg:items-end lg:px-6"
      >
        {toasts.map((t) => (
          <Toast key={t.id} toast={t} onClose={() => dismiss(t.id)} />
        ))}
      </div>
    </ToastContext.Provider>
  );
}

function Toast({ toast, onClose }) {
  const tone = TONES[toast.tone];
  const duration = DURATION[toast.tone];
  const [paused, setPaused] = useState(false);
  const left = useRef(duration);
  const started = useRef(Date.now());

  useEffect(() => {
    if (paused) return;
    started.current = Date.now();
    const t = setTimeout(onClose, left.current);
    return () => {
      clearTimeout(t);
      left.current -= Date.now() - started.current;
    };
  }, [paused, onClose]);

  return (
    <div
      role={toast.tone === "bad" ? "alert" : "status"}
      onMouseEnter={() => setPaused(true)}
      onMouseLeave={() => setPaused(false)}
      onTouchStart={() => setPaused(true)}
      onTouchEnd={() => setPaused(false)}
      className="animate-toast pointer-events-auto relative w-full max-w-sm overflow-hidden rounded-2xl border border-line bg-surface/95 shadow-[0_10px_30px_-10px_rgb(0_0_0/0.35)] backdrop-blur-xl"
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
        className={`absolute bottom-0 left-0 h-0.5 animate-countdown opacity-60 ${tone.bar}`}
        style={{ animationDuration: `${duration}ms`, animationPlayState: paused ? "paused" : "running" }}
      />
    </div>
  );
}

export function useToast() {
  return useContext(ToastContext);
}

// useAction wraps an async action: busy while it runs, a notification when
// it's done (done: text, or a function of the result) or when it fails.
export function useAction() {
  const toast = useToast();
  const [busy, setBusy] = useState(null);
  const run = useCallback(
    async (name, fn, done) => {
      setBusy(name);
      try {
        const out = await fn();
        if (done) {
          const msg = typeof done === "function" ? done(out) : done;
          if (msg) typeof msg === "string" ? toast(msg, "good") : toast(msg.title, msg.tone ?? "good", msg.detail);
          if (msg?.celebrate) celebrate();
        }
        return out;
      } catch (e) {
        toast("Couldn't do that", "bad", e.message || "Something went wrong");
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
export function useEventNotifications(events) {
  const toast = useToast();
  const seen = useRef(null); // newest event time already shown
  useEffect(() => {
    if (!events?.length) return;
    const newest = events[events.length - 1].at;
    if (seen.current === null) {
      seen.current = newest; // first load: just remember where we are
      return;
    }
    const fresh = events.filter((e) => e.at > seen.current);
    seen.current = newest;
    for (const e of fresh.slice(-3)) {
      if (e.level === "info") continue; // routine; it's in the activity list
      toast(EVENT_TITLE[e.level], e.level, e.text);
      if (CELEBRATE.has(e.kind)) celebrate();
    }
  }, [events, toast]);
}

// Events worth a burst of confetti, by kind.
const CELEBRATE = new Set(["drop", "craft", "all-unlocked"]);

const EVENT_TITLE = { good: "houridle", warn: "Heads up", bad: "Problem" };
