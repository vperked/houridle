import type { Review } from "./types.ts";

// Formatting for durations given in seconds.

// "25m" under an hour, else "3.4 h".
export function hours(s: number): string {
  if (s < 3600) return `${Math.floor(s / 60)}m`;
  return `${(s / 3600).toFixed(1)} h`;
}

// "01:02:03".
export function clock(s: number): string {
  const t = Math.max(0, Math.floor(s));
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${pad(Math.floor(t / 3600))}:${pad(Math.floor((t % 3600) / 60))}:${pad(t % 60)}`;
}

export function timeOfDay(iso: string): string {
  return new Date(iso).toLocaleTimeString([], { hour: "numeric", minute: "2-digit" });
}

export function dateTime(iso: string): string {
  return new Date(iso).toLocaleString([], { weekday: "short", hour: "2-digit", minute: "2-digit" });
}

export function plural(n: number, word: string): string {
  return `${n} ${word}${n === 1 ? "" : "s"}`;
}

// "2d 4h", "3h 12m" or "45m": how long something has been running.
export function span(s: number): string {
  const t = Math.max(0, Math.floor(s));
  const d = Math.floor(t / 86400);
  const h = Math.floor((t % 86400) / 3600);
  const m = Math.floor((t % 3600) / 60);
  if (d) return `${d}d ${h}h`;
  if (h) return `${h}h ${m}m`;
  return `${m}m`;
}

// Whole hours as "100 h", otherwise like hours().
export function target(s: number): string {
  return s % 3600 === 0 ? `${s / 3600} h` : hours(s);
}

export interface DayLabel {
  weekday: string;
  day: number;
  short: string;
  long: string;
}

const WEEKDAY = new Intl.DateTimeFormat([], { weekday: "short" });
const SHORT_DAY = new Intl.DateTimeFormat([], { month: "short", day: "numeric" });
const LONG_DAY = new Intl.DateTimeFormat([], { weekday: "long", month: "short", day: "numeric" });
const dayLabels = new Map<string, DayLabel>();

// "Mon" / "3" style labels for a YYYY-MM-DD day. Remembered: the History
// tab labels its 84 days again with every tick of the clock.
export function dayLabel(ymd: string): DayLabel {
  let label = dayLabels.get(ymd);
  if (!label) {
    const [y, m, d] = ymd.split("-").map(Number);
    const date = new Date(y, m - 1, d);
    label = { weekday: WEEKDAY.format(date), day: d, short: SHORT_DAY.format(date), long: LONG_DAY.format(date) };
    dayLabels.set(ymd, label);
  }
  return label;
}

// A Steam review summary on one line: "Very Positive · 92% of 1.2k".
export function reviewText(review: Review | undefined): string {
  if (!review?.positive) return "";
  const count = review.count ?? 0;
  const n = count >= 1000 ? `${(count / 1000).toFixed(count >= 10000 ? 0 : 1)}k` : String(review.count);
  return `${review.summary ? `${review.summary} · ` : ""}${review.positive}% of ${n}`;
}

// pageAddress is where the page is served, as people would type it.
export function pageAddress(webAddr: string): string {
  return webAddr.startsWith(":") ? location.hostname + webAddr : webAddr;
}

// notifyLabel names the kind of alerts webhook ("discord" -> "Discord").
export function notifyLabel(kind: string): string {
  return kind === "text" ? "Plain text" : kind.charAt(0).toUpperCase() + kind.slice(1);
}

// truthy keeps the set items of a list built with `cond && item`.
export function truthy<T>(x: T | false | null | undefined | 0 | ""): x is T {
  return !!x;
}
