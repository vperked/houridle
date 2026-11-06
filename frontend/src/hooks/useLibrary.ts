import { useCallback, useEffect, useState } from "react";
import { api, errorText } from "../lib/api.ts";
import type { LibraryGame } from "../lib/types.ts";

// The library is fetched once per account and shared by the screens that
// pick games. Listing goes through houridle's Steam connection, so it's
// kept rather than fetched on every visit.
const cache = new Map<string, LibraryGame[]>(); // source -> games

// forgetLibrary drops the kept list, e.g. after games were added, so the
// next look fetches it again.
export function forgetLibrary(): void {
  cache.clear();
}

export interface Library {
  games: LibraryGame[] | null; // null until loaded
  error: string;
  loading: boolean;
  reload: () => Promise<LibraryGame[] | null | undefined>;
}

export function useLibrary(source: string): Library {
  const [games, setGames] = useState<LibraryGame[] | null>(() => cache.get(source) ?? null);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);

  const load = useCallback(
    async (force = false): Promise<LibraryGame[] | null | undefined> => {
      if (!source) return;
      if (!force && cache.has(source)) {
        setGames(cache.get(source)!);
        return;
      }
      setLoading(true);
      setError("");
      try {
        const list = await api.get<LibraryGame[] | null>("/api/library");
        cache.set(source, list ?? []);
        setGames(list ?? []);
        return list ?? [];
      } catch (e) {
        setError(errorText(e));
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
