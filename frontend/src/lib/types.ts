// The JSON houridle's API sends and takes. Each type mirrors a Go struct in
// backend/web (server.go, control.go, more.go); `omitempty` / `omitzero`
// fields are optional here. Times are RFC 3339 strings, durations seconds,
// money cents. A Go slice that may be nil arrives as null, typed `| null`.

// ---------- GET /api/status (statusJSON) ----------

export type GameState = "idling" | "starting" | "waiting" | "failing" | "queued" | "done" | "refundable" | "stopped";
export type EventLevel = "good" | "warn" | "bad" | "info";
export type EventKind = "drop" | "item" | "craft" | "achievements" | "all-unlocked";

export interface Status {
  version: string;
  account: string;
  started: string;
  stopAt?: string;
  games: Game[];
  events: StatusEvent[];
  stats?: Stats;
  run: Run;
  panel?: Panel; // present in houridle serve: the page can control it
}

export interface Run {
  signedIn: boolean;
  cards?: boolean;
  all?: boolean;
  claim?: boolean;
  target?: number;
  rotate?: number;
  status?: string;
  craft?: boolean;
  events?: boolean;
}

export interface Game {
  appId: number;
  name: string;
  state: GameState;
  note?: string;
  idled: number;
  total?: number;
  target?: number;
  drops?: number;
}

export interface StatusEvent {
  at: string;
  level: EventLevel;
  text: string;
  kind?: EventKind;
}

export interface Stats {
  today: number;
  week: number;
  month: number;
  all: number;
  since?: string; // first day on record, YYYY-MM-DD
  days: Day[]; // oldest first
  games: GameTotal[]; // all time, most first
}

export interface Day {
  day: string; // YYYY-MM-DD
  time: number;
}

export interface GameTotal {
  appId: number;
  name: string;
  week: number;
  all: number;
}

// GET /api/art: pictures by app ID (as a string key).
export interface Art {
  capsule?: string;
  header?: string;
}

// ---------- the control panel (Panel and what it holds) ----------

export type IdleMode = "games" | "all" | "cards";

// Settings is how to idle, as the page sends it (POST /api/idle/start).
export interface Settings {
  mode: IdleMode;
  games: number[];
  until: string;
  rotate: string;
  status: string;
  claim: boolean;
  craft: boolean;
  events: boolean;
  stop: string;
  refundable: boolean;
}

export interface Panel {
  idling: boolean;
  source: string; // "client", a SteamID, or "" if there's nothing to idle with
  signedIn: boolean;
  steamClient: boolean;
  settings: Settings;
  accounts: AccountInfo[];
  notify: boolean;
  notifyKind: string; // "discord", "slack" or "text"
  webAddr: string;
  login: Login;
  schedule: ScheduleInfo;
  drip: DripInfo[];
  extras: ExtraInfo[];
  weekly: boolean;
  update: UpdateInfo;
  standing?: Standing; // until checked
  steam: SteamInfo;
}

export interface AccountInfo {
  steamId: string;
  name: string;
  default: boolean;
  expires?: string;
  damaged?: boolean;
}

// Login is a sign-in in progress (or its outcome): a QR code the Steam
// app scans, or an account name and password and then Steam Guard.
export interface Login {
  state: "" | "waiting" | "done" | "failed";
  method?: "qr" | "password";
  url?: string;
  qr?: string; // SVG of url
  code?: "app" | "email"; // password: Steam Guard wants a code from here
  approve?: "app" | "email"; // password: or it can be approved there instead
  hint?: string; // where Steam emailed the code
  account?: string;
  error?: string;
}

export interface ScheduleInfo {
  on: boolean;
  start: string;
  end: string;
  days: boolean[]; // 7, Monday first
  active?: boolean;
  paused?: boolean;
  next?: string;
}

export interface DripInfo {
  appId: number;
  name: string;
  every: number; // minutes
  next: string;
  left: number;
}

export interface ExtraInfo {
  steamId: string;
  name: string;
  mode: string;
  on: boolean;
  idling: number;
  games: number;
  idled: number; // seconds this run, all games
  note?: string;
}

export interface UpdateInfo {
  current: string;
  latest?: string;
  available: boolean;
  auto: boolean;
  checked?: string;
  published?: string;
  notes?: string;
  error?: string;
  installing?: boolean;
}

export interface Standing {
  vacBans: number;
  gameBans: number;
  daysSinceBan: number; // -1: no ban
  tradeBan: string; // "None", "Probation" or "Banned"
  limited: boolean;
  market: boolean;
  marketReason?: string;
  checked: string;
  ok: boolean;
  error?: string;
}

export interface SteamInfo {
  protocol: number;
  client: number;
  fromRepo: boolean;
  build?: string;
  checked?: string;
  error?: string;
}

// ---------- library and achievements ----------

// GET /api/library: null when the library is empty.
export interface LibraryGame {
  appId: number;
  name: string;
  minutes: number;
  shared?: boolean;
}

// GET /api/drops: card drops left by app ID.
export type Drops = Record<number, number>;

export interface Achievement {
  id: string;
  name: string;
  description?: string;
  hidden?: boolean;
  unlocked: boolean;
  protected?: boolean;
}

// POST /api/achievements: the ones that didn't change, id -> why.
export interface AchievementsSaved {
  failed: Record<string, string> | null;
}

export interface BulkAchievements {
  appId: number;
  name: string;
  total: number;
  changed: number;
  already: number;
  gameOnly: number;
  failed: number;
  error?: string;
}

// POST /api/achievements/bulk
export interface BulkResult {
  games: BulkAchievements[];
}

// ---------- free games ----------

export interface Review {
  summary?: string;
  positive?: number;
  count?: number;
}

export type FreeOutcome = "added" | "sent" | "would-add" | "owned" | "needs-base" | "failed";

export interface FreeGame {
  name: string;
  appId?: number;
  outcome: FreeOutcome;
  error?: string;
  review: Review;
}

export interface FreeToPlay {
  appId: number;
  name: string;
  review: Review;
  owned: boolean;
}

// POST /api/freetoplay/add
export interface FreeAdded {
  added: number[];
}

// ---------- badges, level, inventory ----------

export interface BadgesInfo {
  ready: BadgeInfo[];
  closest: BadgeInfo[];
}

export interface BadgeInfo {
  appId: number;
  name: string;
  collected: number;
  total: number;
  foil?: boolean;
  market: string;
}

export interface CardInfo {
  name: string;
  owned: number;
  market: string;
}

// POST /api/badges/craft
export interface Crafted {
  crafted: string[];
}

export interface Explored {
  games: number;
  sticker: boolean;
}

export interface Level {
  level: number;
  xp: number;
  levelXp: number;
  nextXp: number;
  badges: number;
  ready: number;
  partial: number;
  missing: number;
  dropsXp: number;
  unlocked?: number;
}

export interface Inventory {
  gems: number;
  cards: number;
  value: number;
  spare: number;
  boosters: number;
  games: CardGroup[];
  priced: boolean;
}

export interface CardGroup {
  appId: number;
  name: string;
  cards: number;
  kinds: number;
  spare: number;
  foils: number;
  value: number;
  gems: number; // what each spare card turns into (0 = not checked)
  market: string;
}

export interface Ground {
  cards: number;
  gems: number;
  error?: string;
}

export interface BoosterList {
  gems: number;
  boosters: BoosterInfo[];
}

export interface BoosterInfo {
  appId: number;
  name: string;
  price: number; // gems
  available: boolean;
  availableAt?: string;
}

// ---------- buying: the badge shop and deals ----------

export interface Shop {
  balance: number;
  currency: number;
  allowed: boolean;
  reason?: string;
  cap: number;
  spentToday: number;
  badges: ShopBadge[];
}

export interface ShopBadge {
  appId: number;
  name: string;
  collected: number;
  total: number;
  missing: string[]; // a listed badge always has some
  estimate: number;
  xpPerDollar: number;
}

export interface Quote {
  appId: number;
  name: string;
  items: QuoteItem[];
  total: number;
  currency: number;
}

export interface QuoteItem {
  card: string;
  price: number;
  market: string;
}

export interface Bought {
  cards: string[];
  spent: number;
  error?: string;
}

export interface Deals {
  balance: number;
  currency: number;
  cap: number;
  spentToday: number;
  deals: Deal[];
}

export interface Deal {
  appId: number;
  name: string;
  package: string;
  price: number;
  currency: string; // the store's, e.g. "USD"
  discount: number;
  review: Review;
  cards: number;
  drops: number;
  cardPrice: number;
  dropValue: number;
  xpCost: number;
  xpPerDollar: number;
}

export interface GameBought {
  appId: number;
  name: string;
  bought: boolean;
  spent: number;
  currency: number;
  error?: string;
}

// ---------- small answers ----------

// What an action with nothing to report answers.
export interface OK {
  ok: true;
}

// POST /api/weekly/send
export interface WeeklySent {
  text: string;
}

// POST /api/webkey
export interface NewLinks {
  links: string[] | null;
}
