import { useEffect, useMemo, useState } from "react";
import GameArt from "../components/GameArt.tsx";
import { Checkbox, Switch } from "../components/Checkbox.tsx";
import { ActionBar, Empty, ErrorState, Group, Icon, Page, Search, Segmented, SelectRow, Skeleton } from "../components/ui.tsx";
import { api } from "../lib/api.ts";
import { hours, plural } from "../lib/format.ts";
import { useAction, useToast } from "../components/toast.tsx";
import { useLibrary } from "../hooks/useLibrary.ts";
import { useDrops } from "../hooks/useDrops.ts";
import type { SegmentOption } from "../components/ui.tsx";
import type { Go } from "../App.tsx";
import type { IdleMode, LibraryGame, Panel, ScheduleInfo, Settings } from "../lib/types.ts";

const MODES: SegmentOption<IdleMode>[] = [
  { value: "games", label: "Pick games" },
  { value: "all", label: "Everything" },
  { value: "cards", label: "Card drops" },
];
const STATUSES: [string, string][] = [
  ["online", "Online"],
  ["away", "Away"],
  ["busy", "Busy"],
  ["invisible", "Invisible"],
  ["offline", "Offline"],
];

// The settings to start from, under what the panel last used.
const DEFAULTS: Settings = { mode: "games", games: [], until: "", rotate: "", status: "", claim: false, craft: false, events: false, refundable: false, stop: "" };

// Idle sets up idling: which account, which games, and how, then starts
// (or restarts) it. It's everything `houridle start` takes.
interface ScreenProps {
  panel: Panel;
  refresh: () => Promise<void>;
  goTo: Go;
}

export default function Idle({ panel, refresh, goTo }: ScreenProps) {
  const [st, setSt] = useState<Settings>(() => ({ ...DEFAULTS, ...panel.settings }));
  const [busy, run] = useAction();
  const signedIn = panel.signedIn;
  const set = (patch: Partial<Settings>) => setSt((s) => ({ ...s, ...patch }));

  // Follow the panel when it switches account (the saved list differs).
  useEffect(() => {
    setSt((s) => ({ ...s, games: panel.settings.games }));
  }, [panel.source]); // eslint-disable-line react-hooks/exhaustive-deps

  const start = () =>
    run(
      "start",
      async () => {
        await api.post("/api/idle/start", { ...st, games: st.mode === "games" ? st.games : [] });
        await refresh();
        goTo("now");
      },
      panel.idling ? "Restarted with the new settings" : "Started idling",
    );
  const mode = signedIn ? st.mode : "games";
  const blocked = mode === "games" && st.games.length === 0;
  const label = busy === "start" ? "Starting…" : blocked ? "Pick at least one game" : `${panel.idling ? "Restart" : "Start"} · ${mode === "games" ? `${plural(st.games.length, "game")}` : MODES.find((m) => m.value === mode)!.label}`;

  return (
    <Page title="Idle">
      <SourcePicker panel={panel} refresh={refresh} goTo={goTo} />
      {panel.source && (
        <>
          {signedIn && (
            <section>
              <h2 className="heading">What to idle</h2>
              <Segmented label="What to idle" options={MODES} value={st.mode} onChange={(m) => set({ mode: m, until: m === "cards" ? "" : st.until })} />
              {st.mode !== "games" && (
                <p className="sub px-4 pt-2.5 text-[13px]">
                  {st.mode === "all"
                    ? "Every game you own, 32 at a time, taking turns. Games you get later join by themselves."
                    : "The games with trading card drops left, read from your badge pages, until every card has dropped."}
                </p>
              )}
            </section>
          )}
          {mode === "games" && <GamePicker source={panel.source} picked={st.games} onChange={(games) => set({ games })} max={signedIn ? null : 32} signedIn={signedIn} />}
          <Options st={st} set={set} signedIn={signedIn} />
          <ScheduleGroup schedule={panel.schedule} refresh={refresh} />
          <ActionBar>
            <button type="button" disabled={busy === "start" || blocked} onClick={start} className="btn-primary btn-lg w-full">
              {!blocked && <Icon name="play" className="size-4" strokeWidth={2.4} />}
              {label}
            </button>
          </ActionBar>
        </>
      )}
    </Page>
  );
}

function SourcePicker({ panel, refresh, goTo }: ScreenProps) {
  const [busy, run] = useAction();
  const options = panel.accounts.filter((a) => !a.damaged).map((a) => ({ id: a.steamId, label: a.name }));
  if (panel.steamClient) options.push({ id: "client", label: "Steam client on this computer" });

  if (!options.length) {
    return (
      <div className="surface">
        <Empty icon="user" action={<button type="button" onClick={() => goTo("more", "accounts")} className="btn-primary">Sign in</button>}>
          No Steam account is signed in yet. Sign in with the Steam mobile app.
        </Empty>
      </div>
    );
  }
  const change = (id: string) =>
    run("use", async () => {
      await api.post("/api/accounts/use", { id });
      await refresh();
    }, "Switched account");
  return (
    <Group title="Account" footer={panel.idling && options.length > 1 ? "Switching account stops the current idling." : null}>
      <label className="row">
        <span className="grid size-8 shrink-0 place-items-center rounded-full bg-fill text-[13px] font-semibold">{panel.source === "client" ? <Icon name="monitor" className="size-4" /> : (options.find((o) => o.id === panel.source)?.label ?? "?").charAt(0).toUpperCase()}</span>
        <select value={panel.source} disabled={!!busy || options.length < 2} onChange={(e) => change(e.target.value)} aria-label="Account to idle with" className="min-w-0 flex-1 appearance-none truncate bg-transparent text-[15px] outline-none disabled:opacity-100">
          {!panel.source && <option value="">Choose…</option>}
          {options.map((o) => (
            <option key={o.id} value={o.id}>
              {o.label}
            </option>
          ))}
        </select>
        {options.length > 1 && <Icon name="down" className="faint size-4" strokeWidth={2.2} />}
      </label>
    </Group>
  );
}

const SORTS: [string, string][] = [
  ["played", "Most played"],
  ["least", "Least played"],
  ["drops", "Card drops left"],
  ["name", "Name"],
];

interface GamePickerProps {
  source: string;
  picked: number[];
  onChange: (games: number[]) => void;
  max: number | null;
  signedIn: boolean;
}

function GamePicker({ source, picked, onChange, max, signedIn }: GamePickerProps) {
  const { games, error, loading, reload: refetch } = useLibrary(source);
  const toast = useToast();
  const reload = async () => {
    const list = await refetch();
    if (list) toast(`Loaded ${list.length} games from Steam`, "good");
  };
  const [query, setQuery] = useState("");
  const [onlyPicked, setOnlyPicked] = useState(false);
  const [sort, setSort] = useState("played");
  const [topN, setTopN] = useState(10);
  const drops = useDrops(source, signedIn);
  const pickedSet = useMemo(() => new Set(picked), [picked]);

  const q = query.trim().toLowerCase();
  const shown = useMemo(() => {
    let list = games ?? [];
    // Picked games that aren't in the library (passed by app ID) still show.
    const inLibrary = new Set(list.map((g) => g.appId));
    const missing = picked.filter((id) => !inLibrary.has(id)).map((id) => ({ appId: id, name: `App ${id}`, minutes: 0 }));
    list = [...missing, ...list];
    if (onlyPicked) list = list.filter((g) => pickedSet.has(g.appId));
    if (q) list = list.filter((g) => g.name.toLowerCase().includes(q) || String(g.appId) === q);
    const d = (g: LibraryGame) => drops?.[g.appId] ?? 0;
    const sorts: Record<string, (a: LibraryGame, b: LibraryGame) => number> = {
      played: (a, b) => b.minutes - a.minutes,
      least: (a, b) => a.minutes - b.minutes,
      drops: (a, b) => d(b) - d(a) || b.minutes - a.minutes,
      name: (a, b) => a.name.localeCompare(b.name),
    };
    return [...list].sort(sorts[sort]);
  }, [games, picked, pickedSet, onlyPicked, q, sort, drops]);

  const toggle = (id: number) => onChange(pickedSet.has(id) ? picked.filter((x) => x !== id) : [...picked, id]);
  const atMax = !!max && picked.length >= max;
  const limit = max ?? 500;

  return (
    <section>
      <div className="flex items-end justify-between">
        <h2 className="heading">
          Games <span className="faint num">· {picked.length} picked</span>
        </h2>
        <div className="mb-1 flex items-center">
          {picked.length > 0 && (
            <button type="button" onClick={() => onChange([])} className="btn-ghost btn-sm h-7">
              Clear
            </button>
          )}
          <button type="button" onClick={reload} disabled={loading} aria-label="Reload the list from Steam" title="Reload the list from Steam" className="btn-ghost btn-sm h-7">
            <Icon name="refresh" className={`size-4 ${loading ? "animate-spin" : ""}`} strokeWidth={2} />
          </button>
        </div>
      </div>
      <div className="mb-2.5 space-y-2">
        <Search value={query} onChange={setQuery} placeholder="Search your games" />
        <div className="flex items-center gap-2">
          <select value={sort} onChange={(e) => setSort(e.target.value)} aria-label="Sort games" className="input h-8 w-auto min-w-0 flex-1 rounded-lg text-[13px]">
            {SORTS.filter(([v]) => v !== "drops" || signedIn).map(([v, l]) => (
              <option key={v} value={v}>
                {l}
              </option>
            ))}
          </select>
          <button type="button" onClick={() => setOnlyPicked(!onlyPicked)} aria-pressed={onlyPicked} className={`btn-sm shrink-0 ${onlyPicked ? "btn-primary" : "btn"}`}>
            Picked
          </button>
          <div className="flex h-8 shrink-0 items-center overflow-hidden rounded-lg border border-line bg-surface text-[13px]">
            <button type="button" onClick={() => onChange(shown.slice(0, topN).map((g) => g.appId))} className="h-full px-2.5 font-medium text-accent active:bg-fill">
              Pick top
            </button>
            <input
              type="number"
              min={1}
              max={limit}
              value={topN}
              onChange={(e) => setTopN(Math.max(1, Math.min(limit, Number(e.target.value) || 1)))}
              aria-label="How many of the top games to pick"
              className="num h-full w-11 border-l border-line bg-transparent text-center outline-none"
            />
          </div>
        </div>
      </div>
      {sort === "drops" && drops === null && <p className="sub mb-2 px-4 text-[13px]">Reading your card drops from Steam…</p>}
      <div className="surface overflow-hidden">
        {error ? (
          <ErrorState error={error} onRetry={reload} />
        ) : loading && !games ? (
          <Skeleton art rows={6} />
        ) : shown.length === 0 ? (
          <Empty icon="search">{q ? "No games match." : onlyPicked ? "Nothing picked yet." : "No games."}</Empty>
        ) : (
          <ul className="rows max-h-[30rem] overflow-y-auto overscroll-contain">
            {shown.slice(0, 300).map((g) => {
              const on = pickedSet.has(g.appId);
              const d = drops?.[g.appId];
              return (
                <li key={g.appId}>
                  <label className={`row row-tap cursor-pointer ${!on && atMax ? "opacity-40" : ""}`}>
                    <Checkbox checked={on} disabled={!on && atMax} onChange={() => toggle(g.appId)} label={g.name} />
                    <GameArt appId={g.appId} name={g.name} className="h-8 w-[75px]" />
                    <span className="min-w-0 flex-1">
                      <span className="block truncate text-[15px]">{g.name}</span>
                      <span className="sub block truncate text-[13px]">
                        {g.shared ? <span className="text-accent">Family · </span> : null}
                        {g.minutes ? hours(g.minutes * 60) : g.shared ? "Shared with you" : "Never played"}
                        {(d ?? 0) > 0 && <span className="text-accent"> · {d} card drop{d === 1 ? "" : "s"}</span>}
                      </span>
                    </span>
                  </label>
                </li>
              );
            })}
          </ul>
        )}
      </div>
      <p className="sub px-4 pt-2 text-[13px]">
        {shown.length > 300
          ? `Showing 300 of ${shown.length}. Search to narrow it down.`
          : atMax
            ? `Through the Steam client, at most ${max} games at once.`
            : !max && picked.length > 32
              ? `Steam counts 32 at a time, so these ${picked.length} take turns.`
              : null}
      </p>
    </section>
  );
}

function Options({ st, set, signedIn }: { st: Settings; set: (patch: Partial<Settings>) => void; signedIn: boolean }) {
  const text = "w-28 bg-transparent text-right text-[15px] outline-none placeholder:text-faint";
  return (
    <>
      <Group title="When to stop" footer="Hours (3) or a time (23:30). Games past their limit are left out.">
        <label className="row">
          <span className="flex-1">Stop idling</span>
          <input value={st.stop} onChange={(e) => set({ stop: e.target.value })} placeholder="Never" aria-label="Stop idling after" className={text} />
        </label>
        {signedIn && st.mode !== "cards" && (
          <label className="row">
            <span className="flex-1">Stop each game at</span>
            <input value={st.until} onChange={(e) => set({ until: e.target.value })} placeholder="No limit" inputMode="decimal" aria-label="Hours on record to stop each game at" className={text} />
          </label>
        )}
        {signedIn && (
          <label className="row">
            <span className="flex-1">
              Take turns every
              <span className="sub block text-[13px]">With more than 32 games</span>
            </span>
            <input value={st.rotate} onChange={(e) => set({ rotate: e.target.value })} placeholder="1h" aria-label="Take turns every" className={text} />
          </label>
        )}
      </Group>
      {signedIn && (
        <>
          <Group title="Profile">
            <SelectRow label="Friends see you as" value={st.status || "online"} onChange={(status) => set({ status })} options={STATUSES} />
          </Group>
          <Group title="While idling">
            <Toggle title="Claim free games" detail="Adds Steam's giveaways, checking every 12 hours" checked={st.claim} onChange={(claim) => set({ claim })} />
            <Toggle title="Craft badges" detail="Crafts a badge (Steam XP) once a card set is complete" checked={!!st.craft} onChange={(craft) => set({ craft })} />
            <Toggle title="Sale events" detail="Discovery queue and daily sticker during Steam sales" checked={!!st.events} onChange={(events) => set({ events })} />
            <Toggle
              title="Idle refundable games"
              detail="Off: games bought in the last 14 days with under 2 hours played wait, so you can still refund them"
              checked={!!st.refundable}
              onChange={(refundable) => set({ refundable })}
            />
          </Group>
        </>
      )}
    </>
  );
}

function Toggle({ title, detail, checked, onChange }: { title: string; detail: string; checked: boolean; onChange: (on: boolean) => void }) {
  return (
    <label className="row cursor-pointer">
      <span className="min-w-0 flex-1">
        <span className="block">{title}</span>
        <span className="sub block text-[13px]">{detail}</span>
      </span>
      <Switch checked={checked} onChange={onChange} label={title} />
    </label>
  );
}

const DAYS = ["M", "T", "W", "T", "F", "S", "S"];
const DAY_NAMES = ["Mondays", "Tuesdays", "Wednesdays", "Thursdays", "Fridays", "Saturdays", "Sundays"];

// ScheduleGroup limits idling to certain hours: houridle pauses when they
// end and picks up again when they start.
function ScheduleGroup({ schedule, refresh }: { schedule: ScheduleInfo; refresh: () => Promise<void> }) {
  const [s, setS] = useState(schedule);
  const [busy, run] = useAction();
  const dirty = JSON.stringify(s) !== JSON.stringify(schedule) && !busy;
  useEffect(() => {
    if (!busy) setS(schedule);
  }, [JSON.stringify(schedule)]); // eslint-disable-line react-hooks/exhaustive-deps
  const save = (next: ScheduleInfo) =>
    run("schedule", async () => {
      await api.post("/api/schedule", next);
      await refresh();
    }, next.on ? "Schedule saved" : "Schedule off");
  const days = s.days.every(Boolean) ? "Every day" : s.days.filter(Boolean).length === 0 ? "No days" : s.days.map((d, i) => (d ? DAY_NAMES[i].slice(0, 3) : null)).filter(Boolean).join(", ");
  return (
    <Group
      title="Schedule"
      footer={
        s.on
          ? `${days}, ${s.start}–${s.end}${s.end < s.start ? " (past midnight)" : ""}. Outside these hours idling pauses, then picks up again by itself.`
          : "Idle only at certain times, such as overnight, so you're not shown in-game while you play."
      }
    >
      <label className="row cursor-pointer">
        <span className="flex-1">Only idle at certain times</span>
        <Switch checked={s.on} onChange={(on) => (on ? setS({ ...s, on }) : save({ ...s, on }))} label="Only idle at certain times" />
      </label>
      {s.on && (
        <>
          <div className="row">
            <span className="flex-1">From</span>
            <input type="time" value={s.start} onChange={(e) => setS({ ...s, start: e.target.value })} aria-label="From" className="input h-8 w-auto rounded-lg px-2 text-[15px]" />
            <span className="sub">to</span>
            <input type="time" value={s.end} onChange={(e) => setS({ ...s, end: e.target.value })} aria-label="To" className="input h-8 w-auto rounded-lg px-2 text-[15px]" />
          </div>
          <div className="row justify-between">
            {DAYS.map((d, i) => (
              <button
                key={i}
                type="button"
                aria-pressed={s.days[i]}
                aria-label={DAY_NAMES[i]}
                onClick={() => setS({ ...s, days: s.days.map((v, j) => (j === i ? !v : v)) })}
                className={`grid size-9 place-items-center rounded-full text-[13px] font-semibold transition ${s.days[i] ? "bg-accent text-white" : "bg-fill sub"}`}
              >
                {d}
              </button>
            ))}
          </div>
          {(dirty || !schedule.on) && (
            <div className="p-3">
              <button type="button" disabled={!!busy} onClick={() => save(s)} className="btn-primary w-full">
                {busy ? "Saving…" : "Save schedule"}
              </button>
            </div>
          )}
        </>
      )}
    </Group>
  );
}
