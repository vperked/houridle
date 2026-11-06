// Per-device conveniences (the open tab, a filter). Storage can be missing
// or blocked (private browsing), so every access is guarded.
export function load<T>(key: string, fallback: T): T {
  try {
    const v = localStorage.getItem(`houridle.${key}`);
    return v === null ? fallback : (JSON.parse(v) as T);
  } catch {
    return fallback;
  }
}

export function save(key: string, value: unknown): void {
  try {
    localStorage.setItem(`houridle.${key}`, JSON.stringify(value));
  } catch {
    // not kept: fine
  }
}
