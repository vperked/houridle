import { useEffect, useState } from "react";
import { api } from "../lib/api.ts";
import type { Drops } from "../lib/types.ts";

// Card drops left per game (app ID -> drops), read from the badge pages
// once per account and kept, like the library.
const cache = new Map<string, Drops>();

export function useDrops(source: string, enabled: boolean): Drops | null {
  const [drops, setDrops] = useState<Drops | null>(() => cache.get(source) ?? null);
  useEffect(() => {
    if (!enabled || !source) return;
    if (cache.has(source)) {
      setDrops(cache.get(source)!);
      return;
    }
    let live = true;
    api
      .get<Drops | null>("/api/drops")
      .then((d) => {
        cache.set(source, d ?? {});
        if (live) setDrops(d ?? {});
      })
      .catch(() => live && setDrops({}));
    return () => {
      live = false;
    };
  }, [source, enabled]);
  return drops;
}
