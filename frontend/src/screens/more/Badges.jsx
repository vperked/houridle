import { useEffect, useState } from "react";
import { Badge, Empty, Group, Icon, NeedsSignIn, Note, Row, Sheet, Skeleton } from "../../components/ui.jsx";
import GameArt from "../../components/GameArt.jsx";
import { api } from "../../api.js";
import { useAction } from "../../toast.jsx";
import LevelCard, { useLevel } from "../../components/Level.jsx";
import { plural } from "../../format.js";

// Badges & XP: what's ready to craft, the sets you've started, and sale
// events.

export default function BadgesXP({ signedIn }) {
  const [badges, setBadges] = useState(null);
  const [open, setOpen] = useState(null); // the badge whose cards are shown
  const [cards, setCards] = useState({});
  const [busy, run] = useAction();
  const load = () => run("load", async () => setBadges(await api.get("/api/badges")));
  useEffect(() => {
    if (signedIn) load();
  }, [signedIn]); // eslint-disable-line react-hooks/exhaustive-deps
  const craft = () =>
    run(
      "craft",
      async () => {
        const res = await api.post("/api/badges/craft");
        setBadges(await api.get("/api/badges"));
        return res.crafted;
      },
      (crafted) => (crafted.length ? { title: `Crafted ${plural(crafted.length, "badge")}`, celebrate: true, detail: crafted.join(", ") } : { title: "Nothing to craft", tone: "info" }),
    );
  const explore = () =>
    run("explore", () => api.post("/api/events/explore"), (r) => ({
      title: `Explored the discovery queue`,
      detail: `${r.games} games.${r.sticker ? " Claimed today's sale sticker too." : " No sale sticker to claim right now."}`,
    }));
  const showCards = async (b) => {
    setOpen(b);
    if (!cards[b.appId]) {
      const list = await run("cards", () => api.get(`/api/badges/cards?app=${b.appId}`));
      if (list) setCards((c) => ({ ...c, [b.appId]: list }));
    }
  };

  const level = useLevel(signedIn);
  if (!signedIn) return <NeedsSignIn />;
  return (
    <>
      {level && <LevelCard level={level} />}
      <Note>Steam XP comes mostly from crafting badges, which takes a game's full card set. Idling drops about half a set; the rest can be bought.</Note>
      <Group title="Ready to craft">
        {!badges ? (
          <Skeleton art rows={1} />
        ) : badges.ready.length === 0 ? (
          <Empty icon="award">No complete card set yet.</Empty>
        ) : (
          <>
            {badges.ready.map((b) => (
              <Row key={b.appId} lead={<GameArt appId={b.appId} name={b.name} className="h-8 w-[75px]" />} title={b.name} trailing={b.foil ? <Badge tone="warn">Foil too</Badge> : null} />
            ))}
            <div className="p-3">
              <button type="button" className="btn-primary w-full" disabled={!!busy} onClick={craft}>
                {busy === "craft" ? "Crafting…" : `Craft ${badges.ready.length === 1 ? "it" : `all ${badges.ready.length}`}`}
              </button>
            </div>
          </>
        )}
      </Group>
      <Group title="Closest to a badge">
        {!badges ? (
          <Skeleton art rows={3} />
        ) : badges.closest.length === 0 ? (
          <Empty icon="cards">No partly collected sets.</Empty>
        ) : (
          badges.closest.map((b) => (
            <button key={b.appId} type="button" onClick={() => showCards(b)} className="row row-tap">
              <GameArt appId={b.appId} name={b.name} className="h-8 w-[75px]" />
              <span className="min-w-0 flex-1">
                <span className="block truncate">{b.name}</span>
                <span className="mt-1.5 flex items-center gap-2">
                  <span className="h-[3px] flex-1 overflow-hidden rounded-full bg-fill">
                    <span className="block h-full rounded-full bg-accent" style={{ width: `${(100 * b.collected) / b.total}%` }} />
                  </span>
                  <span className="sub num text-[12px]">
                    {b.collected}/{b.total}
                  </span>
                </span>
              </span>
              <Icon name="chevron" className="faint size-4" strokeWidth={2.2} />
            </button>
          ))
        )}
      </Group>
      <Group title="Sale events" footer="During Steam's big sales, exploring the queue earns event cards, and a free sticker can be claimed each day.">
        <Row icon="compass" title={busy === "explore" ? "Exploring…" : "Explore the discovery queue"} onClick={busy ? undefined : explore} chevron={false} />
      </Group>
      <BadgeCards badge={open} cards={open && cards[open.appId]} loading={busy === "cards"} onClose={() => setOpen(null)} />
    </>
  );
}

function BadgeCards({ badge, cards, loading, onClose }) {
  return (
    <Sheet open={!!badge} onClose={onClose} title={badge?.name ?? ""}>
      {badge && (
        <>
          <p className="sub num -mt-2 mb-4 text-[14px]">
            {badge.collected} of {badge.total} cards · {badge.total - badge.collected} missing
          </p>
          <div className="surface rows overflow-hidden">
            {!cards ? (
              loading ? <Skeleton rows={4} /> : <Empty icon="alert">Couldn't load the cards.</Empty>
            ) : (
              cards.map((c) => (
                <div key={c.name} className="row">
                  <span className={`grid size-6 place-items-center rounded-full ${c.owned ? "bg-good text-white" : "bg-fill"}`}>{c.owned ? <Icon name="check" className="size-3.5" strokeWidth={3} /> : null}</span>
                  <span className={`min-w-0 flex-1 truncate ${c.owned ? "" : "sub"}`}>{c.name}</span>
                  {c.owned ? (
                    <span className="faint num text-[13px]">×{c.owned}</span>
                  ) : (
                    <a href={c.market} target="_blank" rel="noopener noreferrer" className="flex items-center gap-1 text-[14px] text-accent">
                      Market <Icon name="external" className="size-3.5" />
                    </a>
                  )}
                </div>
              ))
            )}
          </div>
          {badge.market && (
            <a href={badge.market} target="_blank" rel="noopener noreferrer" className="btn mt-3 w-full">
              All cards on the Market <Icon name="external" className="size-4" />
            </a>
          )}
        </>
      )}
    </Sheet>
  );
}
