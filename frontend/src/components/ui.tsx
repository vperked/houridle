import { useEffect, useLayoutEffect, useRef, useState } from "react";
import type { ReactNode } from "react";
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

export type IconName = keyof typeof PATHS;

interface IconProps {
  name: IconName;
  className?: string;
  strokeWidth?: number;
}

export function Icon({ name, className = "size-5", strokeWidth = 1.8 }: IconProps) {
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
interface PageProps {
  title?: ReactNode;
  back?: ReactNode;
  onBack?: () => void;
  action?: ReactNode;
  children?: ReactNode;
  wide?: boolean;
}

export function Page({ title, back, onBack, action, children, wide }: PageProps) {
  return (
    <div className={`mx-auto w-full ${wide ? "lg:max-w-2xl xl:max-w-none" : "lg:max-w-2xl"}`}>
      {back && (
        <button type="button" onClick={onBack} className="animate-rise -ml-1 mb-1 inline-flex h-8 items-center gap-0.5 pr-2 text-[15px] text-accent">
          <Icon name="back" className="size-5" strokeWidth={2.2} />
          {back}
        </button>
      )}
      {title && (
        <div className="animate-rise mb-5 flex items-end justify-between gap-3">
          <h1 className="text-[28px] leading-tight font-bold tracking-[-0.02em] lg:text-[32px]">{title}</h1>
          {action}
        </div>
      )}
      <div className="page-enter space-y-7">{children}</div>
    </div>
  );
}

// Group is a titled block: a heading above a surface.
interface GroupProps {
  title?: ReactNode;
  action?: ReactNode;
  footer?: ReactNode;
  children?: ReactNode;
  flush?: boolean;
}

export function Group({ title, action, footer, children, flush }: GroupProps) {
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

// Quiet colors for section icons.
export type Tint = "green" | "amber" | "violet" | "sky" | "pink" | "indigo" | "red" | "teal";

const TINTS: Record<Tint, string> = {
  green: "bg-emerald-500/15 text-emerald-500",
  amber: "bg-amber-500/15 text-amber-500",
  violet: "bg-violet-500/15 text-violet-400",
  sky: "bg-sky-500/15 text-sky-500",
  pink: "bg-pink-500/15 text-pink-500",
  indigo: "bg-indigo-500/15 text-indigo-400",
  red: "bg-rose-500/15 text-rose-500",
  teal: "bg-teal-500/15 text-teal-500",
};

// Row is one line in a Group: an optional leading icon or picture, a title
// and detail, and something trailing. With onClick it's a button with a
// chevron.
interface RowProps {
  icon?: IconName;
  lead?: ReactNode;
  title: ReactNode;
  detail?: ReactNode;
  trailing?: ReactNode;
  onClick?: () => void;
  chevron?: boolean;
  tone?: "bad";
  tint?: Tint;
  className?: string;
}

export function Row({ icon, lead, title, detail, trailing, onClick, chevron = !!onClick, tone, tint, className = "" }: RowProps) {
  const inner = (
    <>
      {icon && (
        <span className={`grid size-8 shrink-0 place-items-center rounded-[9px] ${tone === "bad" ? "bg-bad/12 text-bad" : (tint ? TINTS[tint] : "bg-fill text-fg")}`}>
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

// Fill is a progress bar's filled part. It slides in with a transform, so
// a change animates on the GPU (changing width would re-lay out the page on
// every frame). Its track needs overflow-hidden and rounded ends.
export function Fill({ pct, className = "duration-500" }: { pct: number; className?: string }) {
  const p = Math.min(100, Math.max(0, pct));
  return <span className={`block h-full w-full rounded-full bg-accent transition-transform ease-out motion-reduce:transition-none ${className}`} style={{ transform: `translateX(${p - 100}%)` }} />;
}

// pageSlots is which page buttons to show: the first, the last, and the
// ones around the current page, with gaps (null) between. Always 7 or fewer,
// so the bar doesn't change width as you go.
function pageSlots(page: number, pages: number): (number | null)[] {
  if (pages <= 7) return Array.from({ length: pages }, (_, i) => i);
  const lo = Math.max(1, Math.min(page - 1, pages - 5));
  const hi = Math.min(pages - 2, Math.max(page + 1, 4));
  const out: (number | null)[] = [0];
  if (lo > 1) out.push(null);
  for (let i = lo; i <= hi; i++) out.push(i);
  if (hi < pages - 2) out.push(null);
  out.push(pages - 1);
  return out;
}

interface PagerProps {
  page: number;
  pages: number;
  from: number;
  total: number;
  size: number;
  setPage: (page: number) => void;
}

// Pager is the bar under a paged list: which items are showing, and the
// pages. It's nothing when everything fits on one page. Takes usePages'
// result as is: <Pager {...p} />.
export function Pager({ page, pages, from, total, size, setPage }: PagerProps) {
  if (pages <= 1) return null;
  const step = "grid size-8 place-items-center rounded-lg text-[13px] transition-colors active:bg-fill disabled:pointer-events-none disabled:opacity-30";
  return (
    <nav aria-label="Pages" className="flex items-center justify-between gap-2 border-t border-line px-2 py-1.5">
      <span className="sub num hidden pl-2 text-[13px] whitespace-nowrap sm:inline">
        {from + 1}–{Math.min(from + size, total)} of {total}
      </span>
      <span className="flex flex-1 items-center justify-center gap-0.5 sm:flex-none">
        <button type="button" onClick={() => setPage(page - 1)} disabled={page === 0} aria-label="Previous page" className={`${step} text-accent`}>
          <Icon name="back" className="size-4" strokeWidth={2.2} />
        </button>
        {pageSlots(page, pages).map((p, i) =>
          p === null ? (
            <span key={`gap${i}`} className="faint grid size-8 place-items-center text-[13px]" aria-hidden="true">
              …
            </span>
          ) : (
            <button
              key={p}
              type="button"
              onClick={() => setPage(p)}
              aria-label={`Page ${p + 1}`}
              aria-current={p === page ? "page" : undefined}
              className={`${step} num ${p === page ? "bg-accent font-semibold text-white active:bg-accent" : "hover:bg-fill"}`}
            >
              {p + 1}
            </button>
          ),
        )}
        <button type="button" onClick={() => setPage(page + 1)} disabled={page === pages - 1} aria-label="Next page" className={`${step} text-accent`}>
          <Icon name="chevron" className="size-4" strokeWidth={2.2} />
        </button>
      </span>
    </nav>
  );
}

// Loading stands in while a screen's code arrives. It shows only if that
// takes a moment (it fades in late), so a quick load doesn't flash it.
export function Loading() {
  return (
    <div className="loading-late grid place-items-center py-16" role="status" aria-label="Loading">
      <Icon name="refresh" className="faint size-5 animate-spin" strokeWidth={2} />
    </div>
  );
}

// Stat is a number with a label, for rows of figures.
export function Stat({ value, label }: { value: ReactNode; label: ReactNode }) {
  return (
    <div className="min-w-0 flex-1 px-3 py-3 text-center">
      <div className="num truncate text-[20px] leading-6 font-semibold tracking-tight">{value}</div>
      <div className="sub mt-0.5 text-[12px]">{label}</div>
    </div>
  );
}

// Stats is a row of Stat, divided by hairlines.
export function Stats({ children }: { children: ReactNode }) {
  return <div className="flex divide-x divide-line">{children}</div>;
}

// Empty stands in for a list with nothing in it: an icon, a line about
// why, and maybe a way forward.
export function Empty({ icon = "info", children, action }: { icon?: IconName; children?: ReactNode; action?: ReactNode }) {
  return (
    <div className="flex flex-col items-center px-6 py-9 text-center">
      <span className="grid size-12 place-items-center rounded-full bg-fill">
        <Icon name={icon} className="sub size-6" strokeWidth={1.7} />
      </span>
      <p className="sub mt-3 max-w-xs text-[14px] leading-relaxed">{children}</p>
      {action && <div className="mt-4">{action}</div>}
    </div>
  );
}

// Badge is a small label for a state (In use, Queued, Done...).
export type BadgeTone = "good" | "bad" | "warn" | "accent";

export function Badge({ tone, children }: { tone?: BadgeTone; children: ReactNode }) {
  const tones: Record<BadgeTone, string> = { good: "bg-good/12 text-good", bad: "bg-bad/12 text-bad", warn: "bg-warn/12 text-warn", accent: "bg-accent/12 text-accent" };
  return <span className={`inline-flex h-5 shrink-0 items-center rounded-md px-1.5 text-[11px] font-semibold ${tone ? tones[tone] : "bg-fill text-sub"}`}>{children}</span>;
}

// Dot is a status light: green and pulsing while live, grey otherwise.
export function Dot({ live }: { live: boolean }) {
  return (
    <span className="relative flex size-2">
      {live && <span className="absolute inline-flex size-full animate-ping rounded-full bg-good opacity-60" />}
      <span className={`relative inline-flex size-2 rounded-full ${live ? "bg-good" : "bg-faint"}`} />
    </span>
  );
}

// Note is a short message in a tinted box.
export type NoteTone = "warn" | "bad" | "good";

export function Note({ tone, children }: { tone?: NoteTone; children: ReactNode }) {
  const tones: Record<NoteTone, string> = { warn: "bg-warn/10 text-warn", bad: "bg-bad/10 text-bad", good: "bg-good/10 text-good" };
  return (
    <div className={`flex gap-2.5 rounded-xl px-3.5 py-3 text-[14px] ${tone ? tones[tone] : "bg-fill text-sub"}`}>
      <Icon name={tone === "good" ? "check" : tone ? "alert" : "info"} className="mt-px size-[18px]" />
      <div className="min-w-0">{children}</div>
    </div>
  );
}

// Skeleton is grey placeholder rows while something loads.
export function Skeleton({ rows = 4, art }: { rows?: number; art?: boolean }) {
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
export interface SegmentOption<T> {
  value: T;
  label: ReactNode;
  count?: number;
}

interface SegmentedProps<T> {
  options: SegmentOption<T>[];
  value: T;
  onChange: (value: T) => void;
  label: string;
}

export function Segmented<T extends string | number>({ options, value, onChange, label }: SegmentedProps<T>) {
  const bar = useRef<HTMLDivElement>(null);
  const [thumb, setThumb] = useState<{ x: number; w: number; moved: boolean } | null>(null);
  const [more, setMore] = useState(""); // which sides have more to scroll to: "", "l", "r" or "lr"
  const edges = () => {
    const el = bar.current;
    if (!el) return;
    const l = el.scrollLeft > 1 ? "l" : "";
    const r = el.scrollLeft + el.clientWidth < el.scrollWidth - 1 ? "r" : "";
    setMore(l + r);
  };
  const picked = options.findIndex((o) => o.value === value);

  // One raised pill slides to the chosen option. It's measured rather than
  // worked out, so options can be as wide as their labels: they share the
  // width when there's room, and the bar scrolls sideways when there isn't
  // (instead of cutting labels short).
  useLayoutEffect(() => {
    const el = bar.current;
    if (!el) return;
    const chosen = () => el.querySelector<HTMLElement>('[aria-selected="true"]');
    const place = () => {
      const b = chosen();
      setThumb((t) => (b ? { x: b.offsetLeft, w: b.offsetWidth, moved: t !== null } : null));
      edges();
    };
    place();
    const b = chosen();
    if (b && el.scrollWidth > el.clientWidth) el.scrollLeft = b.offsetLeft - (el.clientWidth - b.offsetWidth) / 2;
    edges();
    const watch = new ResizeObserver(place);
    watch.observe(el);
    for (const child of el.querySelectorAll("[role=tab]")) watch.observe(child);
    return () => watch.disconnect();
  }, [picked, options.length]); // eslint-disable-line react-hooks/exhaustive-deps

  return (
    <div ref={bar} role="tablist" aria-label={label} onScroll={edges} className={`no-scrollbar relative flex w-full overflow-x-auto rounded-[10px] bg-fill p-0.5 ${more ? `fade-${more}` : ""}`}>
      {thumb && (
        <span
          aria-hidden="true"
          className="seg-thumb absolute top-0.5 bottom-0.5 left-0 rounded-lg bg-raised shadow-[0_1px_3px_rgb(0_0_0/0.12),0_0_0_0.5px_rgb(0_0_0/0.04)]"
          style={{ width: thumb.w, transform: `translateX(${thumb.x}px)`, transition: thumb.moved ? undefined : "none" }}
        />
      )}
      {options.map((o) => {
        const on = value === o.value;
        return (
          <button
            key={o.value}
            role="tab"
            type="button"
            aria-selected={on}
            onClick={() => onChange(o.value)}
            className={`relative h-8 flex-1 rounded-lg px-3 text-[13px] font-medium whitespace-nowrap transition-colors duration-200 ${on ? "text-fg" : "sub hover:text-fg"}`}
          >
            {o.label}
            {o.count != null && <span className={`num ml-1 ${on ? "sub" : "faint"}`}>{o.count}</span>}
          </button>
        );
      })}
    </div>
  );
}

// Search is a search field with an icon.
export function Search({ value, onChange, placeholder }: { value: string; onChange: (value: string) => void; placeholder: string }) {
  return (
    <label className="relative block">
      <Icon name="search" className="faint pointer-events-none absolute top-1/2 left-3 size-[17px] -translate-y-1/2" />
      <input type="search" value={value} onChange={(e) => onChange(e.target.value)} placeholder={placeholder} aria-label={placeholder} className="input h-9 rounded-[10px] border-0 bg-fill pl-9" />
    </label>
  );
}

// Sheet is a panel that slides up from the bottom (a dialog on wide
// screens), for confirmations and details. Escape or the backdrop close it.
interface SheetProps {
  open: boolean;
  onClose: () => void;
  title?: string;
  children?: ReactNode;
}

// How long a sheet takes to go away. Match animate-sheet-out in index.css.
const SHEET_LEAVE_MS = 240;

export function Sheet({ open, onClose, title, children }: SheetProps) {
  // A closed sheet stays up while it slides away, still showing what it
  // showed (the screen may already have dropped what it was about).
  const [shown, setShown] = useState(open);
  const kept = useRef({ title, children });
  if (open) kept.current = { title, children };
  if (open && !shown) setShown(true);
  const leaving = shown && !open;
  useEffect(() => {
    if (!leaving) return;
    const t = setTimeout(() => setShown(false), SHEET_LEAVE_MS);
    return () => clearTimeout(t);
  }, [leaving]);

  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && onClose();
    addEventListener("keydown", onKey);
    const overflow = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    return () => {
      removeEventListener("keydown", onKey);
      document.body.style.overflow = overflow;
    };
  }, [open, onClose]);
  if (!shown) return null;
  const view = open ? { title, children } : kept.current;
  return createPortal(
    <div className={`fixed inset-0 z-50 flex items-end justify-center sm:items-center sm:p-6 ${leaving ? "pointer-events-none" : ""}`}>
      <div className={`${leaving ? "animate-fade-out" : "animate-fade"} absolute inset-0 bg-black/40`} onClick={onClose} />
      <div role="dialog" aria-modal="true" aria-label={view.title} className={`${leaving ? "animate-sheet-out" : "animate-sheet"} relative max-h-[88dvh] w-full max-w-md overflow-y-auto rounded-t-3xl bg-surface px-5 pt-2 pb-[max(1.25rem,env(safe-area-inset-bottom))] sm:rounded-3xl sm:pt-5`}>
        <div className="mx-auto mb-3 h-1 w-9 rounded-full bg-line sm:hidden" />
        <div className="mb-4 flex items-start justify-between gap-3">
          <h2 className="text-[19px] font-semibold tracking-tight">{view.title}</h2>
          <button type="button" onClick={onClose} aria-label="Close" className="-mt-0.5 -mr-1.5 grid size-8 place-items-center rounded-full bg-fill text-sub">
            <Icon name="x" className="size-4" strokeWidth={2.4} />
          </button>
        </div>
        {view.children}
      </div>
    </div>,
    document.body,
  );
}

// Confirm is a Sheet asking to go ahead with something that can't be undone.
interface ConfirmProps {
  open: boolean;
  onClose: () => void;
  title: string;
  children: ReactNode;
  action: ReactNode;
  danger?: boolean;
  busy?: boolean;
  onConfirm: () => void;
}

export function Confirm({ open, onClose, title, children, action, danger, busy, onConfirm }: ConfirmProps) {
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
export function ErrorState({ error, onRetry }: { error: ReactNode; onRetry?: () => void }) {
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
export function ActionBar({ children }: { children: ReactNode }) {
  return <div className="animate-fade sticky bottom-[calc(3.25rem+env(safe-area-inset-bottom))] z-10 -mx-4 bg-bg/85 px-4 pt-2 pb-3 backdrop-blur-xl lg:bottom-0 lg:mx-0 lg:px-0 lg:pb-5">{children}</div>;
}

// SelectRow is a row with a label and a choice on the right. options is
// [value, label] pairs.
interface SelectRowProps<V> {
  label: string;
  detail?: ReactNode;
  value: V;
  onChange: (value: string) => void;
  options: (readonly [V, string])[];
  disabled?: boolean;
}

export function SelectRow<V extends string | number>({ label, detail, value, onChange, options, disabled }: SelectRowProps<V>) {
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
export function Avatar({ name, size = "size-8" }: { name: string; size?: string }) {
  return <span className={`grid shrink-0 place-items-center rounded-full bg-fill text-[13px] font-semibold ${size}`}>{(name || "?").charAt(0).toUpperCase()}</span>;
}
