import { useEffect } from "react";
import { createPortal } from "react-dom";

// The building blocks every screen is made of. Looks live in index.css
// (surface, row, btn, input...), so screens stay consistent.

// ---------- icons (24px line icons) ----------

const PATHS = {
  now: "M22 12h-4l-3 9L9 3l-3 9H2",
  play: "M7 4.5v15l12.5-7.5L7 4.5z",
  trophy: "M8 21h8M12 17v4M7 4h10v5a5 5 0 0 1-10 0V4zM7 6H4v1a3 3 0 0 0 3 3M17 6h3v1a3 3 0 0 1-3 3",
  history: "M3 3v18h18M8 17v-4M13 17V9M18 17V6",
  more: "M5 12h.01M12 12h.01M19 12h.01",
  chevron: "m9 18 6-6-6-6",
  back: "m15 18-6-6 6-6",
  down: "m6 9 6 6 6-6",
  gift: "M20 12v9H4v-9M2 7h20v5H2zM12 22V7M12 7H7.5a2.5 2.5 0 1 1 0-5C11 2 12 7 12 7zM12 7h4.5a2.5 2.5 0 1 0 0-5C13 2 12 7 12 7z",
  award: "M12 15a6.5 6.5 0 1 0 0-13 6.5 6.5 0 0 0 0 13zM8.5 13.8 7 22l5-3 5 3-1.5-8.2",
  cart: "M3 3h2l2.4 12.2a2 2 0 0 0 2 1.6h8.1a2 2 0 0 0 2-1.5L21 8H6M9.5 21h.01M17.5 21h.01",
  tag: "M20.6 13.4 13.4 20.6a2 2 0 0 1-2.8 0L2 12V2h10l8.6 8.6a2 2 0 0 1 0 2.8zM7 7h.01",
  user: "M20 21a8 8 0 0 0-16 0M12 13a5 5 0 1 0 0-10 5 5 0 0 0 0 10z",
  bell: "M6 8a6 6 0 0 1 12 0c0 7 3 9 3 9H3s3-2 3-9M10.3 21a1.9 1.9 0 0 0 3.4 0",
  link: "M10 13a5 5 0 0 0 7.5.5l3-3a5 5 0 0 0-7-7l-1.7 1.7M14 11a5 5 0 0 0-7.5-.5l-3 3a5 5 0 0 0 7 7l1.7-1.7",
  power: "M12 2v10M18.4 6.6a9 9 0 1 1-12.8 0",
  search: "M11 19a8 8 0 1 0 0-16 8 8 0 0 0 0 16zM21 21l-4.3-4.3",
  refresh: "M21 12a9 9 0 1 1-2.6-6.4M21 3v6h-6",
  check: "M20 6 9 17l-5-5",
  x: "M18 6 6 18M6 6l12 12",
  lock: "M7 11V7a5 5 0 0 1 10 0v4M5 11h14v10H5z",
  stop: "M7 7h10v10H7z",
  sliders: "M4 21v-7M4 10V3M12 21v-9M12 8V3M20 21v-5M20 12V3M1 14h6M9 8h6M17 16h6",
  external: "M15 3h6v6M10 14 21 3M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6",
  alert: "M12 9v4M12 17h.01M10.3 3.9 1.8 18a2 2 0 0 0 1.7 3h17a2 2 0 0 0 1.7-3L13.7 3.9a2 2 0 0 0-3.4 0z",
  info: "M12 22a10 10 0 1 0 0-20 10 10 0 0 0 0 20zM12 16v-4M12 8h.01",
  wallet: "M19 7V5a2 2 0 0 0-2-2H5a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2v-4M21 11h-5a2 2 0 0 0 0 4h5v-4z",
  compass: "M12 22a10 10 0 1 0 0-20 10 10 0 0 0 0 20zM16.2 7.8l-2.1 6.3-6.3 2.1 2.1-6.3z",
  cards: "M8 3h9a2 2 0 0 1 2 2v12M5 7h9a2 2 0 0 1 2 2v10a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V9a2 2 0 0 1 2-2z",
  clock: "M12 22a10 10 0 1 0 0-20 10 10 0 0 0 0 20zM12 6v6l4 2",
  plus: "M12 5v14M5 12h14",
  key: "m15.5 7.5 3 3L22 7l-3-3M2 22l9.6-9.6M7.5 21a5.5 5.5 0 1 0 0-11 5.5 5.5 0 0 0 0 11z",
  monitor: "M2 4h20v13H2zM8 21h8M12 17v4",
};

export function Icon({ name, className = "size-5", strokeWidth = 1.8 }) {
  return (
    <svg viewBox="0 0 24 24" className={`shrink-0 ${className}`} fill="none" stroke="currentColor" strokeWidth={name === "more" ? 3 : strokeWidth} strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <path d={PATHS[name]} />
    </svg>
  );
}

// ---------- page structure ----------

// Page is a screen: a large title (or a back link and title), then content.
// On a desktop, settings-like screens keep a comfortable reading width;
// wide ones (Now, History) use the whole area.
export function Page({ title, back, onBack, action, children, wide }) {
  return (
    <div className={`animate-fade mx-auto w-full ${wide ? "" : "lg:max-w-2xl"}`}>
      {back && (
        <button type="button" onClick={onBack} className="-ml-1 mb-1 inline-flex h-8 items-center gap-0.5 pr-2 text-[15px] text-accent">
          <Icon name="back" className="size-5" strokeWidth={2.2} />
          {back}
        </button>
      )}
      {title && (
        <div className="mb-5 flex items-end justify-between gap-3">
          <h1 className="text-[28px] leading-tight font-bold tracking-[-0.02em] lg:text-[32px]">{title}</h1>
          {action}
        </div>
      )}
      <div className="space-y-7">{children}</div>
    </div>
  );
}

// Group is a titled block: a heading above a surface.
export function Group({ title, action, footer, children, flush }) {
  return (
    <section>
      {(title || action) && (
        <div className="flex items-end justify-between gap-3">
          {title ? <h2 className="heading">{title}</h2> : <span />}
          {action && <div className="pr-1 pb-1.5">{action}</div>}
        </div>
      )}
      <div className={`surface overflow-hidden ${flush ? "" : "rows"}`}>{children}</div>
      {footer && <p className="sub px-4 pt-2 text-[13px]">{footer}</p>}
    </section>
  );
}

// Row is one line in a Group: an optional leading icon or picture, a title
// and detail, and something trailing. With onClick it's a button with a
// chevron.
// Quiet colors for section icons.
const TINTS = {
  green: "bg-emerald-500/15 text-emerald-500",
  amber: "bg-amber-500/15 text-amber-500",
  violet: "bg-violet-500/15 text-violet-400",
  sky: "bg-sky-500/15 text-sky-500",
  pink: "bg-pink-500/15 text-pink-500",
  indigo: "bg-indigo-500/15 text-indigo-400",
  red: "bg-rose-500/15 text-rose-500",
  teal: "bg-teal-500/15 text-teal-500",
};

export function Row({ icon, lead, title, detail, trailing, onClick, chevron = !!onClick, tone, tint, className = "" }) {
  const inner = (
    <>
      {icon && (
        <span className={`grid size-8 shrink-0 place-items-center rounded-[9px] ${tone === "bad" ? "bg-bad/12 text-bad" : (TINTS[tint] ?? "bg-fill text-fg")}`}>
          <Icon name={icon} className="size-[18px]" />
        </span>
      )}
      {lead}
      <span className="min-w-0 flex-1">
        <span className={`block truncate ${tone === "bad" ? "text-bad" : ""}`}>{title}</span>
        {detail && <span className="sub block truncate text-[13px]">{detail}</span>}
      </span>
      {trailing != null && <span className="sub shrink-0 text-[15px]">{trailing}</span>}
      {chevron && <Icon name="chevron" className="faint size-4" strokeWidth={2.2} />}
    </>
  );
  return onClick ? (
    <button type="button" onClick={onClick} className={`row row-tap ${className}`}>
      {inner}
    </button>
  ) : (
    <div className={`row ${className}`}>{inner}</div>
  );
}

// Stat is a number with a label, for rows of figures.
export function Stat({ value, label }) {
  return (
    <div className="min-w-0 flex-1 px-3 py-3 text-center">
      <div className="num truncate text-[20px] leading-6 font-semibold tracking-tight">{value}</div>
      <div className="sub mt-0.5 text-[12px]">{label}</div>
    </div>
  );
}

// Stats is a row of Stat, divided by hairlines.
export function Stats({ children }) {
  return <div className="flex divide-x divide-line">{children}</div>;
}

export function Empty({ icon = "info", children, action }) {
  return (
    <div className="flex flex-col items-center px-6 py-8 text-center">
      <Icon name={icon} className="faint size-7" strokeWidth={1.5} />
      <p className="sub mt-2 max-w-xs text-[14px]">{children}</p>
      {action && <div className="mt-4">{action}</div>}
    </div>
  );
}

export function Badge({ tone, children }) {
  const tones = { good: "bg-good/12 text-good", bad: "bg-bad/12 text-bad", warn: "bg-warn/12 text-warn", accent: "bg-accent/12 text-accent" };
  return <span className={`inline-flex h-5 shrink-0 items-center rounded-md px-1.5 text-[11px] font-semibold ${tones[tone] ?? "bg-fill text-sub"}`}>{children}</span>;
}

export function Dot({ live }) {
  return (
    <span className="relative flex size-2">
      {live && <span className="absolute inline-flex size-full animate-ping rounded-full bg-good opacity-60" />}
      <span className={`relative inline-flex size-2 rounded-full ${live ? "bg-good" : "bg-faint"}`} />
    </span>
  );
}

// Note is a short message in a tinted box.
export function Note({ tone, children }) {
  const tones = { warn: "bg-warn/10 text-warn", bad: "bg-bad/10 text-bad", good: "bg-good/10 text-good" };
  return (
    <div className={`flex gap-2.5 rounded-xl px-3.5 py-3 text-[14px] ${tones[tone] ?? "bg-fill text-sub"}`}>
      <Icon name={tone === "good" ? "check" : tone ? "alert" : "info"} className="mt-px size-[18px]" />
      <div className="min-w-0">{children}</div>
    </div>
  );
}

// Skeleton is grey placeholder rows while something loads.
export function Skeleton({ rows = 4, art }) {
  return (
    <div className="rows" aria-busy="true" aria-label="Loading">
      {Array.from({ length: rows }, (_, i) => (
        <div key={i} className="row">
          {art && <span className="shimmer h-9 w-[84px] rounded-md" />}
          <span className="flex-1 space-y-2">
            <span className="shimmer block h-3 w-2/3 rounded" />
            <span className="shimmer block h-2.5 w-1/3 rounded" />
          </span>
        </div>
      ))}
    </div>
  );
}

// Segmented is a row of mutually exclusive choices.
export function Segmented({ options, value, onChange, label }) {
  return (
    <div role="tablist" aria-label={label} className="flex w-full rounded-[10px] bg-fill p-0.5">
      {options.map((o) => {
        const on = value === o.value;
        return (
          <button
            key={o.value}
            role="tab"
            type="button"
            aria-selected={on}
            onClick={() => onChange(o.value)}
            className={`h-8 min-w-0 flex-1 truncate rounded-lg px-2 text-[13px] font-medium transition ${on ? "bg-surface shadow-[0_1px_3px_rgb(0_0_0/0.12)] dark:bg-[#3a3a3e]" : "sub"}`}
          >
            {o.label}
            {o.count != null && <span className="faint num ml-1">{o.count}</span>}
          </button>
        );
      })}
    </div>
  );
}

// Search is a search field with an icon.
export function Search({ value, onChange, placeholder }) {
  return (
    <label className="relative block">
      <Icon name="search" className="faint pointer-events-none absolute top-1/2 left-3 size-[17px] -translate-y-1/2" />
      <input type="search" value={value} onChange={(e) => onChange(e.target.value)} placeholder={placeholder} aria-label={placeholder} className="input h-9 rounded-[10px] border-0 bg-fill pl-9" />
    </label>
  );
}

// Sheet is a panel that slides up from the bottom (a dialog on wide
// screens), for confirmations and details. Escape or the backdrop close it.
export function Sheet({ open, onClose, title, children }) {
  useEffect(() => {
    if (!open) return;
    const onKey = (e) => e.key === "Escape" && onClose();
    addEventListener("keydown", onKey);
    const overflow = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    return () => {
      removeEventListener("keydown", onKey);
      document.body.style.overflow = overflow;
    };
  }, [open, onClose]);
  if (!open) return null;
  return createPortal(
    <div className="fixed inset-0 z-50 flex items-end justify-center sm:items-center sm:p-6">
      <div className="animate-fade absolute inset-0 bg-black/40" onClick={onClose} />
      <div role="dialog" aria-modal="true" aria-label={title} className="animate-sheet relative max-h-[88dvh] w-full max-w-md overflow-y-auto rounded-t-3xl bg-surface px-5 pt-2 pb-[max(1.25rem,env(safe-area-inset-bottom))] sm:rounded-3xl sm:pt-5">
        <div className="mx-auto mb-3 h-1 w-9 rounded-full bg-line sm:hidden" />
        <div className="mb-4 flex items-start justify-between gap-3">
          <h2 className="text-[19px] font-semibold tracking-tight">{title}</h2>
          <button type="button" onClick={onClose} aria-label="Close" className="-mt-0.5 -mr-1.5 grid size-8 place-items-center rounded-full bg-fill text-sub">
            <Icon name="x" className="size-4" strokeWidth={2.4} />
          </button>
        </div>
        {children}
      </div>
    </div>,
    document.body,
  );
}

// Confirm is a Sheet asking to go ahead with something that can't be undone.
export function Confirm({ open, onClose, title, children, action, danger, busy, onConfirm }) {
  return (
    <Sheet open={open} onClose={onClose} title={title}>
      <div className="sub text-[15px]">{children}</div>
      <div className="mt-6 grid gap-2">
        <button type="button" disabled={busy} onClick={onConfirm} className={`btn-lg ${danger ? "btn-primary !bg-bad !text-white" : "btn-primary"}`}>
          {action}
        </button>
        <button type="button" onClick={onClose} className="btn btn-lg">
          Cancel
        </button>
      </div>
    </Sheet>
  );
}

// ErrorState is an error in place of content, with a way to try again.
export function ErrorState({ error, onRetry }) {
  return (
    <Empty icon="alert" action={onRetry && <button type="button" onClick={onRetry} className="btn btn-sm">Try again</button>}>
      {error}
    </Empty>
  );
}

// NeedsSignIn stands in for a screen that needs a saved Steam sign-in.
export function NeedsSignIn() {
  return (
    <div className="surface">
      <Empty icon="user">This needs a saved Steam sign-in. Add one in Accounts.</Empty>
    </div>
  );
}

// ActionBar keeps a screen's main button above the tab bar while the
// screen scrolls.
export function ActionBar({ children }) {
  return <div className="animate-fade sticky bottom-[calc(3.25rem+env(safe-area-inset-bottom))] z-10 -mx-4 bg-bg/85 px-4 pt-2 pb-3 backdrop-blur-xl lg:bottom-0 lg:mx-0 lg:px-0 lg:pb-5">{children}</div>;
}

// SelectRow is a row with a label and a choice on the right. options is
// [value, label] pairs.
export function SelectRow({ label, detail, value, onChange, options, disabled }) {
  return (
    <label className="row">
      <span className="min-w-0 flex-1">
        <span className="block">{label}</span>
        {detail && <span className="sub block text-[13px]">{detail}</span>}
      </span>
      <select value={value} disabled={disabled} onChange={(e) => onChange(e.target.value)} aria-label={label} className="sub appearance-none bg-transparent text-right outline-none">
        {options.map(([v, l]) => (
          <option key={v} value={v}>
            {l}
          </option>
        ))}
      </select>
      <Icon name="down" className="faint size-4" strokeWidth={2.2} />
    </label>
  );
}

// Avatar is a round initial for an account.
export function Avatar({ name, size = "size-8" }) {
  return <span className={`grid shrink-0 place-items-center rounded-full bg-fill text-[13px] font-semibold ${size}`}>{(name || "?").charAt(0).toUpperCase()}</span>;
}
