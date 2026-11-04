// Per-device conveniences (the open tab, a filter). Storage can be missing
// or blocked (private browsing), so every access is guarded.
export function load(key, fallback) {
  try {
    const v = localStorage.getItem(`houridle.${key}`);
    return v === null ? fallback : JSON.parse(v);
  } catch {
    return fallback;
  }
}

export function save(key, value) {
  try {
    localStorage.setItem(`houridle.${key}`, JSON.stringify(value));
  } catch {
    // not kept: fine
  }
}
