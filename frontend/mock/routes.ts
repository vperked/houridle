// The test houridle's API: every endpoint the page calls, answering like
// backend/views/web (control.go, more.go, server.go) with test data.
import type {
  AchievementsSaved, Art, BadgesInfo, BoosterList, Bought, BulkAchievements, BulkResult, CardInfo, Crafted, Deals, Drops, Explored, FreeAdded, FreeGame,
  FreeToPlay, GameBought, Ground, Inventory, Level, LibraryGame, Login, NewLinks, Quote, ScheduleInfo, Settings, Shop, Standing, Status, UpdateInfo, WeeklySent,
} from "../src/lib/types.ts";
import * as data from "./data.ts";
import { addEvent, badges, drip, extra, inventory, library, startRun, status, stopRun } from "./state.ts";
import type { MockState } from "./state.ts";

// HttpError is an answer other than 200: a bad request (400) or something
// houridle couldn't do (409), sent as {"error": "..."} like the real one.
export class HttpError extends Error {
  constructor(
    readonly code: number,
    message: string,
  ) {
    super(message);
  }
}
const bad = (msg: string) => new HttpError(400, msg);
const refuse = (msg: string) => new HttpError(409, msg);

export type Body = Record<string, unknown>;
export interface Request {
  s: MockState;
  body: Body;
  query: URLSearchParams;
}
type Handler = (r: Request) => unknown;

// Reading the request's JSON, with the zero values Go would decode.
const num = (b: Body, k: string): number => {
  const v = b[k];
  return typeof v === "number" ? v : 0;
};
const str = (b: Body, k: string): string => {
  const v = b[k];
  return typeof v === "string" ? v : "";
};
const bool = (b: Body, k: string): boolean => b[k] === true;
const nums = (b: Body, k: string): number[] => {
  const v = b[k];
  return Array.isArray(v) ? v.filter((x): x is number => typeof x === "number") : [];
};
const strs = (b: Body, k: string): string[] => {
  const v = b[k];
  return Array.isArray(v) ? v.filter((x): x is string => typeof x === "string") : [];
};
function appParam(q: URLSearchParams): number {
  const id = Number(q.get("app"));
  if (!Number.isInteger(id) || id <= 0) throw bad("pick a game");
  return id;
}

const ok = undefined; // answered as {"ok": true}

function spend(s: MockState, cents: number): void {
  const left = s.shop.cap - s.shop.spentToday;
  if (s.shop.cap === 0) throw refuse("buying is off: set a daily limit first");
  if (cents > left) throw refuse(`that's over today's limit ($${(left / 100).toFixed(2)} left)`);
  if (cents > s.shop.balance) throw refuse("the Steam wallet doesn't have enough");
  s.shop.balance -= cents;
  s.shop.spentToday += cents;
}

function addToLibrary(s: MockState, appId: number, drops = 0): void {
  for (const source of [s.panel.source, ...(s.panel.source === data.MAIN_ID ? ["client"] : [])]) {
    const lib = (s.libraries[source] ??= []);
    if (!lib.some((g) => g.appId === appId)) lib.push({ appId, name: data.nameOf(appId), minutes: 0, drops });
  }
}

function settingsFrom(b: Body, old: Settings): Settings {
  const mode = str(b, "mode");
  return {
    mode: mode === "all" || mode === "cards" ? mode : "games",
    games: nums(b, "games"),
    until: str(b, "until"),
    rotate: str(b, "rotate"),
    status: str(b, "status"),
    claim: bool(b, "claim"),
    craft: bool(b, "craft"),
    events: bool(b, "events"),
    stop: str(b, "stop"),
    refundable: b.refundable === undefined ? old.refundable : bool(b, "refundable"),
  };
}

// The fake QR code: a pattern that looks the part (it's not scannable).
function qrSVG(): string {
  let seed = 11;
  const cells: string[] = [];
  for (let y = 0; y < 25; y++)
    for (let x = 0; x < 25; x++) {
      const finder = (x < 7 || x > 17) && y < 7 ? true : x < 7 && y > 17;
      seed = (seed * 16807) % 2147483647;
      if (finder ? x % 6 === 0 || y % 6 === 0 || (x % 6 > 1 && x % 6 < 5 && y % 6 > 1 && y % 6 < 5) : seed % 2 === 0) cells.push(`<rect x="${x}" y="${y}" width="1" height="1"/>`);
    }
  return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 25 25" shape-rendering="crispEdges">${cells.join("")}</svg>`;
}

export const routes: Record<string, Handler> = {
  // ---------- status (server.go) ----------
  "GET /api/status": ({ s }): Status => status(s),
  "POST /api/stop": ({ s }) => {
    s.stopped = true;
    return ok;
  },
  "GET /api/art": ({ query }): Record<string, Art> => {
    const ids = (query.get("apps") ?? "").split(",").map((x) => Number(x.trim())).filter((id) => Number.isInteger(id) && id > 0);
    if (ids.length === 0 || ids.length > 300) throw bad("ask for 1 to 300 games");
    return Object.fromEntries(ids.map((id) => [String(id), data.KNOWN_APPS.has(id) ? data.art(id) : {}]));
  },

  // ---------- idling and accounts (control.go) ----------
  "POST /api/idle/start": ({ s, body }) => {
    if (!s.panel.source) throw refuse("sign in to Steam first");
    const st = settingsFrom(body, s.panel.settings);
    if (st.mode === "games" && st.games.length === 0) throw refuse("pick some games to idle");
    s.panel.settings = st;
    if (st.mode === "games") s.gamesBySource[s.panel.source] = st.games;
    startRun(s);
    addEvent(s, "info", `Idling ${s.games.filter((g) => g.state === "idling").length} games`);
    return ok;
  },
  "POST /api/idle/stop": ({ s }) => {
    stopRun(s);
    addEvent(s, "info", "Stopped idling");
    return ok;
  },
  "POST /api/quit": ({ s }) => {
    s.stopped = true;
    return ok;
  },
  "POST /api/accounts/use": ({ s, body }) => {
    const id = str(body, "id");
    const known = id === "client" ? s.panel.steamClient : s.panel.accounts.some((a) => a.steamId === id && !a.damaged);
    if (!known) throw refuse("there's no such account");
    if (id === s.panel.source) return ok;
    if (s.panel.idling) stopRun(s);
    s.panel.source = id;
    s.panel.signedIn = id !== "client";
    s.panel.settings = { ...s.panel.settings, games: s.gamesBySource[id] ?? [] };
    if (!s.panel.signedIn) s.panel.settings.mode = "games";
    s.panel.extras = s.panel.accounts.filter((a) => a.steamId !== id).map((a) => s.panel.extras.find((e) => e.steamId === a.steamId) ?? extra(s, a.steamId, "cards", false));
    return ok;
  },
  "POST /api/accounts/default": ({ s, body }) => {
    const id = str(body, "id");
    if (!s.panel.accounts.some((a) => a.steamId === id)) throw refuse("there's no such account");
    for (const a of s.panel.accounts) a.default = a.steamId === id;
    return ok;
  },
  "POST /api/accounts/logout": ({ s, body }) => {
    const id = str(body, "id");
    s.panel.accounts = s.panel.accounts.filter((a) => a.steamId !== id);
    s.panel.extras = s.panel.extras.filter((e) => e.steamId !== id);
    if (s.panel.accounts.length && !s.panel.accounts.some((a) => a.default)) s.panel.accounts[0].default = true;
    if (s.panel.source === id) {
      stopRun(s);
      s.panel.source = s.panel.accounts[0]?.steamId ?? (s.panel.steamClient ? "client" : "");
      s.panel.signedIn = !!s.panel.source && s.panel.source !== "client";
      s.panel.settings = { ...s.panel.settings, games: s.gamesBySource[s.panel.source] ?? [] };
    }
    return ok;
  },
  "POST /api/login": ({ s }): Login => {
    s.loginPolls = 0;
    s.panel.login = { state: "waiting", url: "https://s.team/q/1/4628351906718273645", qr: qrSVG() };
    return s.panel.login;
  },
  // The test sign-in is approved on the third look (about 6 seconds).
  "GET /api/login": ({ s }): Login => {
    if (s.panel.login.state === "waiting" && ++s.loginPolls >= 3) {
      const id = String(76561198000000000 + 4200 + s.panel.accounts.length);
      const name = `test_account_${s.panel.accounts.length + 1}`;
      s.panel.accounts.push({ steamId: id, name, default: s.panel.accounts.length === 0, expires: new Date(Date.now() + 200 * 864e5).toISOString() });
      s.libraries[id] = [...data.ALT_LIBRARY];
      if (!s.panel.source) {
        s.panel.source = id;
        s.panel.signedIn = true;
      }
      s.panel.login = { state: "done", account: name };
      addEvent(s, "good", `Signed in to Steam as ${name}`);
    }
    return s.panel.login;
  },
  "POST /api/notify": ({ s, body }) => {
    const url = str(body, "url").trim();
    if (url && !/^https?:\/\/\S+$/.test(url)) throw refuse("that isn't a webhook address (it should start with https://)");
    s.panel.notify = !!url;
    s.panel.notifyKind = /discord(app)?\.com/.test(url) ? "discord" : url.includes("hooks.slack.com") ? "slack" : "text";
    return ok;
  },
  "POST /api/notify/test": ({ s }) => {
    if (!s.panel.notify) throw refuse("alerts are off");
    return ok;
  },
  "POST /api/webkey": (): NewLinks => ({ links: [`http://192.168.1.20:8095/?key=${Math.random().toString(36).slice(2, 14)}${Math.random().toString(36).slice(2, 14)}`] }),

  // ---------- library, achievements, drops ----------
  "GET /api/library": ({ s }): LibraryGame[] | null => {
    const lib = library(s);
    if (lib.length === 0) return null; // Go's nil slice
    return lib.map((g) => ({ appId: g.appId, name: g.name, minutes: g.minutes, shared: g.shared })).sort((a, b) => b.minutes - a.minutes || a.name.localeCompare(b.name));
  },
  "GET /api/drops": ({ s }): Drops => Object.fromEntries(library(s).filter((g) => g.drops > 0).map((g) => [g.appId, g.drops])),
  "GET /api/achievements": ({ s, query }) => s.achievements[appParam(query)] ?? [],
  "POST /api/achievements": ({ s, body }): AchievementsSaved => {
    const app = num(body, "app");
    const list = s.achievements[app] ?? [];
    const failed: Record<string, string> = {};
    let unlocked = 0;
    const set = (ids: string[], on: boolean) => {
      for (const id of ids) {
        const a = list.find((x) => x.id === id);
        if (!a) continue;
        if (a.protected) failed[id] = "only the game's servers can change it";
        else {
          if (on && !a.unlocked) unlocked++;
          a.unlocked = on;
        }
      }
    };
    set(strs(body, "unlock"), true);
    set(strs(body, "lock"), false);
    if (unlocked) addEvent(s, "good", `${data.nameOf(app)}: ${unlocked} achievement${unlocked === 1 ? "" : "s"} unlocked`, "achievements");
    return { failed: Object.keys(failed).length ? failed : null };
  },
  "POST /api/achievements/bulk": ({ s, body }): BulkResult => {
    const apps = nums(body, "apps");
    if (apps.length === 0) throw bad("pick at least one game");
    const unlock = bool(body, "unlock");
    return {
      games: apps.map((appId): BulkAchievements => {
        const list = s.achievements[appId] ?? [];
        const open = list.filter((a) => !a.protected);
        const changing = open.filter((a) => a.unlocked !== unlock);
        for (const a of changing) a.unlocked = unlock;
        return { appId, name: data.nameOf(appId), total: list.length, changed: changing.length, already: open.length - changing.length, gameOnly: list.length - open.length, failed: 0 };
      }),
    };
  },
  "POST /api/drip": ({ s, body }) => {
    const apps = nums(body, "apps");
    const minutes = num(body, "minutes");
    if (apps.length === 0 || minutes < 5 || minutes > 7 * 24 * 60) throw bad("pick games and an interval between 5 minutes and a week");
    s.panel.drip = [...s.panel.drip.filter((d) => !apps.includes(d.appId)), ...apps.map((id) => drip(s, id, minutes))];
    return ok;
  },
  "POST /api/drip/stop": ({ s, body }) => {
    s.panel.drip = s.panel.drip.filter((d) => d.appId !== num(body, "app"));
    return ok;
  },

  // ---------- badges, level, inventory ----------
  "GET /api/level": ({ s }): Level => {
    const b = badges(s);
    return { ...s.level, ready: b.ready.length, partial: b.closest.length, missing: b.closest.reduce((t, x) => t + x.total - x.collected, 0) };
  },
  "GET /api/badges": ({ s }): BadgesInfo => badges(s),
  "GET /api/badges/cards": ({ s, query }): CardInfo[] => {
    const app = appParam(query);
    return data.cardsOf(app, s.owned[app] ?? []);
  },
  "POST /api/badges/craft": ({ s }): Crafted => {
    const crafted = badges(s).ready;
    for (const b of crafted) {
      s.owned[b.appId] = s.owned[b.appId].map((n) => n - 1);
      s.level.xp += 100;
      s.level.badges++;
      while (s.level.xp >= s.level.nextXp) {
        s.level.level++;
        s.level.levelXp = s.level.nextXp;
        s.level.nextXp += 300;
      }
      addEvent(s, "good", `Crafted the ${b.name} badge (+100 XP)`, "craft");
    }
    return { crafted: crafted.map((b) => b.name) };
  },
  "POST /api/events/explore": ({ s }): Explored => {
    const sticker = !s.events.some((e) => e.text.startsWith("Claimed today's sale sticker"));
    addEvent(s, "good", sticker ? "Claimed today's sale sticker" : "Explored the discovery queue");
    return { games: 12, sticker };
  },
  "GET /api/inventory": ({ s }): Inventory => s.inventory,
  "POST /api/gems/grind": ({ s, body }): Ground => {
    const apps = nums(body, "apps");
    if (apps.length === 0) throw bad("pick at least one game");
    let cards = 0;
    let gems = 0;
    const games = s.inventory.games.map((g) => {
      if (!apps.includes(g.appId) || !g.spare || !g.gems) return g;
      cards += g.spare;
      gems += g.spare * g.gems;
      return { ...g, cards: g.cards - g.spare, value: Math.round((g.value * (g.cards - g.spare)) / g.cards), spare: 0 };
    });
    s.inventory = inventory(games, s.inventory.gems + gems, s.inventory.boosters);
    s.boosters.gems = s.inventory.gems;
    return { cards, gems };
  },
  "GET /api/boosters": ({ s }): BoosterList => s.boosters,
  "POST /api/boosters/make": ({ s, body }) => {
    const app = num(body, "app");
    const b = s.boosters.boosters.find((x) => x.appId === app);
    if (!b || num(body, "price") <= 0) throw bad("pick a booster pack");
    if (!b.available) throw refuse(`Steam allows one ${b.name} pack a day`);
    if (b.price > s.boosters.gems) throw refuse("not enough gems");
    s.boosters.gems -= b.price;
    s.inventory.gems = s.boosters.gems;
    s.inventory.boosters++;
    Object.assign(b, { available: false, availableAt: "tomorrow" });
    return ok;
  },

  // ---------- buying (shop and deals) ----------
  "GET /api/shop": ({ s }): Shop => s.shop,
  "GET /api/shop/quote": ({ s, query }): Quote => {
    const app = appParam(query);
    const b = s.shop.badges.find((x) => x.appId === app);
    if (!b) throw refuse("that badge has nothing left to buy");
    const per = Math.round(b.estimate / b.missing.length);
    const items = b.missing.map((card, i) => ({ card, price: per + ((i * 7) % 5) - 2, market: `https://steamcommunity.com/market/listings/753/${app}-${encodeURIComponent(card)}` }));
    return { appId: app, name: b.name, items, total: items.reduce((t, it) => t + it.price, 0), currency: s.shop.currency };
  },
  "POST /api/shop/buy": ({ s, body }): Bought => {
    const app = num(body, "app");
    const total = num(body, "total");
    if (!app || total <= 0) throw bad("get a quote first");
    const b = s.shop.badges.find((x) => x.appId === app);
    if (!b) throw refuse("that badge has nothing left to buy");
    spend(s, total);
    s.shop.badges = s.shop.badges.filter((x) => x !== b);
    s.owned[app] = (s.owned[app] ?? data.cardNames(app).map(() => 0)).map((n) => Math.max(n, 1));
    return { cards: b.missing, spent: total };
  },
  "POST /api/shop/cap": ({ s, body }) => {
    const cents = num(body, "cents");
    if (cents < 0 || cents > 100000) throw bad("the daily limit should be between 0 and 1000");
    s.shop.cap = cents;
    return ok;
  },
  "GET /api/deals": ({ s, query }): Deals => {
    const max = Number(query.get("max")) || 100;
    const count = Number(query.get("count")) || 12;
    const owned = new Set(library(s).map((g) => g.appId));
    const sorts: Record<string, (a: Deals["deals"][number], b: Deals["deals"][number]) => number> = {
      xp: (a, b) => b.xpPerDollar - a.xpPerDollar,
      price: (a, b) => a.price - b.price,
      payback: (a, b) => b.dropValue / b.price - a.dropValue / a.price,
      reviews: (a, b) => (b.review.positive ?? 0) - (a.review.positive ?? 0),
    };
    const deals = s.deals
      .filter((d) => !owned.has(d.appId) && d.price <= max && (query.get("reviews") === "0" || (d.review.positive ?? 0) >= 80))
      .sort(sorts[query.get("sort") ?? "xp"] ?? sorts.xp)
      .slice(0, count);
    return { balance: s.shop.balance, currency: s.shop.currency, cap: s.shop.cap, spentToday: s.shop.spentToday, deals };
  },
  "POST /api/deals/buy": ({ s, body }): GameBought => {
    const app = num(body, "app");
    const price = num(body, "price");
    if (!app || price <= 0) throw bad("pick a game first");
    const d = s.deals.find((x) => x.appId === app);
    if (!d) throw refuse("that game isn't on sale any more");
    if (d.price > price) throw refuse("the price went up; look again");
    spend(s, d.price);
    addToLibrary(s, app, d.drops);
    addEvent(s, "good", `Bought ${d.name}`);
    return { appId: app, name: d.name, bought: true, spent: d.price, currency: s.shop.currency };
  },

  // ---------- free games ----------
  "GET /api/freegames": ({ s }): FreeGame[] => s.freeGames,
  "POST /api/freegames/claim": ({ s }): FreeGame[] => {
    for (const g of s.freeGames) {
      if (g.outcome !== "would-add" || !g.appId) continue;
      g.outcome = "added";
      addToLibrary(s, g.appId);
      addEvent(s, "good", `Added a free game: ${g.name}`);
    }
    return s.freeGames;
  },
  "GET /api/freetoplay": ({ s, query }): FreeToPlay[] => {
    const owned = new Set(library(s).map((g) => g.appId));
    const count = Math.min(200, Math.max(10, Number(query.get("count")) || 50));
    const sorts: Record<string, (a: data.FreeToPlayEntry, b: data.FreeToPlayEntry) => number> = {
      reviews: (a, b) => (b.review.positive ?? 0) - (a.review.positive ?? 0),
      popular: (a, b) => (b.review.count ?? 0) - (a.review.count ?? 0),
      newest: (a, b) => b.released - a.released,
      name: (a, b) => a.name.localeCompare(b.name),
    };
    return s.f2p
      .filter((g) => (query.get("achievements") !== "1" || g.achievements) && (query.get("cards") !== "1" || g.cards))
      .sort(sorts[query.get("sort") ?? "reviews"] ?? sorts.reviews)
      .slice(0, count)
      .map((g) => ({ appId: g.appId, name: g.name, review: g.review, owned: owned.has(g.appId) }));
  },
  "POST /api/freetoplay/add": ({ s, body }): FreeAdded => {
    const apps = nums(body, "apps");
    if (apps.length === 0 || apps.length > 100) throw bad("pick between 1 and 100 games");
    const owned = new Set(library(s).map((g) => g.appId));
    const added = apps.filter((id) => !owned.has(id) && s.f2p.some((g) => g.appId === id));
    for (const id of added) addToLibrary(s, id);
    return { added };
  },

  // ---------- automation, alerts, updates (more.go) ----------
  "POST /api/schedule": ({ s, body }) => {
    const start = str(body, "start");
    const end = str(body, "end");
    if (!/^\d{2}:\d{2}$/.test(start) || !/^\d{2}:\d{2}$/.test(end)) throw bad("times look like 22:00");
    const days = Array.isArray(body.days) && body.days.length === 7 ? body.days.map((d) => d === true) : s.panel.schedule.days;
    const next: ScheduleInfo = { on: bool(body, "on"), start, end, days };
    s.panel.schedule = next;
    return ok;
  },
  "POST /api/extras": ({ s, body }) => {
    const steamId = str(body, "steamId");
    if (!/^\d+$/.test(steamId)) throw bad("pick an account");
    const on = bool(body, "on");
    const e = extra(s, steamId, str(body, "mode") || "cards", on);
    const i = s.panel.extras.findIndex((x) => x.steamId === steamId);
    if (i >= 0) s.panel.extras[i] = { ...e, idled: on ? s.panel.extras[i].idled : 0 };
    else s.panel.extras.push(e);
    return ok;
  },
  "POST /api/weekly": ({ s, body }) => {
    s.panel.weekly = bool(body, "on");
    return ok;
  },
  "POST /api/weekly/send": ({ s }): WeeklySent => {
    if (!s.panel.notify) throw refuse("set up alerts first");
    const week = s.days.slice(-7).reduce((t, x) => t + x, 0);
    return { text: `houridle: your week\n${(week / 3600).toFixed(1)} game-hours idled\n6 card drops\n1 badge crafted (+100 XP)\nSteam level ${s.level.level}` };
  },
  "POST /api/standing/check": ({ s }): Standing => {
    s.panel.standing = { ...s.panel.standing!, checked: new Date().toISOString() };
    return s.panel.standing;
  },
  "POST /api/update/check": ({ s }): UpdateInfo => {
    s.panel.update.checked = new Date().toISOString();
    return s.panel.update;
  },
  "POST /api/update/install": ({ s }) => {
    const u = s.panel.update;
    if (!u.available || !u.latest) throw refuse("houridle is up to date");
    u.installing = true;
    setTimeout(() => {
      Object.assign(u, { current: u.latest, available: false, installing: undefined });
      addEvent(s, "good", `Updated to houridle ${u.current}`);
    }, 4000);
    return ok;
  },
  "POST /api/update/auto": ({ s, body }) => {
    s.panel.update.auto = bool(body, "on");
    return ok;
  },
};
