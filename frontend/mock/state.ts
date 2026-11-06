// The test houridle's memory: everything the page can change lives here, so
// clicking around behaves like the real thing until `npm run dev` restarts.
import type { Achievement, BadgeInfo, BoosterList, Deal, DripInfo, ExtraInfo, FreeGame, Game, GameState, Inventory, Level, Panel, Settings, Shop, Stats, Status, StatusEvent } from "../src/lib/types.ts";
import * as data from "./data.ts";
import type { CatalogGame, FreeToPlayEntry } from "./data.ts";

export type Scenario = "panel" | "status" | "big" | "empty" | "offline" | "unauthorized";
export const SCENARIOS: Scenario[] = ["panel", "status", "big", "empty", "offline", "unauthorized"];

const HISTORY_DAYS = 84; // like houridle: 12 weeks for the calendar
const VERSION = "1.6.2";

// A game in the current run. Idling games count up from `since`.
interface RunGame {
  appId: number;
  name: string;
  state: GameState;
  note?: string;
  idled: number; // seconds, up to `since`
  since?: number; // ms; set while idling
  total: number; // seconds on record when the run started
  target?: number;
  drops?: number;
}

export interface MockState {
  scenario: Scenario;
  stopped: boolean; // Stop or Quit was pressed
  restarting?: boolean; // and it's coming back
  started: number;
  stopAt?: number;
  games: RunGame[];
  events: StatusEvent[];
  days: number[]; // seconds idled per day, oldest first
  since?: number; // first day on record, in days ago
  panel: Panel;
  libraries: Record<string, CatalogGame[]>; // by source
  gamesBySource: Record<string, number[]>; // the last picked games, by source
  achievements: Record<number, Achievement[]>;
  owned: Record<number, number[]>; // cards of each set
  level: Level;
  shop: Shop;
  deals: Deal[];
  inventory: Inventory;
  boosters: BoosterList;
  freeGames: FreeGame[];
  f2p: FreeToPlayEntry[];
  loginPolls: number;
}

const iso = (ms: number) => new Date(ms).toISOString();

export function createState(scenario: Scenario, now = Date.now()): MockState {
  const empty = scenario === "empty";
  const big = scenario === "big";
  const settings: Settings = {
    mode: big ? "all" : "games",
    games: empty ? [] : [620, 105600, 413150, 367520, 1145360, 292030, 268910, 504230],
    until: "",
    rotate: "",
    status: "",
    claim: true,
    craft: true,
    events: false,
    stop: "",
    refundable: false,
  };
  const accounts = [{ steamId: data.MAIN_ID, name: data.MAIN_NAME, default: true, expires: iso(now + 151 * 864e5) }];
  if (!empty) accounts.push({ steamId: data.ALT_ID, name: data.ALT_NAME, default: false, expires: iso(now + 38 * 864e5) });

  const panel: Panel = {
    idling: !empty,
    source: data.MAIN_ID,
    signedIn: true,
    steamClient: !empty,
    settings,
    accounts,
    notify: !empty,
    notifyKind: "discord",
    webAddr: ":8095",
    login: { state: "" },
    schedule: { on: false, start: "22:00", end: "08:00", days: [true, true, true, true, true, true, true] },
    drip: empty ? [] : [{ appId: 413150, name: "Stardew Valley", every: 60, next: iso(now + 23 * 60e3), left: 3 }],
    extras: empty ? [] : [{ steamId: data.ALT_ID, name: data.ALT_NAME, mode: "cards", on: true, idling: 3, games: 3, idled: 2 * 3600 + 840 }],
    weekly: !empty,
    update: { current: VERSION, latest: "1.7.0", available: !empty, auto: false, checked: iso(now - 2 * 3600e3), published: iso(now - 3 * 864e5), notes: "- Signing in survives Steam's server moves\n- The page loads faster on phones\n- Card drops are read less often, so Steam answers quicker" },
    standing: { vacBans: 0, gameBans: 0, daysSinceBan: -1, tradeBan: "None", limited: false, market: true, checked: iso(now - 5 * 3600e3), ok: true },
    steam: { protocol: 65580, client: 1731024000, fromRepo: true, build: "1731024000", checked: iso(now - 7 * 3600e3) },
  };

  const s: MockState = {
    scenario,
    stopped: false,
    started: now,
    games: [],
    events: empty ? [] : data.events(now),
    days: empty ? Array<number>(HISTORY_DAYS).fill(0) : data.dailySeconds(HISTORY_DAYS, 41),
    since: empty ? undefined : 41,
    panel,
    libraries: empty ? {} : { [data.MAIN_ID]: big ? data.bigLibrary(640) : [...data.LIBRARY], client: [...data.LIBRARY], [data.ALT_ID]: [...data.ALT_LIBRARY] },
    gamesBySource: { [data.MAIN_ID]: settings.games, [data.ALT_ID]: [105600, 646570] },
    achievements: empty ? {} : { 620: data.achievementsFor(620), 105600: data.achievementsFor(105600), 413150: data.achievementsFor(413150) },
    owned: empty ? {} : structuredClone(data.OWNED),
    level: empty
      ? { level: 1, xp: 0, levelXp: 0, nextXp: 100, badges: 0, ready: 0, partial: 0, missing: 0, dropsXp: 0 }
      : { level: 23, xp: 2280, levelXp: 2100, nextXp: 2400, badges: 31, ready: 1, partial: 4, missing: 9, dropsXp: 300, unlocked: 412 },
    shop: { balance: empty ? 0 : 1234, currency: 1, allowed: true, cap: 500, spentToday: 0, badges: empty ? [] : data.shopBadges() },
    deals: [...data.DEALS],
    inventory: empty ? { gems: 0, cards: 0, value: 0, spare: 0, boosters: 0, games: [], priced: true } : inventory(data.inventoryGames(), 3450, 2),
    boosters: { gems: empty ? 0 : 3450, boosters: empty ? [] : data.boosters(now) },
    freeGames: data.FREE_GAMES.map((g) => ({ ...g })),
    f2p: data.F2P.map((g) => ({ ...g })),
    loginPolls: 0,
  };
  if (!empty) {
    startRun(s, now);
    // A run that's been going a while (since about 3 hours ago).
    s.started = now - 190 * 60e3;
    for (const g of s.games) if (g.since) g.since = s.started;
    const set = (appId: number, state: GameState, note?: string) => {
      const g = s.games.find((x) => x.appId === appId);
      if (g) Object.assign(g, { state, note, since: undefined, idled: state === "done" ? 2 * 3600 : 0 });
    };
    set(268910, "queued");
    set(504230, "done");
    if (scenario === "status") s.stopAt = now + 5 * 3600e3; // houridle start -web -stop 5h
  }
  return s;
}

// inventory totals up the per-game card groups.
export function inventory(games: Inventory["games"], gems: number, boosters: number): Inventory {
  return {
    gems,
    cards: games.reduce((t, g) => t + g.cards, 0),
    value: games.reduce((t, g) => t + g.value, 0),
    spare: games.reduce((t, g) => t + g.spare, 0),
    boosters,
    games: [...games].sort((a, b) => b.value - a.value),
    priced: true,
  };
}

export function library(s: MockState): CatalogGame[] {
  return s.libraries[s.panel.source] ?? [];
}

// seconds reads "100" (hours), "90m" or "2h" as seconds; 0 if it can't.
export function seconds(text: string): number {
  const m = /^\s*(\d+(?:\.\d+)?)\s*(h|m)?\s*$/i.exec(text);
  if (!m) return 0;
  return Math.round(Number(m[1]) * (m[2]?.toLowerCase() === "m" ? 60 : 3600));
}

// stopTime reads the "stop idling" box: hours from now, or a time of day.
function stopTime(text: string, now: number): number | undefined {
  const at = /^\s*(\d{1,2}):(\d{2})\s*$/.exec(text);
  if (at) {
    const d = new Date(now);
    d.setHours(Number(at[1]), Number(at[2]), 0, 0);
    if (d.getTime() <= now) d.setDate(d.getDate() + 1);
    return d.getTime();
  }
  const s = seconds(text);
  return s ? now + s * 1000 : undefined;
}

// startRun starts idling with the panel's settings.
export function startRun(s: MockState, now = Date.now()): void {
  const st = s.panel.settings;
  const lib = library(s);
  const picked: CatalogGame[] =
    st.mode === "all"
      ? lib
      : st.mode === "cards"
        ? lib.filter((g) => g.drops > 0)
        : st.games.map((id) => lib.find((g) => g.appId === id) ?? { appId: id, name: `App ${id}`, minutes: 0, drops: 0 });
  const target = st.mode === "cards" ? 0 : seconds(st.until);
  let idling = 0;
  s.games = picked.map((g): RunGame => {
    const base = { appId: g.appId, name: g.name, idled: 0, total: g.minutes * 60, target: target || undefined, drops: g.drops || undefined };
    if (g.refundable && !st.refundable) return { ...base, state: "refundable", note: "refundable for 13 days" };
    if (target && base.total >= target) return { ...base, state: "done" };
    if (idling >= 32) return { ...base, state: "queued" };
    idling++;
    return { ...base, state: "idling", since: now };
  });
  s.started = now;
  s.stopAt = stopTime(st.stop, now);
  s.panel.idling = true;
}

export function stopRun(s: MockState): void {
  s.panel.idling = false;
  s.games = [];
  s.stopAt = undefined;
}

export function addEvent(s: MockState, level: StatusEvent["level"], text: string, kind?: StatusEvent["kind"]): void {
  s.events.push(kind ? { at: iso(Date.now()), level, text, kind } : { at: iso(Date.now()), level, text });
  s.events = s.events.slice(-100);
}

// badges is the badge page: complete sets, then the closest to complete.
export function badges(s: MockState): { ready: BadgeInfo[]; closest: BadgeInfo[] } {
  const all = Object.entries(s.owned).map(([id, owned]): BadgeInfo => {
    const appId = Number(id);
    const total = data.cardNames(appId).length;
    return { appId, name: data.nameOf(appId), collected: owned.filter((n) => n > 0).length, total, market: data.marketSearch(appId), foil: appId === 105600 ? true : undefined };
  });
  return {
    ready: all.filter((b) => b.collected === b.total),
    closest: all.filter((b) => b.collected < b.total).sort((a, b) => b.collected / b.total - a.collected / a.total),
  };
}

function stats(s: MockState, now: number): Stats {
  const days = s.days.map((time, i) => {
    const d = new Date(now - (s.days.length - 1 - i) * 864e5);
    const day = `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;
    return { day, time };
  });
  const sum = (n: number) => s.days.slice(-n).reduce((t, x) => t + x, 0);
  const all = sum(s.days.length);
  const week = sum(7);
  const weights = [0.27, 0.21, 0.15, 0.12, 0.09, 0.07, 0.05, 0.04];
  const games = all ? data.LIBRARY.slice(0, weights.length).map((g, i) => ({ appId: g.appId, name: g.name, week: Math.round(week * weights[i]), all: Math.round(all * weights[i]) })) : [];
  return { today: sum(1), week, month: sum(30), all, since: s.since === undefined ? undefined : days[days.length - 1 - s.since].day, days, games };
}

// schedule fills in where the idling hours leave things now.
function schedule(s: MockState, now: number): Panel["schedule"] {
  const sc = s.panel.schedule;
  if (!sc.on) return { on: false, start: sc.start, end: sc.end, days: sc.days };
  const at = (hm: string, dayOffset: number) => {
    const [h, m] = hm.split(":").map(Number);
    const d = new Date(now);
    d.setDate(d.getDate() + dayOffset);
    d.setHours(h, m, 0, 0);
    return d.getTime();
  };
  const mins = new Date(now).getHours() * 60 + new Date(now).getMinutes();
  const toMin = (hm: string) => Number(hm.slice(0, 2)) * 60 + Number(hm.slice(3, 5));
  const [start, end] = [toMin(sc.start), toMin(sc.end)];
  const active = start <= end ? mins >= start && mins < end : mins >= start || mins < end;
  const next = active ? at(sc.end, mins >= toMin(sc.end) ? 1 : 0) : at(sc.start, mins >= start ? 1 : 0);
  return { ...sc, active: active || undefined, paused: !active || undefined, next: iso(next) };
}

export function status(s: MockState, now = Date.now()): Status {
  const games: Game[] = s.games.map((g) => {
    const idled = g.idled + (g.since ? Math.floor((now - g.since) / 1000) : 0);
    return { appId: g.appId, name: g.name, state: g.state, note: g.note, idled, total: g.total + idled || undefined, target: g.target, drops: g.drops };
  });
  const st = s.panel.settings;
  const running = s.panel.idling || s.scenario === "status";
  const out: Status = {
    version: s.panel.update.current,
    account: s.panel.source === "client" ? "vperk (Steam client)" : (s.panel.accounts.find((a) => a.steamId === s.panel.source)?.name ?? ""),
    started: iso(running ? s.started : now),
    stopAt: s.stopAt ? iso(s.stopAt) : undefined,
    games,
    events: s.events,
    stats: stats(s, now),
    run: running
      ? { signedIn: s.panel.signedIn, cards: st.mode === "cards" || undefined, all: st.mode === "all" || undefined, claim: st.claim || undefined, target: seconds(st.until) || undefined, rotate: seconds(st.rotate) || undefined, status: s.panel.signedIn ? st.status || "online" : undefined, craft: st.craft || undefined, events: st.events || undefined }
      : { signedIn: s.panel.signedIn },
  };
  if (s.scenario !== "status") out.panel = { ...s.panel, schedule: schedule(s, now) };
  return JSON.parse(JSON.stringify(out)) as Status; // drops the undefined fields, like Go's omitempty
}

export function drip(s: MockState, appId: number, minutes: number, now = Date.now()): DripInfo {
  const left = (s.achievements[appId] ?? []).filter((a) => !a.unlocked && !a.protected).length || 8;
  return { appId, name: data.nameOf(appId), every: minutes, next: iso(now + minutes * 60e3), left };
}

export function extra(s: MockState, steamId: string, mode: string, on: boolean): ExtraInfo {
  const lib = s.libraries[steamId] ?? [];
  const count = mode === "cards" ? lib.filter((g) => g.drops > 0).length : lib.length;
  const name = s.panel.accounts.find((a) => a.steamId === steamId)?.name ?? steamId;
  return { steamId, name, mode, on, idling: on ? count : 0, games: count, idled: 0 };
}
