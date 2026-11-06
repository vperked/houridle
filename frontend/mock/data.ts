// Test data for `npm run dev` without a houridle: real Steam games (so
// their pictures load), with made-up hours, cards and prices. Every shape
// comes from src/lib/types.ts, which mirrors the Go API.
import type { Achievement, BoosterInfo, CardInfo, CardGroup, Deal, FreeGame, FreeToPlay, Review, ShopBadge, StatusEvent } from "../src/lib/types.ts";

export const MAIN_ID = "76561198012345678";
export const ALT_ID = "76561198087654321";
export const MAIN_NAME = "vperk";
export const ALT_NAME = "vperk_alt";

// A game in the test library: what Steam knows about it for this account.
export interface CatalogGame {
  appId: number;
  name: string;
  minutes: number; // on record
  drops: number; // card drops left
  shared?: boolean; // a family member's
  refundable?: boolean; // bought recently, under 2 hours played
}

export const LIBRARY: CatalogGame[] = [
  { appId: 620, name: "Portal 2", minutes: 1490, drops: 2 },
  { appId: 105600, name: "Terraria", minutes: 18210, drops: 3 },
  { appId: 413150, name: "Stardew Valley", minutes: 9935, drops: 0 },
  { appId: 367520, name: "Hollow Knight", minutes: 2650, drops: 1 },
  { appId: 1145360, name: "Hades", minutes: 52, drops: 4, refundable: true },
  { appId: 292030, name: "The Witcher 3: Wild Hunt", minutes: 4410, drops: 0 },
  { appId: 268910, name: "Cuphead", minutes: 610, drops: 3 },
  { appId: 504230, name: "Celeste", minutes: 1200, drops: 0 },
  { appId: 250900, name: "The Binding of Isaac: Rebirth", minutes: 7720, drops: 0 },
  { appId: 646570, name: "Slay the Spire", minutes: 3305, drops: 2 },
  { appId: 4000, name: "Garry's Mod", minutes: 940, drops: 0 },
  { appId: 440, name: "Team Fortress 2", minutes: 26100, drops: 0 },
  { appId: 730, name: "Counter-Strike 2", minutes: 11860, drops: 0 },
  { appId: 1245620, name: "ELDEN RING", minutes: 0, drops: 0, shared: true },
];

// The library the alternate account sees.
export const ALT_LIBRARY: CatalogGame[] = [
  { appId: 570, name: "Dota 2", minutes: 30120, drops: 0 },
  { appId: 105600, name: "Terraria", minutes: 340, drops: 3 },
  { appId: 646570, name: "Slay the Spire", minutes: 90, drops: 3 },
  { appId: 4000, name: "Garry's Mod", minutes: 0, drops: 4 },
];

// Games the mock knows pictures for (anything else has none, like a game
// the store doesn't show).
export const KNOWN_APPS = new Set<number>([
  ...LIBRARY.map((g) => g.appId),
  ...ALT_LIBRARY.map((g) => g.appId),
  391540, 219150, 207610, 233450, 242680, 257850, 1213700, 711540,
  230410, 1172470, 238960, 444090, 1085660, 386360, 552990,
]);

// Steam's older fixed picture addresses (houridle looks up the versioned
// ones; these still answer for every game here).
const ASSETS = "https://shared.akamai.steamstatic.com/store_item_assets/steam/apps/";
export const art = (id: number) => ({ capsule: `${ASSETS}${id}/capsule_231x87.jpg`, header: `${ASSETS}${id}/header.jpg` });

export const marketSearch = (appId: number) => `https://steamcommunity.com/market/search?appid=753&category_753_Game%5B%5D=tag_app_${appId}`;
const cardListing = (appId: number, card: string) => `https://steamcommunity.com/market/listings/753/${appId}-${encodeURIComponent(card)}`;

const CARD_NAMES: Record<number, string[]> = {
  620: ["Atlas", "P-Body", "Wheatley", "GLaDOS", "Cave Johnson", "Chell", "Turret", "Space Core"],
  105600: ["The Guide", "Eye of Cthulhu", "Skeletron", "Wall of Flesh", "Plantera", "Golem", "Moon Lord", "Merchant"],
  367520: ["The Knight", "Hornet", "Zote", "Quirrel", "Cornifer", "Grimm"],
  646570: ["The Ironclad", "The Silent", "The Defect", "The Watcher", "Neow"],
  268910: ["Cuphead", "Mugman", "The Devil", "King Dice", "Ms. Chalice", "Elder Kettle", "Cagney Carnation", "Baroness Von Bon Bon", "Hilda Berg"],
  1145360: ["Zagreus", "Megaera", "Thanatos", "Hades", "Nyx", "Cerberus", "Achilles", "Dusa"],
  4000: ["Physgun", "Ragdoll", "Toolgun", "Sandbox", "Dupe", "Wiremod"],
};

export function cardNames(appId: number): string[] {
  return CARD_NAMES[appId] ?? Array.from({ length: 6 }, (_, i) => `Card ${i + 1}`);
}

// cardsOf is a game's card set, with how many of each card are owned.
export function cardsOf(appId: number, owned: number[]): CardInfo[] {
  return cardNames(appId).map((name, i) => ({ name, owned: owned[i] ?? 0, market: cardListing(appId, name) }));
}

// The owned counts behind the badge and shop screens.
export const OWNED: Record<number, number[]> = {
  105600: [1, 2, 1, 1, 3, 1, 1, 2], // complete: ready to craft
  620: [1, 1, 0, 1, 2, 1, 0, 1],
  367520: [2, 1, 0, 1, 0, 1],
  646570: [1, 0, 1, 0, 0],
  268910: [1, 1, 0, 0, 1, 2, 0, 1, 0],
};

export function shopBadges(): ShopBadge[] {
  const est: Record<number, number> = { 620: 24, 367520: 31, 646570: 38, 268910: 52 };
  return [620, 367520, 646570, 268910].map((appId) => {
    const owned = OWNED[appId];
    const names = cardNames(appId);
    const missing = names.filter((_, i) => !owned[i]);
    const estimate = est[appId] * missing.length;
    return { appId, name: nameOf(appId), collected: names.length - missing.length, total: names.length, missing, estimate, xpPerDollar: 100 / (estimate / 100) };
  });
}

export function nameOf(appId: number): string {
  return [...LIBRARY, ...ALT_LIBRARY].find((g) => g.appId === appId)?.name ?? DEALS.find((d) => d.appId === appId)?.name ?? F2P.find((g) => g.appId === appId)?.name ?? `App ${appId}`;
}

const review = (summary: string, positive: number, count: number): Review => ({ summary, positive, count });

// ---------- achievements ----------

const ach = (id: string, name: string, description: string, unlocked: boolean, extra: Partial<Achievement> = {}): Achievement => ({ id, name, description, unlocked, ...extra });

export function achievementsFor(appId: number): Achievement[] {
  switch (appId) {
    case 620:
      return [
        ach("ACH.SURVIVE_CONTAINER_RIDE", "Wake Up Call", "Survive the manual override", true),
        ach("ACH.WAKE_UP", "You Monster", "Reunite with GLaDOS", true),
        ach("ACH.LASER", "Undiscouraged", "Complete the first Thermal Discouragement Beam test", true),
        ach("ACH.BRIDGE", "Bridge Over Troubling Water", "Complete the first Hard Light Bridge test", false),
        ach("ACH.BREAK_OUTPUTS", "SaBOTour", "Make a break for it", false),
        ach("ACH.STALEMATE_ASSOCIATE", "Stalemate Associate", "Press the button!", false),
        ach("ACH.ADDICTED_TO_SPUDS", "Tater Tote", "Carry science forward", false),
        ach("ACH.BLUE_GEL", "Vertically Unchallenged", "Master the Repulsion Gel", false),
        ach("ACH.SHOOT_THE_MOON", "Lunacy", "", false, { hidden: true }),
      ];
    case 105600:
      return [
        ach("TIMBER", "Timber!!", "Chop down your first tree.", true),
        ach("BENCHED", "Benched", "Craft your first work bench.", true),
        ach("EYE_ON_YOU", "Eye on You", "Defeat the Eye of Cthulhu.", true),
        ach("STILL_HUNGRY", "Still Hungry", "Defeat the Wall of Flesh.", false),
        ach("CHAMPION_OF_TERRARIA", "Champion of Terraria", "Defeat the Moon Lord.", false),
        ach("TOPPED_OFF", "Topped Off", "Reach max life and mana.", false),
        ach("FISH_OUT_OF_WATER", "Fish Out of Water", "Defeat Duke Fishron.", false, { protected: true }),
      ];
    case 413150:
      return [
        ach("Achievement_Greenhorn", "Greenhorn", "Earn 15,000g", true),
        ach("Achievement_Cowpoke", "Cowpoke", "Earn 50,000g", true),
        ach("Achievement_Homesteader", "Homesteader", "Earn 250,000g", false),
        ach("Achievement_Gofer", "Gofer", "Complete 10 'Help Wanted' requests", false),
        ach("Achievement_Fector", "Fector's Challenge", "Beat 'Journey Of The Prairie King' without dying", false),
        ach("Achievement_Moving_Up", "Moving Up", "Upgrade your house", true),
        ach("Achievement_Full_House", "Full House", "Get married and have two kids", false, { protected: true }),
      ];
    default:
      return [];
  }
}

// ---------- the badge shop, deals, free games ----------

function deal(appId: number, name: string, price: number, discount: number, r: Review, cards: number, cardPrice: number): Deal {
  const drops = Math.ceil(cards / 2);
  const xpCost = price + (cards - drops) * cardPrice;
  return { appId, name, package: name, price, currency: "USD", discount, review: r, cards, drops, cardPrice, dropValue: Math.round(drops * cardPrice * 0.87), xpCost, xpPerDollar: 100 / (xpCost / 100) };
}

export const DEALS: Deal[] = [
  deal(219150, "Hotline Miami", 99, 90, review("Overwhelmingly Positive", 97, 82000), 6, 9),
  deal(391540, "Undertale", 299, 70, review("Overwhelmingly Positive", 96, 210000), 9, 7),
  deal(207610, "The Walking Dead", 149, 85, review("Very Positive", 94, 31000), 6, 11),
  deal(233450, "Prison Architect", 199, 90, review("Very Positive", 90, 52000), 8, 6),
  deal(242680, "Nuclear Throne", 449, 70, review("Very Positive", 92, 20000), 6, 8),
  deal(257850, "Hyper Light Drifter", 499, 75, review("Very Positive", 91, 29000), 8, 10),
  { ...deal(1213700, "Spirit of the North", 79, 92, review("Mixed", 63, 1900), 6, 5), package: "Spirit of the North + Soundtrack" },
];

export const FREE_GAMES: FreeGame[] = [
  { name: "Spirit of the North", appId: 1213700, outcome: "would-add", review: review("Mixed", 63, 1900) },
  { name: "Lonely Mountains: Downhill", appId: 711540, outcome: "owned", review: review("Very Positive", 92, 6800) },
  { name: "Ori and the Blind Forest: Soundtrack", outcome: "needs-base", review: {} },
];

// FreeToPlayEntry is a free-to-play game with what the filters look at.
export interface FreeToPlayEntry extends FreeToPlay {
  achievements: boolean;
  cards: boolean;
  released: number; // year
}

const f2p = (appId: number, name: string, r: Review, achievements: boolean, cards: boolean, released: number): FreeToPlayEntry => ({ appId, name, review: r, owned: false, achievements, cards, released });

export const F2P: FreeToPlayEntry[] = [
  f2p(440, "Team Fortress 2", review("Very Positive", 89, 1000000), true, false, 2007),
  f2p(570, "Dota 2", review("Very Positive", 82, 2300000), false, true, 2013),
  f2p(730, "Counter-Strike 2", review("Very Positive", 86, 8400000), true, false, 2023),
  f2p(230410, "Warframe", review("Very Positive", 87, 600000), true, true, 2013),
  f2p(1172470, "Apex Legends", review("Mixed", 66, 900000), true, false, 2020),
  f2p(238960, "Path of Exile", review("Very Positive", 87, 210000), true, true, 2013),
  f2p(444090, "Paladins", review("Mostly Positive", 79, 330000), true, true, 2018),
  f2p(1085660, "Destiny 2", review("Mostly Positive", 78, 600000), true, true, 2019),
  f2p(386360, "SMITE", review("Very Positive", 81, 140000), true, true, 2015),
  f2p(552990, "World of Warships", review("Mixed", 69, 60000), true, true, 2017),
];

// ---------- inventory ----------

export function inventoryGames(): CardGroup[] {
  const g = (appId: number, cards: number, kinds: number, spare: number, foils: number, value: number, gems: number): CardGroup => ({ appId, name: nameOf(appId), cards, kinds, spare, foils, value, gems, market: marketSearch(appId) });
  return [
    g(105600, 12, 8, 4, 1, 168, 40),
    g(620, 7, 6, 1, 0, 84, 40),
    g(268910, 6, 5, 1, 0, 61, 30),
    g(367520, 5, 4, 1, 0, 52, 30),
    g(646570, 2, 2, 0, 0, 30, 30),
    g(250900, 3, 3, 0, 1, 0, 0),
  ];
}

export function boosters(now: number): BoosterInfo[] {
  return [
    { appId: 105600, name: "Terraria", price: 600, available: true },
    { appId: 620, name: "Portal 2", price: 800, available: true },
    { appId: 268910, name: "Cuphead", price: 1200, available: true },
    { appId: 367520, name: "Hollow Knight", price: 750, available: false, availableAt: new Date(now + 16 * 3600e3).toLocaleString("en-US", { month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" }) },
  ];
}

// ---------- activity ----------

export function events(now: number): StatusEvent[] {
  const at = (minutesAgo: number) => new Date(now - minutesAgo * 60e3).toISOString();
  return [
    { at: at(190), level: "info", text: `Signed in to Steam as ${MAIN_NAME}` },
    { at: at(188), level: "info", text: "Idling 5 games, 1 waiting its turn" },
    { at: at(150), level: "good", text: "Portal 2: 1 card dropped", kind: "drop" },
    { at: at(122), level: "warn", text: "Steam didn't answer in time; trying again in a minute" },
    { at: at(121), level: "info", text: "Connected to Steam again" },
    { at: at(95), level: "good", text: "Terraria: 2 cards dropped", kind: "drop" },
    { at: at(70), level: "good", text: "Celeste: every card has dropped" },
    { at: at(42), level: "good", text: "Added a free game: Lonely Mountains: Downhill" },
    { at: at(18), level: "good", text: "Crafted the Terraria badge (+100 XP)", kind: "craft" },
    { at: at(6), level: "info", text: `${ALT_NAME}: idling 3 games for card drops` },
  ];
}

// dailyHours is a believable amount of idling for each of the last n days
// (oldest first), none before `since` days ago.
export function dailySeconds(n: number, since: number): number[] {
  let seed = 7;
  const rand = () => ((seed = (seed * 16807) % 2147483647) / 2147483647);
  return Array.from({ length: n }, (_, i) => {
    const ago = n - 1 - i;
    if (ago > since) return 0;
    if (rand() < 0.12) return 0; // a day off
    return Math.round((4 + rand() * 30) * 3600); // game-hours: several games at once
  });
}

// bigLibrary is LIBRARY plus n made-up games (no pictures), for seeing how
// the page copes with a library of hundreds.
export function bigLibrary(n: number): CatalogGame[] {
  const first = ["Iron", "Hollow", "Neon", "Silent", "Crimson", "Lost", "Tiny", "Star", "Deep", "Wild", "Frozen", "Last"];
  const second = ["Harbor", "Crown", "Signal", "Forge", "Garden", "Drift", "Tower", "Echo", "Run", "Kingdom", "Circuit", "Tides"];
  let seed = 11;
  const rand = () => ((seed = (seed * 16807) % 2147483647) / 2147483647);
  const made = Array.from({ length: n }, (_, i): CatalogGame => {
    const name = `${first[i % first.length]} ${second[Math.floor(i / first.length) % second.length]}${i >= first.length * second.length ? ` ${Math.floor(i / (first.length * second.length)) + 1}` : ""}`;
    const played = rand() < 0.35 ? 0 : Math.round(rand() * rand() * 6000);
    return { appId: 3_000_000 + i * 10, name, minutes: played, drops: rand() < 0.2 ? 1 + Math.floor(rand() * 4) : 0 };
  });
  return [...LIBRARY, ...made];
}
