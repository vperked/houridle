import { useEffect, useMemo, useState } from "react";
import GameArt from "../components/GameArt.tsx";
import { Checkbox } from "../components/Checkbox.tsx";
import { ActionBar, Confirm, Empty, ErrorState, Group, Icon, NeedsSignIn, Note, Segmented, Skeleton, Stat, Stats } from "../components/ui.tsx";
import { api, errorText } from "../lib/api.ts";
import { money } from "../lib/money.ts";
import { useAction } from "../components/toast.tsx";
import { plural } from "../lib/format.ts";
import type { BoosterInfo, BoosterList, Ground, Inventory as InventoryInfo } from "../lib/types.ts";

// Inventory is the trading cards and gems in the Steam inventory: what the
// cards are worth, turning spare copies into gems, and gems into booster
// packs (each only after you confirm).
export default function Inventory({ signedIn }: { signedIn: boolean }) {
  const [tab, setTab] = useState<"cards" | "boosters">("cards");
  if (!signedIn) {
    return <NeedsSignIn />;
  }
  return (
    <>
      <Segmented
        label="Cards or gems"
        value={tab}
        onChange={setTab}
        options={[
          { value: "cards", label: "Cards" },
          { value: "boosters", label: "Booster packs" },
        ]}
      />
      {tab === "cards" ? <Cards /> : <Boosters />}
    </>
  );
}

function Cards() {
  const [inv, setInv] = useState<InventoryInfo | null>(null);
  const [error, setError] = useState("");
  const [picked, setPicked] = useState<number[]>([]);
  const [confirm, setConfirm] = useState(false);
  const [busy, run] = useAction();
  const load = () =>
    run("load", async () => {
      setError("");
      try {
        setInv(await api.get<InventoryInfo>("/api/inventory"));
        setPicked([]);
      } catch (e) {
        setError(errorText(e));
      }
    });
  useEffect(() => {
    load();
  }, []); // eslint-disable-line react-hooks/exhaustive-deps

  const spareGames = useMemo(() => (inv?.games ?? []).filter((g) => g.spare > 0 && g.gems > 0), [inv]);
  const chosen = spareGames.filter((g) => picked.includes(g.appId));
  const gems = chosen.reduce((t, g) => t + g.spare * g.gems, 0);
  const cards = chosen.reduce((t, g) => t + g.spare, 0);
  const grind = () =>
    run(
      "grind",
      async () => {
        const res = await api.post<Ground>("/api/gems/grind", { apps: chosen.map((g) => g.appId), gems });
        setConfirm(false);
        await load();
        return res;
      },
      (res) => (res.error ? { title: `Turned ${res.cards} card(s) into ${res.gems} gems, then stopped`, tone: "warn", detail: res.error } : { title: `+${res.gems} gems`, detail: `From ${res.cards} spare card${res.cards === 1 ? "" : "s"}.`, celebrate: true }),
    );

  if (error) {
    return (
      <div className="surface">
        <ErrorState error={error} onRetry={load} />
      </div>
    );
  }
  return (
    <>
      <section className="surface overflow-hidden">
        <Stats>
          <Stat value={inv ? inv.cards : "–"} label="Cards" />
          <Stat value={inv ? `~${money(inv.value)}` : "–"} label="Worth" />
          <Stat value={inv ? inv.gems.toLocaleString() : "–"} label="Gems" />
        </Stats>
      </section>
      <Group
        title="By game"
        action={
          <button type="button" onClick={load} disabled={!!busy} aria-label="Read again" className="btn-ghost btn-sm h-7">
            <Icon name="refresh" className={`size-4 ${busy === "load" ? "animate-spin" : ""}`} strokeWidth={2} />
          </button>
        }
        footer={
          inv && !inv.priced
            ? "Only the games with the most cards are priced (the Market limits lookups). Foil cards aren't priced."
            : "What each game's cards sell for on the Market, roughly. Foil cards aren't priced."
        }
      >
        {!inv ? (
          <Skeleton art rows={4} />
        ) : inv.games.length === 0 ? (
          <Empty icon="cards">No trading cards yet. They drop while games idle.</Empty>
        ) : (
          inv.games.map((g) => {
            const can = g.spare > 0 && g.gems > 0;
            const on = picked.includes(g.appId);
            const Tag = can ? "label" : "div";
            return (
              <Tag key={g.appId} className={`row ${can ? "row-tap cursor-pointer" : ""}`}>
                <GameArt appId={g.appId} name={g.name} className="h-8 w-[75px]" />
                <span className="min-w-0 flex-1">
                  <span className="block truncate">{g.name || `App ${g.appId}`}</span>
                  <span className="sub block truncate text-[13px]">
                    {g.cards} card{g.cards === 1 ? "" : "s"}
                    {g.foils > 0 && ` · ${g.foils} foil`}
                    {g.spare > 0 && ` · ${g.spare} spare${g.gems ? ` = ${g.spare * g.gems} gems` : ""}`}
                  </span>
                </span>
                <span className="num text-[14px]">{g.value ? `~${money(g.value)}` : ""}</span>
                {can && <Checkbox checked={on} onChange={() => setPicked((p) => (on ? p.filter((x) => x !== g.appId) : [...p, g.appId]))} label={`Turn ${g.name}'s spare cards into gems`} />}
              </Tag>
            );
          })
        )}
      </Group>
      {spareGames.length > 0 && <p className="sub -mt-5 px-4 text-[13px]">Tick games to turn their spare copies into gems. One of each card is always kept, for crafting.</p>}
      {chosen.length > 0 && (
        <ActionBar>
          <button type="button" onClick={() => setConfirm(true)} className="btn-primary btn-lg w-full">
            Turn {cards} spare card{cards === 1 ? "" : "s"} into {gems} gems…
          </button>
        </ActionBar>
      )}
      <Confirm
        open={confirm}
        onClose={() => setConfirm(false)}
        title={`Turn ${plural(cards, "card")} into gems?`}
        action={busy === "grind" ? "Turning into gems…" : `Turn into ${gems} gems`}
        danger
        busy={busy === "grind"}
        onConfirm={grind}
      >
        The spare copies from {chosen.map((g) => g.name).join(", ")} become {gems} gems. Cards turned into gems are gone for good. One of each card is kept.
      </Confirm>
    </>
  );
}

function Boosters() {
  const [list, setList] = useState<BoosterList | null>(null);
  const [error, setError] = useState("");
  const [making, setMaking] = useState<BoosterInfo | null>(null);
  const [busy, run] = useAction();
  const load = () =>
    run("load", async () => {
      setError("");
      try {
        setList(await api.get<BoosterList>("/api/boosters"));
      } catch (e) {
        setError(errorText(e));
      }
    });
  useEffect(() => {
    load();
  }, []); // eslint-disable-line react-hooks/exhaustive-deps
  const make = () =>
    run(
      "make",
      async () => {
        await api.post("/api/boosters/make", { app: making!.appId, price: making!.price });
        setMaking(null);
        await load();
      },
      { title: `Made a ${making?.name} booster pack`, detail: "Open it in your Steam inventory for 3 cards.", celebrate: true },
    );

  if (error) {
    return (
      <div className="surface">
        <ErrorState error={error} onRetry={load} />
      </div>
    );
  }
  return (
    <>
      <section className="surface overflow-hidden">
        <Stats>
          <Stat value={list ? list.gems.toLocaleString() : "–"} label="Gems" />
          <Stat value={list ? list.boosters.filter((b) => b.available && b.price <= list.gems).length : "–"} label="Packs you can make" />
        </Stats>
      </section>
      <Group title="Booster packs" footer="A booster pack has 3 cards of its game, sometimes a foil. Steam allows one pack per game per day, for games you've earned card drops in.">
        {!list ? (
          <Skeleton art rows={4} />
        ) : list.boosters.length === 0 ? (
          <Empty icon="cards">No games can make booster packs yet.</Empty>
        ) : (
          list.boosters.map((b) => {
            const afford = b.price <= list.gems;
            return (
              <button key={b.appId} type="button" disabled={!b.available || !afford} onClick={() => setMaking(b)} className="row row-tap disabled:opacity-100">
                <GameArt appId={b.appId} name={b.name} className="h-8 w-[75px]" />
                <span className="min-w-0 flex-1">
                  <span className="block truncate">{b.name}</span>
                  <span className={`block truncate text-[13px] ${b.available && afford ? "text-good" : "sub"}`}>
                    {!b.available ? `Next one ${b.availableAt || "later"}` : afford ? "Ready to make" : `Need ${(b.price - list.gems).toLocaleString()} more`}
                  </span>
                </span>
                <span className="num text-[14px]">{b.price.toLocaleString()} gems</span>
              </button>
            );
          })
        )}
      </Group>
      <Confirm
        open={!!making}
        onClose={() => setMaking(null)}
        title={`Make a ${making?.name} booster pack?`}
        action={busy === "make" ? "Making…" : `Use ${making?.price.toLocaleString()} gems`}
        busy={busy === "make"}
        onConfirm={make}
      >
        {making?.price.toLocaleString()} of your {list?.gems.toLocaleString()} gems become one booster pack with 3 {making?.name} cards.
      </Confirm>
      {list && list.gems > 0 && list.boosters.length > 0 && !list.boosters.some((b) => b.available && b.price <= list.gems) && (
        <Note>Turn spare cards into gems on the Cards tab to afford more packs.</Note>
      )}
    </>
  );
}
