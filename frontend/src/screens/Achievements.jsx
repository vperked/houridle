import { useEffect, useMemo, useRef, useState } from "react";
import GameArt from "../components/GameArt.jsx";
import { Checkbox } from "../components/Checkbox.jsx";
import { ActionBar, Badge, Empty, ErrorState, Group, Icon, Note, Page, Search, Segmented, SelectRow, Sheet, Skeleton } from "../components/ui.jsx";
import { api } from "../api.js";
import { useAction, useToast } from "../toast.jsx";
import { celebrate } from "../fx.js";
import { useLibrary } from "../useLibrary.js";
import { plural } from "../format.js";

// Achievements unlocks and locks a game's achievements, like
// `houridle -achievements`.
export default function Achievements({ panel }) {
  const [game, setGame] = useState(null);
  if (!panel.source) {
    return (
      <Page title="Achievements">
        <div className="surface">
          <Empty icon="user">Sign in to Steam first (More → Accounts).</Empty>
        </div>
      </Page>
    );
  }
  return game ? <Editor game={game} onBack={() => setGame(null)} drips={panel.drip} /> : <GameList source={panel.source} onPick={setGame} drips={panel.drip} />;
}

function GameList({ source, onPick, drips }) {
  const { games, error, loading, reload } = useLibrary(source);
  const [query, setQuery] = useState("");
  const [selecting, setSelecting] = useState(false);
  const [picked, setPicked] = useState([]);
  const [bulk, setBulk] = useState(false);
  const q = query.trim().toLowerCase();
  const shown = useMemo(() => (games ?? []).filter((g) => !q || g.name.toLowerCase().includes(q) || String(g.appId) === q), [games, q]);
  const pickedSet = useMemo(() => new Set(picked), [picked]);
  const pickedGames = useMemo(() => {
    const byId = new Map((games ?? []).map((g) => [g.appId, g]));
    return picked.map((id) => byId.get(id) ?? { appId: id, name: `App ${id}` });
  }, [games, picked]);
  const toggle = (id) => setPicked((p) => (p.includes(id) ? p.filter((x) => x !== id) : [...p, id]));
  const allShown = shown.length > 0 && shown.slice(0, MAX_BULK).every((g) => pickedSet.has(g.appId));
  const stopSelecting = () => {
    setSelecting(false);
    setPicked([]);
  };

  return (
    <Page
      title="Achievements"
      action={
        games?.length > 0 && (
          <button type="button" onClick={() => (selecting ? stopSelecting() : setSelecting(true))} className="btn-ghost mb-0.5 h-8 font-medium">
            {selecting ? "Done" : "Select"}
          </button>
        )
      }
    >
      {!selecting && <DripList drips={drips} />}
      <section>
        <div className="mb-2.5 space-y-2">
          <Search value={query} onChange={setQuery} placeholder="Search your games" />
          {selecting && (
            <div className="flex items-center justify-between px-1">
              <span className="sub num text-[13px]">{picked.length ? `${picked.length} selected` : "Pick games to change all at once"}</span>
              <span className="flex">
                {picked.length > 0 && (
                  <button type="button" onClick={() => setPicked([])} className="btn-ghost btn-sm h-7">
                    Clear
                  </button>
                )}
                <button
                  type="button"
                  onClick={() => setPicked(allShown ? picked.filter((id) => !shown.some((g) => g.appId === id)) : [...new Set([...picked, ...shown.map((g) => g.appId)])].slice(0, MAX_BULK))}
                  className="btn-ghost btn-sm h-7"
                >
                  {allShown ? "Deselect shown" : q ? `Select ${Math.min(shown.length, MAX_BULK)} shown` : "Select all"}
                </button>
              </span>
            </div>
          )}
        </div>
        <div className="surface overflow-hidden">
          {error ? (
            <ErrorState error={error} onRetry={reload} />
          ) : loading && !games ? (
            <Skeleton art rows={7} />
          ) : shown.length === 0 ? (
            <Empty icon="search">No games match.</Empty>
          ) : (
            <ul className="rows max-h-[36rem] overflow-y-auto overscroll-contain">
              {shown.slice(0, 300).map((g) => (
                <li key={g.appId}>
                  {selecting ? (
                    <label className="row row-tap cursor-pointer">
                      <Checkbox checked={pickedSet.has(g.appId)} disabled={!pickedSet.has(g.appId) && picked.length >= MAX_BULK} onChange={() => toggle(g.appId)} label={g.name} />
                      <GameArt appId={g.appId} name={g.name} className="h-8 w-[75px]" />
                      <span className="min-w-0 flex-1 truncate">{g.name}</span>
                    </label>
                  ) : (
                    <button type="button" onClick={() => onPick(g)} className="row row-tap">
                      <GameArt appId={g.appId} name={g.name} className="h-8 w-[75px]" />
                      <span className="min-w-0 flex-1 truncate">{g.name}</span>
                      {g.shared && <Badge>Family</Badge>}
                      <Icon name="chevron" className="faint size-4" strokeWidth={2.2} />
                    </button>
                  )}
                </li>
              ))}
            </ul>
          )}
        </div>
        {!selecting && games?.length > 1 && <p className="sub px-4 pt-2 text-[13px]">Tap a game to pick achievements, or Select to unlock everything in several games at once.</p>}
        {selecting && picked.length >= MAX_BULK && <p className="sub px-4 pt-2 text-[13px]">Up to {MAX_BULK} games at a time.</p>}
      </section>
      {selecting && picked.length > 0 && (
        <ActionBar>
          <button type="button" onClick={() => setBulk(true)} className="btn-primary btn-lg w-full">
            Change {picked.length} game{picked.length === 1 ? "" : "s"}…
          </button>
        </ActionBar>
      )}
      <BulkSheet
        open={bulk}
        games={pickedGames}
        onClose={() => setBulk(false)}
        onDone={() => {
          setBulk(false);
          stopSelecting();
        }}
      />
    </Page>
  );
}

const MAX_BULK = 100;
const CHUNK = 4; // games per request: keeps each well inside the page's time limit, and shows progress

// BulkSheet unlocks (or locks) everything in the picked games: it asks
// first, shows progress game by game, then what happened in each.
function BulkSheet({ open, games, onClose, onDone }) {
  const [mode, setMode] = useState("unlock"); // unlock | drip | lock
  const [every, setEvery] = useState(60);
  const unlock = mode !== "lock";
  const [phase, setPhase] = useState("ask"); // ask | running | done
  const [results, setResults] = useState([]);
  const [error, setError] = useState("");
  const stop = useRef(false);
  const toast = useToast();

  useEffect(() => {
    if (open) {
      setPhase("ask");
      setResults([]);
      setError("");
    }
  }, [open]);

  const startDrip = async () => {
    try {
      await api.post("/api/drip", { apps: games.map((g) => g.appId), minutes: every });
      toast(`Unlocking gradually in ${plural(games.length, "game")}`, "good", `About one every ${intervalLabel(every)}, the most common first.`);
      onDone();
    } catch (e) {
      toast("Couldn't do that", "bad", e.message);
    }
  };

  const run = async () => {
    if (mode === "drip") return startDrip();
    setPhase("running");
    stop.current = false;
    const all = [];
    for (let i = 0; i < games.length && !stop.current; i += CHUNK) {
      try {
        const res = await api.post("/api/achievements/bulk", { apps: games.slice(i, i + CHUNK).map((g) => g.appId), unlock });
        all.push(...res.games);
      } catch (e) {
        setError(e.message);
        break;
      }
      setResults([...all]);
    }
    setPhase("done");
    const changed = all.reduce((t, r) => t + r.changed, 0);
    if (changed && unlock) celebrate();
    toast(changed ? `${unlock ? "Unlocked" : "Locked"} ${plural(changed, "achievement")}` : "Nothing needed changing", changed ? "good" : "info", `In ${all.filter((r) => r.changed).length} of ${all.length} games.`);
  };

  const done = results.length;
  const changed = results.reduce((t, r) => t + r.changed, 0);
  const verb = unlock ? "unlock" : "lock";
  return (
    <Sheet open={open} onClose={phase === "running" ? () => (stop.current = true) : phase === "done" ? onDone : onClose} title={phase === "done" ? `${unlock ? "Unlocked" : "Locked"} ${plural(changed, "achievement")}` : `${plural(games.length, "game")}`}>
      {phase === "ask" && (
        <>
          <Segmented
            label="Unlock or lock"
            value={mode}
            onChange={setMode}
            options={[
              { value: "unlock", label: "Unlock all" },
              { value: "drip", label: "Gradually" },
              { value: "lock", label: "Lock all" },
            ]}
          />
          <div className="mt-4 flex -space-x-2 overflow-hidden">
            {games.slice(0, 6).map((g) => (
              <GameArt key={g.appId} appId={g.appId} name={g.name} className="h-9 w-[84px] ring-2 ring-surface" />
            ))}
            {games.length > 6 && <span className="sub grid h-9 shrink-0 place-items-center rounded-md bg-fill px-3 text-[13px] ring-2 ring-surface">+{games.length - 6}</span>}
          </div>
          <p className="sub mt-4 text-[15px]">
            {mode === "unlock"
              ? "Every achievement in these games gets unlocked now. They show on your profile like any others, and you can lock them again."
              : mode === "drip"
                ? "One achievement at a time, the most common first, so your profile looks like you played. houridle keeps going while it runs."
                : "Every achievement in these games gets locked again, including ones you earned by playing."}{" "}
            Ones only a game's own servers can change are left as they are.
          </p>
          {mode === "drip" && <IntervalPicker value={every} onChange={setEvery} />}
          <div className="mt-6 grid gap-2">
            <button type="button" onClick={run} className={`btn-primary btn-lg ${unlock ? "" : "!bg-bad !text-white"}`}>
              {mode === "drip" ? `Start · about every ${intervalLabel(every)}` : `${unlock ? "Unlock" : "Lock"} everything in ${plural(games.length, "game")}`}
            </button>
            <button type="button" onClick={onClose} className="btn btn-lg">
              Cancel
            </button>
          </div>
        </>
      )}
      {phase === "running" && (
        <div className="py-2">
          <div className="flex items-baseline justify-between text-[15px]">
            <span className="min-w-0 truncate">{stop.current ? "Stopping after this batch…" : `Working on ${games[Math.min(done, games.length - 1)]?.name}`}</span>
            <span className="sub num shrink-0 pl-3">
              {done} of {games.length}
            </span>
          </div>
          <div className="mt-3 h-1.5 overflow-hidden rounded-full bg-fill">
            <div className="h-full rounded-full bg-accent transition-[width] duration-500" style={{ width: `${Math.max(4, (100 * done) / games.length)}%` }} />
          </div>
          <button type="button" onClick={() => (stop.current = true)} className="btn btn-lg mt-6 w-full">
            Stop
          </button>
        </div>
      )}
      {phase === "done" && (
        <>
          {error && (
            <div className="mb-3">
              <Note tone="bad">Stopped: {error}</Note>
            </div>
          )}
          <div className="surface rows overflow-hidden">
            {results.map((r) => (
              <div key={r.appId} className="row">
                <GameArt appId={r.appId} name={r.name} className="h-8 w-[75px]" />
                <span className="min-w-0 flex-1">
                  <span className="block truncate">{r.name}</span>
                  <span className={`block truncate text-[13px] ${r.error ? "text-bad" : "sub"}`}>{bulkDetail(r, verb)}</span>
                </span>
                {r.changed > 0 ? <Badge tone={unlock ? "good" : "bad"}>{(unlock ? "+" : "−") + r.changed}</Badge> : r.error ? <Icon name="alert" className="size-4 text-bad" /> : <Icon name="check" className="faint size-4" />}
              </div>
            ))}
          </div>
          {results.length < games.length && !error && <p className="sub mt-2 px-1 text-[13px]">Stopped after {results.length} of {games.length} games.</p>}
          <button type="button" onClick={onDone} className="btn-primary btn-lg mt-5 w-full">
            Done
          </button>
        </>
      )}
    </Sheet>
  );
}

function bulkDetail(r, verb) {
  if (r.error) return r.error;
  if (!r.total) return "No achievements";
  const parts = [];
  if (r.changed) parts.push(`${r.changed} ${verb}ed`);
  else if (!r.failed) parts.push(`All ${r.total - r.gameOnly} already ${verb}ed`);
  if (r.failed) parts.push(`${r.failed} refused`);
  if (r.gameOnly) parts.push(`${r.gameOnly} game-only`);
  return parts.join(" · ");
}

function Editor({ game, onBack, drips }) {
  const [list, setList] = useState(null);
  const [error, setError] = useState("");
  const [want, setWant] = useState({}); // id -> wanted unlocked state, when it differs
  const [filter, setFilter] = useState("all");
  const [query, setQuery] = useState("");
  const [busy, run] = useAction();

  const load = async () => {
    setError("");
    try {
      setList(await api.get(`/api/achievements?app=${game.appId}`));
      setWant({});
    } catch (e) {
      setError(e.message);
    }
  };
  useEffect(() => {
    load();
  }, [game.appId]); // eslint-disable-line react-hooks/exhaustive-deps

  const isOn = (a) => (a.id in want ? want[a.id] : a.unlocked);
  const toggle = (a) =>
    !a.protected &&
    setWant((w) => {
      const next = { ...w };
      const on = !isOn(a);
      if (on === a.unlocked) delete next[a.id];
      else next[a.id] = on;
      return next;
    });
  const setAll = (on) => setWant(Object.fromEntries((list ?? []).filter((a) => !a.protected && a.unlocked !== on).map((a) => [a.id, on])));
  const protectedCount = (list ?? []).filter((a) => a.protected).length;

  const changes = Object.entries(want);
  const unlock = changes.filter(([, on]) => on).map(([id]) => id);
  const lock = changes.filter(([, on]) => !on).map(([id]) => id);
  const unlockedCount = (list ?? []).filter(isOn).length;

  const q = query.trim().toLowerCase();
  const shown = useMemo(
    () =>
      (list ?? []).filter((a) => {
        if (filter === "unlocked" && !isOn(a)) return false;
        if (filter === "locked" && isOn(a)) return false;
        return !q || a.name.toLowerCase().includes(q) || a.id.toLowerCase().includes(q) || (a.description || "").toLowerCase().includes(q);
      }),
    [list, want, filter, q], // eslint-disable-line react-hooks/exhaustive-deps
  );

  const apply = () =>
    run(
      "apply",
      async () => {
        const res = await api.post("/api/achievements", { app: game.appId, unlock, lock });
        await load();
        return res;
      },
      (res) => {
        const failed = Object.keys(res?.failed ?? {}).length;
        return failed
          ? { title: "Saved, with exceptions", tone: "warn", detail: `${failed} didn't change: the game checks them on its servers.` }
          : { title: "Achievements saved", celebrate: unlock.length > 0, detail: `${unlock.length ? `${unlock.length} unlocked` : ""}${unlock.length && lock.length ? ", " : ""}${lock.length ? `${lock.length} locked` : ""} in ${game.name}.` };
      },
    );

  const pct = list?.length ? Math.round((100 * unlockedCount) / list.length) : 0;

  return (
    <Page back="Games" onBack={onBack}>
      <section className="surface overflow-hidden">
        <GameArt appId={game.appId} name={game.name} variant="header" className="aspect-[460/215] w-full !rounded-none" />
        <div className="p-4">
          <h1 className="truncate text-[20px] font-semibold tracking-tight">{game.name}</h1>
          {list && list.length > 0 && (
            <>
              <div className="sub num mt-0.5 text-[14px]">
                {unlockedCount} of {list.length} unlocked · {pct}%
              </div>
              <div className="mt-3 h-1 overflow-hidden rounded-full bg-fill">
                <div className="h-full rounded-full bg-accent transition-[width]" style={{ width: `${pct}%` }} />
              </div>
            </>
          )}
        </div>
      </section>

      {list && list.some((a) => !a.unlocked && !a.protected) && (
        drips?.some((d) => d.appId === game.appId) ? (
          <Note>Unlocking gradually: {drips.find((d) => d.appId === game.appId).left} left. Stop it from the game list.</Note>
        ) : (
          <DripButton game={game} />
        )
      )}
      {error ? (
        <div className="surface">
          <ErrorState error={error} onRetry={load} />
        </div>
      ) : !list ? (
        <div className="surface overflow-hidden">
          <Skeleton rows={6} />
        </div>
      ) : list.length === 0 ? (
        <div className="surface">
          <Empty icon="trophy">This game has no achievements.</Empty>
        </div>
      ) : (
        <section>
          <div className="mb-2.5 space-y-2">
            <Segmented
              label="Filter achievements"
              value={filter}
              onChange={setFilter}
              options={[
                { value: "all", label: "All", count: list.length },
                { value: "locked", label: "Locked", count: list.length - unlockedCount },
                { value: "unlocked", label: "Unlocked", count: unlockedCount },
              ]}
            />
            <Search value={query} onChange={setQuery} placeholder="Search achievements" />
            <div className="flex items-center justify-between px-1">
              <span className="sub text-[13px]">{changes.length ? `${plural(changes.length, "change")}` : "Tap to unlock or lock"}</span>
              <span className="flex">
                {changes.length > 0 && (
                  <button type="button" onClick={() => setWant({})} className="btn-ghost btn-sm h-7">
                    Undo
                  </button>
                )}
                <button type="button" onClick={() => setAll(true)} className="btn-ghost btn-sm h-7">
                  Unlock all
                </button>
                <button type="button" onClick={() => setAll(false)} className="btn-ghost btn-sm h-7">
                  Lock all
                </button>
              </span>
            </div>
          </div>
          <div className="surface overflow-hidden">
            {shown.length === 0 ? (
              <Empty icon="search">No achievements match.</Empty>
            ) : (
              <ul className="rows">
                {shown.map((a) => {
                  const on = isOn(a);
                  const changed = a.id in want;
                  return (
                    <li key={a.id}>
                      <label className={`row ${a.protected ? "cursor-not-allowed" : "row-tap cursor-pointer"}`}>
                        <Checkbox checked={on} disabled={a.protected} onChange={() => toggle(a)} label={a.name} />
                        <span className="min-w-0 flex-1">
                          <span className={`block text-[15px] ${a.hidden ? "italic" : ""}`}>{a.name}</span>
                          <span className="sub block text-[13px]">{a.description || (a.hidden ? "Hidden achievement" : a.id)}</span>
                        </span>
                        {a.protected && (
                          <span title="Only the game's own servers can change this one" className="faint flex items-center gap-1 text-[12px]">
                            <Icon name="lock" className="size-3.5" />
                            Game only
                          </span>
                        )}
                        {changed && <Badge tone={on ? "good" : "bad"}>{on ? "Unlock" : "Lock"}</Badge>}
                      </label>
                    </li>
                  );
                })}
              </ul>
            )}
          </div>
          <p className="sub px-4 pt-2 text-[13px]">
            {protectedCount > 0 && `${protectedCount} marked “Game only” can only be changed by the game's own servers. `}
            Unlocked achievements show on your profile like any others, and you can lock them again.
          </p>
        </section>
      )}

      {changes.length > 0 && (
        <ActionBar>
          <button type="button" disabled={busy === "apply"} onClick={apply} className="btn-primary btn-lg w-full">
            {busy === "apply" ? "Saving…" : `Save · ${[unlock.length && `${unlock.length} to unlock`, lock.length && `${lock.length} to lock`].filter(Boolean).join(", ")}`}
          </button>
        </ActionBar>
      )}
    </Page>
  );
}

const INTERVALS = [15, 30, 60, 120, 360, 1440];

export function intervalLabel(m) {
  return m < 60 ? `${m} min` : m === 60 ? "hour" : m < 1440 ? `${m / 60} hours` : "day";
}

function IntervalPicker({ value, onChange }) {
  return (
    <div className="surface mt-4">
      <SelectRow label="One about every" value={value} onChange={(v) => onChange(Number(v))} options={INTERVALS.map((m) => [m, intervalLabel(m)])} />
    </div>
  );
}

// DripList is the games unlocking gradually, each with what's left.
function DripList({ drips }) {
  const [busy, run] = useAction();
  const now = Date.now();
  if (!drips?.length) return null;
  return (
    <Group title="Unlocking gradually">
      {drips.map((d) => {
        const mins = Math.max(0, Math.round((Date.parse(d.next) - now) / 60000));
        return (
          <div key={d.appId} className="row">
            <GameArt appId={d.appId} name={d.name} className="h-8 w-[75px]" />
            <span className="min-w-0 flex-1">
              <span className="block truncate">{d.name}</span>
              <span className="sub block truncate text-[13px]">
                {d.left} left · next {mins < 1 ? "any moment" : mins < 60 ? `in ${mins} min` : `in ${Math.round(mins / 60)} h`}
              </span>
            </span>
            <button type="button" disabled={!!busy} onClick={() => run("stop" + d.appId, () => api.post("/api/drip/stop", { app: d.appId }), `Stopped unlocking ${d.name}`)} className="btn btn-sm">
              Stop
            </button>
          </div>
        );
      })}
    </Group>
  );
}

// DripButton starts unlocking one game's achievements gradually.
function DripButton({ game }) {
  const [open, setOpen] = useState(false);
  const [every, setEvery] = useState(60);
  const [busy, run] = useAction();
  return (
    <>
      <button type="button" onClick={() => setOpen(true)} className="btn w-full">
        <Icon name="clock" className="size-[18px]" />
        Unlock gradually…
      </button>
      <Sheet open={open} onClose={() => setOpen(false)} title="Unlock gradually">
        <p className="sub text-[15px]">One achievement at a time in {game.name}, the most common first, so your profile looks like you played. It keeps going while houridle runs.</p>
        <IntervalPicker value={every} onChange={setEvery} />
        <button
          type="button"
          disabled={!!busy}
          onClick={() => run("drip", async () => { await api.post("/api/drip", { apps: [game.appId], minutes: every }); setOpen(false); }, { title: "Unlocking gradually", detail: `${game.name}: about one every ${intervalLabel(every)}.` })}
          className="btn-primary btn-lg mt-6 w-full"
        >
          Start
        </button>
      </Sheet>
    </>
  );
}
