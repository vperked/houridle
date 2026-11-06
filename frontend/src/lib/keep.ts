// keep returns next, but with every part that's equal to prev's (same JSON
// content) replaced by prev's own object. Each status poll is a fresh
// parse, so without this every game, account and event would look changed
// to React, and memoized rows would redraw anyway.
export function keep<T>(prev: unknown, next: T): T {
  if (Object.is(prev, next)) return prev as T;
  if (typeof prev !== "object" || typeof next !== "object" || !prev || !next || Array.isArray(prev) !== Array.isArray(next)) return next;
  if (Array.isArray(next)) {
    const old = prev as unknown[];
    let same = old.length === next.length;
    const out = next.map((x, i) => {
      const k = keep(old[i], x);
      if (k !== old[i]) same = false;
      return k;
    });
    return (same ? prev : out) as T;
  }
  const old = prev as Record<string, unknown>;
  const fresh = next as Record<string, unknown>;
  const keys = Object.keys(fresh);
  let same = keys.length === Object.keys(old).length;
  const out: Record<string, unknown> = {};
  for (const k of keys) {
    out[k] = keep(old[k], fresh[k]);
    if (out[k] !== old[k]) same = false;
  }
  return (same ? prev : out) as T;
}
