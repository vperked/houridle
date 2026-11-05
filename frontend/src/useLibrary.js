import { useCallback, useEffect, useState } from "react";
import { api } from "./api.js";

// The library is fetched once per account and shared by the screens that
// pick games. Listing goes through houridle's Steam connection, so it's
// kept rather than fetched on every visit.
const cache = new Map(); // source -> games

// forgetLibrary drops the kept list, e.g. after games were added, so the
// next look fetches it again.
export function forgetLibrary() {
  cache.clear();
}

export function useLibrary(source) {
  const [games, setGames] = useState(() => cache.get(source) ?? null);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);

  const load = useCallback(
    async (force = false) => {
      if (!source) return;
      if (!force && cache.has(source)) {
        setGames(cache.get(source));
        return;
      }
      setLoading(true);
      setError("");
      try {
        const list = await api.get("/api/library");
        cache.set(source, list ?? []);
        setGames(list ?? []);
        return list ?? [];
      } catch (e) {
        setError(e.message);
        return null;
      } finally {
        setLoading(false);
      }
    },
    [source],
  );

  useEffect(() => {
    setGames(cache.get(source) ?? null);
    load();
  }, [source, load]);

  return { games, error, loading, reload: () => load(true) };
}
