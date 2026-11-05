import { useEffect, useState } from "react";

// Where games' store pictures are. Newer games keep them at addresses with
// a version in them, so houridle looks them up (GET /api/art), all the
// games on screen in one request, and they're remembered for the visit.
const known = new Map(); // app ID -> { capsule, header } (empty if the store has none)
const waiting = new Map(); // app ID -> callbacks
let timer = null;

// The old, fixed addresses: used only if the lookup fails.
const guess = (id) => ({
  capsule: `https://shared.akamai.steamstatic.com/store_item_assets/steam/apps/${id}/capsule_184x69.jpg`,
  header: `https://shared.akamai.steamstatic.com/store_item_assets/steam/apps/${id}/header.jpg`,
});

async function flush() {
  timer = null;
  const ids = [...waiting.keys()];
  const calls = new Map(waiting);
  waiting.clear();
  for (let i = 0; i < ids.length; i += 300) {
    const batch = ids.slice(i, i + 300);
    let found = {};
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
      for (const cb of calls.get(id) ?? []) cb(art);
    }
  }
}

function request(id, cb) {
  if (!waiting.has(id)) waiting.set(id, []);
  waiting.get(id).push(cb);
  if (!timer) timer = setTimeout(flush, 30);
}

// useArt is a game's pictures: undefined while they're looked up.
export function useArt(appId) {
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
