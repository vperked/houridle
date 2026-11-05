import { useEffect, useState } from "react";
import { api } from "./api.js";

// Card drops left per game (app ID -> drops), read from the badge pages
// once per account and kept, like the library.
const cache = new Map();

export function useDrops(source, enabled) {
  const [drops, setDrops] = useState(() => cache.get(source) ?? null);
  useEffect(() => {
    if (!enabled || !source) return;
    if (cache.has(source)) {
      setDrops(cache.get(source));
      return;
    }
    let live = true;
    api
      .get("/api/drops")
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
