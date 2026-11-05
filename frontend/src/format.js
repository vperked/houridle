// Formatting for durations given in seconds.

// "25m" under an hour, else "3.4 h".
export function hours(s) {
  if (s < 3600) return `${Math.floor(s / 60)}m`;
  return `${(s / 3600).toFixed(1)} h`;
}

// "01:02:03".
export function clock(s) {
  const t = Math.max(0, Math.floor(s));
  const pad = (n) => String(n).padStart(2, "0");
  return `${pad(Math.floor(t / 3600))}:${pad(Math.floor((t % 3600) / 60))}:${pad(t % 60)}`;
}

export function timeOfDay(iso) {
  return new Date(iso).toLocaleTimeString([], { hour: "numeric", minute: "2-digit" });
}

export function dateTime(iso) {
  return new Date(iso).toLocaleString([], { weekday: "short", hour: "2-digit", minute: "2-digit" });
}

export function plural(n, word) {
  return `${n} ${word}${n === 1 ? "" : "s"}`;
}

// "2d 4h", "3h 12m" or "45m": how long something has been running.
export function span(s) {
  const t = Math.max(0, Math.floor(s));
  const d = Math.floor(t / 86400);
  const h = Math.floor((t % 86400) / 3600);
  const m = Math.floor((t % 3600) / 60);
  if (d) return `${d}d ${h}h`;
  if (h) return `${h}h ${m}m`;
  return `${m}m`;
}

// Whole hours as "100 h", otherwise like hours().
export function target(s) {
  return s % 3600 === 0 ? `${s / 3600} h` : hours(s);
}

// "Mon" / "3" style labels for a YYYY-MM-DD day.
export function dayLabel(ymd) {
  const [y, m, d] = ymd.split("-").map(Number);
  const date = new Date(y, m - 1, d);
  return { weekday: date.toLocaleDateString([], { weekday: "short" }), day: d, short: date.toLocaleDateString([], { month: "short", day: "numeric" }), long: date.toLocaleDateString([], { weekday: "long", month: "short", day: "numeric" }) };
}

// A Steam review summary on one line: "Very Positive · 92% of 1.2k".
export function reviewText(review) {
  if (!review?.positive) return "";
  const n = review.count >= 1000 ? `${(review.count / 1000).toFixed(review.count >= 10000 ? 0 : 1)}k` : String(review.count);
  return `${review.summary ? `${review.summary} · ` : ""}${review.positive}% of ${n}`;
}

// pageAddress is where the page is served, as people would type it.
export function pageAddress(webAddr) {
  return webAddr.startsWith(":") ? location.hostname + webAddr : webAddr;
}

// notifyLabel names the kind of alerts webhook ("discord" -> "Discord").
export function notifyLabel(kind) {
  return kind === "text" ? "Plain text" : kind.charAt(0).toUpperCase() + kind.slice(1);
}
