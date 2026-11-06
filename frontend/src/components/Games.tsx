import { memo, useMemo, useState } from "react";
import GameArt from "./GameArt.tsx";
import { Badge, Empty, Fill, Pager, Search, Segmented, Skeleton } from "./ui.tsx";
import type { BadgeTone, SegmentOption } from "./ui.tsx";
import { clock, hours, target, truthy } from "../lib/format.ts";
import { stateOf } from "../lib/states.ts";
import { load, save } from "../lib/storage.ts";
import { usePages } from "../hooks/usePages.ts";
import type { Game, GameState } from "../lib/types.ts";

const SEARCH_FROM = 8; // show the search box from this many games
const PAGE = 50; // rows a page (every-game mode can have thousands)

const TONE: Partial<Record<GameState, BadgeTone>> = { idling: "good", failing: "bad", starting: "warn", waiting: "warn", done: "accent" };

// Games lists every game with its state and progress, with filters and a
// search box once there are enough games to need them.
export default function Games({ games, loading }: { games: Game[]; loading: boolean }) {
  const [filter, setFilterState] = useState(() => load("filter", "all"));
  const [query, setQuery] = useState("");
  const setFilter = (f: string) => {
    setFilterState(f);
    save("filter", f);
  };

  const counts = useMemo(() => {
    const c: Record<string, number> = { all: games.length, idling: 0, queued: 0, refundable: 0, done: 0 };
    for (const g of games) if (g.state in c) c[g.state]++;
    return c;
  }, [games]);

  const filters: SegmentOption<string>[] = [
    { value: "all", label: "All", count: counts.all },
    { value: "idling", label: "Idling", count: counts.idling },
    counts.queued > 0 && { value: "queued", label: "Queued", count: counts.queued },
    counts.refundable > 0 && { value: "refundable", label: "Refundable", count: counts.refundable },
    counts.done > 0 && { value: "done", label: "Done", count: counts.done },
  ].filter(truthy);
  const active = filters.some((f) => f.value === filter) ? filter : "all";

  const q = query.trim().toLowerCase();
  const shown = useMemo(() => games.filter((g) => (active === "all" || g.state === active) && (!q || g.name.toLowerCase().includes(q) || String(g.appId) === q)), [games, active, q]);
  const p = usePages(shown, PAGE, `${active}|${q}`);
  const tools = filters.length > 2 || games.length >= SEARCH_FROM;

  return (
    <section>
      <h2 className="heading">Games</h2>
      {tools && (
        <div className="mb-2.5 space-y-2">
          {games.length >= SEARCH_FROM && <Search value={query} onChange={setQuery} placeholder="Search games" />}
          {filters.length > 2 && <Segmented label="Filter games" options={filters} value={active} onChange={setFilter} />}
        </div>
      )}
      <div ref={p.ref} className="surface overflow-hidden">
        {games.length === 0 ? (
          loading ? <Skeleton art /> : <Empty icon="play">No games yet. houridle is still reading them from Steam.</Empty>
        ) : shown.length === 0 ? (
          <Empty icon="search">No games match.</Empty>
        ) : (
          <>
            <div className="rows">
              {p.items.map((g) => (
                <GameRow key={g.appId} game={g} />
              ))}
            </div>
            <Pager {...p} />
          </>
        )}
      </div>
    </section>
  );
}

// GameRow is memoized: each second only the idling games' rows change.
const GameRow = memo(function GameRow({ game: g }: { game: Game }) {
  const s = stateOf(g.state);
  const live = g.state === "idling";
  const progress = g.target && g.total ? Math.min(100, (100 * g.total) / g.target) : null;
  const details: string[] = [];
  if (g.total) details.push(`${hours(g.total)} total`);
  if (g.target) details.push(`goal ${target(g.target)}`);
  if ((g.drops ?? 0) > 0) details.push(`${g.drops} card drop${g.drops === 1 ? "" : "s"} left`);
  if (g.note && !live && g.note !== s.label.toLowerCase()) details.push(g.note);

  return (
    <div className="row">
      <GameArt appId={g.appId} name={g.name} className="h-9 w-[84px]" />
      <div className="min-w-0 flex-1">
        <div className="truncate text-[15px] font-medium" title={g.name}>
          {g.name}
        </div>
        <div className="sub truncate text-[13px]">{details.length ? details.join(" · ") : live ? "Just started" : "No hours yet"}</div>
        {progress !== null && (
          <div className="mt-1.5 h-[3px] overflow-hidden rounded-full bg-fill">
            <Fill pct={progress} className="duration-700" />
          </div>
        )}
      </div>
      {live ? <span className="num shrink-0 text-[14px] font-medium text-good">{clock(g.idled)}</span> : <Badge tone={TONE[g.state]}>{s.label}</Badge>}
    </div>
  );
});
