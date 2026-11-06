import { useEffect, useState } from "react";
import type { Art } from "./types.ts";

// Where games' store pictures are. Newer games keep them at addresses with
// a version in them, so houridle looks them up (GET /api/art), all the
// games on screen in one request, and they're remembered for the visit.
const known = new Map<number, Art>(); // app ID -> { capsule, header } (empty if the store has none)
const waiting = new Map<number, ((art: Art) => void)[]>(); // app ID -> callbacks, not asked for yet
const asked = new Map<number, ((art: Art) => void)[]>(); // app ID -> callbacks, asked for and on the way
let timer: ReturnType<typeof setTimeout> | null = null;

// The old, fixed addresses: used only if the lookup fails.
const guess = (id: number): Art => ({
  capsule: `https://shared.akamai.steamstatic.com/store_item_assets/steam/apps/${id}/capsule_184x69.jpg`,
  header: `https://shared.akamai.steamstatic.com/store_item_assets/steam/apps/${id}/header.jpg`,
});

async function flush() {
  timer = null;
  const ids = [...waiting.keys()];
  for (const [id, cbs] of waiting) asked.set(id, cbs);
  waiting.clear();
  for (let i = 0; i < ids.length; i += 300) {
    const batch = ids.slice(i, i + 300);
    let found: Record<string, Art> = {};
    let ok = true;
    try {
      const r = await fetch(`/api/art?apps=${batch.join(",")}`, { cache: "no-store" });
      if (!r.ok) throw new Error();
      found = await r.json();
    } catch {
      ok = false;
    }
    for (const id of batch) {
      const art = ok ? (found[id] ?? {}) : guess(id);
      known.set(id, art);
      for (const cb of asked.get(id) ?? []) cb(art);
      asked.delete(id);
    }
  }
}

function request(id: number, cb: (art: Art) => void) {
  const onTheWay = asked.get(id);
  if (onTheWay) {
    onTheWay.push(cb); // its answer is coming: wait for that one
    return;
  }
  if (!waiting.has(id)) waiting.set(id, []);
  waiting.get(id)!.push(cb);
  if (!timer) timer = setTimeout(flush, 30);
}

// useArt is a game's pictures: undefined while they're looked up.
export function useArt(appId: number): Art | undefined {
  const [art, setArt] = useState(() => known.get(appId));
  useEffect(() => {
    if (known.has(appId)) {
      setArt(known.get(appId));
      return;
    }
    let live = true;
    request(appId, (a) => live && setArt(a));
    return () => {
      live = false;
    };
  }, [appId]);
  return art;
}
