import { useEffect, useMemo, useState } from "react";
import { ActionBar, Badge, Empty, Group, Icon, NeedsSignIn, Segmented, SelectRow, Skeleton } from "../../components/ui.tsx";
import { Checkbox } from "../../components/Checkbox.tsx";
import GameArt from "../../components/GameArt.tsx";
import { api } from "../../lib/api.ts";
import { useAction } from "../../components/toast.tsx";
import { forgetLibrary } from "../../hooks/useLibrary.ts";
import type { BadgeTone } from "../../components/ui.tsx";
import type { FreeAdded, FreeGame, FreeToPlay as FreeToPlayGame } from "../../lib/types.ts";
import { plural, reviewText } from "../../lib/format.ts";

// Free games: Steam's giveaways (100% off, to keep) and the best free-to-play
// games.

const OUTCOME: Record<string, [label: string, tone: BadgeTone | undefined]> = {
  added: ["Added", "good"],
  sent: ["Added", "good"],
  "would-add": ["Free now", "accent"],
  owned: ["Owned", undefined],
  "needs-base": ["Needs the base game", undefined],
  failed: ["Couldn't add", "bad"],
};

export default function FreeScreen({ signedIn }: { signedIn: boolean }) {
  const [which, setWhich] = useState<"f2p" | "giveaways">("f2p");
  if (!signedIn) return <NeedsSignIn />;
  return (
    <>
      <Segmented
        label="Free games"
        value={which}
        onChange={setWhich}
        options={[
          { value: "f2p", label: "Free to play" },
          { value: "giveaways", label: "Giveaways" },
        ]}
      />
      {which === "giveaways" ? <FreeGames /> : <FreeToPlay />}
    </>
  );
}

function FreeGames() {
  const [list, setList] = useState<FreeGame[] | null>(null);
  const [busy, run] = useAction();
  const check = () => run("check", async () => setList(await api.get<FreeGame[]>("/api/freegames")));
  useEffect(() => {
    check();
  }, []); // eslint-disable-line react-hooks/exhaustive-deps
  const claim = () =>
    run(
      "claim",
      async () => {
        const res = await api.post<FreeGame[]>("/api/freegames/claim");
        forgetLibrary();
        setList(res);
        return res;
      },
      (res) => {
        const added = (res ?? []).filter((g) => g.outcome === "added" || g.outcome === "sent");
        return added.length ? { title: `Added ${added.length} free game${added.length === 1 ? "" : "s"}`, celebrate: true, detail: added.map((g) => g.name).join(", ") } : { title: "Nothing new to add", tone: "info" };
      },
    );
  const canAdd = (list ?? []).filter((g) => g.outcome === "would-add").length;

  return (
    <Group
      title="Free to keep right now"
      action={
        <button type="button" onClick={check} disabled={!!busy} aria-label="Check again" className="btn-ghost btn-sm h-7">
          <Icon name="refresh" className={`size-4 ${busy === "check" ? "animate-spin" : ""}`} strokeWidth={2} />
        </button>
      }
      footer="Games Steam is giving away to keep (100% off), best reviewed first. What you own and DLC for games you don't are skipped."
    >
      {!list ? (
        <Skeleton art rows={3} />
      ) : list.length === 0 ? (
        <Empty icon="gift">Steam isn't giving anything away right now.</Empty>
      ) : (
        <>
          {list.map((g, i) => {
            const [label, tone] = OUTCOME[g.outcome] ?? OUTCOME.failed;
            return (
              <div key={i} className="row">
                {g.appId ? <GameArt appId={g.appId} name={g.name} className="h-8 w-[75px]" /> : null}
                <span className="min-w-0 flex-1">
                  <span className="block truncate">{g.name}</span>
                  <span className="sub block truncate text-[13px]">{g.error || reviewText(g.review) || " "}</span>
                </span>
                <Badge tone={tone}>{label}</Badge>
              </div>
            );
          })}
          {canAdd > 0 && (
            <div className="p-3">
              <button type="button" className="btn-primary w-full" disabled={!!busy} onClick={claim}>
                {busy === "claim" ? "Adding…" : `Add ${canAdd} to my account`}
              </button>
            </div>
          )}
        </>
      )}
    </Group>
  );
}

// F2PWant is which free-to-play games to list.
interface F2PWant {
  achievements: boolean;
  cards: boolean;
  sort: string;
  count: number;
}

function FreeToPlay() {
  const [list, setList] = useState<FreeToPlayGame[] | null>(null);
  const [picked, setPicked] = useState<number[]>([]);
  const [want, setWant] = useState<F2PWant>({ achievements: false, cards: false, sort: "reviews", count: 50 });
  const [busy, run] = useAction();
  const fresh = useMemo(() => (list ?? []).filter((g) => !g.owned), [list]);
  const load = (w: F2PWant = want) =>
    run("load", async () => {
      const q = new URLSearchParams({ achievements: w.achievements ? "1" : "0", cards: w.cards ? "1" : "0", sort: w.sort, count: String(w.count) });
      setList(await api.get<FreeToPlayGame[]>(`/api/freetoplay?${q}`));
      setPicked([]);
    });
  useEffect(() => {
    load();
  }, []); // eslint-disable-line react-hooks/exhaustive-deps
  const change = (patch: Partial<F2PWant>) => {
    const w = { ...want, ...patch };
    setWant(w);
    load(w);
  };
  const add = (apps: number[]) =>
    run(
      "add",
      async () => {
        const res = await api.post<FreeAdded>("/api/freetoplay/add", { apps });
        if (res.added.length) forgetLibrary(); // the new games show up in the pickers
        const added = new Set(res.added);
        setList((l) => l!.map((g) => (added.has(g.appId) ? { ...g, owned: true } : g)));
        setPicked([]);
        return res.added.length;
      },
      (n) => (n ? { title: `Added ${plural(n, "game")}`, detail: "They're in your library, ready to idle." } : { title: "Nothing new was added", tone: "info", detail: "They may be yours already." }),
    );
  const toggle = (id: number) => setPicked((p) => (p.includes(id) ? p.filter((x) => x !== id) : [...p, id]));

  return (
    <>
      <Group title="Filters">
        <label className="row cursor-pointer">
          <span className="flex-1">Has achievements</span>
          <Checkbox checked={want.achievements} onChange={() => change({ achievements: !want.achievements })} label="Has achievements" />
        </label>
        <label className="row cursor-pointer">
          <span className="flex-1">
            Has trading cards
            {want.cards && <span className="sub block text-[13px]">Free games only drop cards after you spend money in them</span>}
          </span>
          <Checkbox checked={want.cards} onChange={() => change({ cards: !want.cards })} label="Has trading cards" />
        </label>
        <SelectRow
          label="Sort by"
          value={want.sort}
          onChange={(sort) => change({ sort })}
          options={[
            ["reviews", "Best reviewed"],
            ["popular", "Most reviewed"],
            ["newest", "Newest"],
            ["name", "Name"],
          ]}
        />
        <SelectRow label="Show" value={want.count} onChange={(v) => change({ count: Number(v) })} options={[25, 50, 100, 200].map((n): [number, string] => [n, `${n} games`])} />
      </Group>
      <section>
        <div className="flex items-end justify-between">
          <h2 className="heading">Top free-to-play</h2>
          {fresh.length > 0 && (
            <button type="button" className="btn-ghost btn-sm mb-1 h-7" disabled={!!busy} onClick={() => add(fresh.slice(0, 100).map((g) => g.appId))}>
              Add all {Math.min(100, fresh.length)} new
            </button>
          )}
        </div>
        <div className="surface overflow-hidden">
          {!list || busy === "load" ? (
            <Skeleton art rows={6} />
          ) : list.length === 0 ? (
            <Empty icon="search">No games found.</Empty>
          ) : (
            <ul className="rows max-h-[32rem] overflow-y-auto overscroll-contain">
              {list.map((g, i) => (
                <li key={g.appId}>
                  <label className={`row ${g.owned ? "" : "row-tap cursor-pointer"}`}>
                    <span className="faint num w-5 text-right text-[12px]">{i + 1}</span>
                    <GameArt appId={g.appId} name={g.name} className="h-8 w-[75px]" />
                    <span className="min-w-0 flex-1">
                      <span className="block truncate">{g.name}</span>
                      <span className={`block truncate text-[13px] ${g.owned ? "text-good" : "sub"}`}>{g.owned ? "In your library" : reviewText(g.review)}</span>
                    </span>
                    {!g.owned && <Checkbox checked={picked.includes(g.appId)} onChange={() => toggle(g.appId)} label={g.name} />}
                  </label>
                </li>
              ))}
            </ul>
          )}
        </div>
        <p className="sub px-4 pt-2 text-[13px]">Always-free games. Adding them puts them in your library, where houridle can idle them.</p>
      </section>
      {picked.length > 0 && (
        <ActionBar>
          <button type="button" className="btn-primary btn-lg w-full" disabled={!!busy} onClick={() => add(picked)}>
            {busy === "add" ? "Adding…" : `Add ${picked.length} to my library`}
          </button>
        </ActionBar>
      )}
    </>
  );
}
