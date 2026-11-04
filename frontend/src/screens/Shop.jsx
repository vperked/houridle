import { useEffect, useState } from "react";
import GameArt from "../components/GameArt.jsx";
import { Empty, Group, Icon, NeedsSignIn, Note, SelectRow, Sheet, Skeleton, Stat, Stats } from "../components/ui.jsx";
import { api } from "../api.js";
import { CAPS, money } from "../money.js";
import { useAction } from "../toast.jsx";
import { plural } from "../format.js";

// WalletCard shows the Steam wallet and today's spending limit (shared by
// everything houridle can buy), with the limit's setting.
export function WalletCard({ balance, currency, cap, spentToday, onCap, busy }) {
  return (
    <section className="surface overflow-hidden">
      <Stats>
        <Stat value={money(balance, currency)} label="Steam wallet" />
        <Stat value={money(Math.max(0, cap - spentToday), currency)} label="Left to spend today" />
      </Stats>
      <div className="border-t border-line">
        <SelectRow
          label="Daily limit"
          detail="For cards and games together"
          value={cap}
          disabled={busy}
          onChange={(v) => onCap(Number(v))}
          options={[...new Set([...CAPS, cap])].sort((a, b) => a - b).map((c) => [c, c ? money(c, currency) : "Off"])}
        />
      </div>
    </section>
  );
}

// useCap changes the daily limit and reports it.
export function useCap(setState) {
  const [busy, run] = useAction();
  const set = (cents, currency) =>
    run("cap", async () => {
      await api.post("/api/shop/cap", { cents });
      setState((s) => ({ ...s, cap: cents }));
    }, cents ? `Daily limit set to ${money(cents, currency)}` : "Buying is off");
  return [busy === "cap", set];
}

// Shop finishes badges by buying their missing cards on the Steam Market.
// Nothing is bought until you confirm the exact price, within today's limit.
export default function Shop({ signedIn }) {
  const [shop, setShop] = useState(null);
  const [quote, setQuote] = useState(null);
  const [busy, run] = useAction();
  const [capBusy, setCap] = useCap(setShop);

  const load = () => run("load", async () => setShop(await api.get("/api/shop")));
  useEffect(() => {
    if (signedIn) load();
  }, [signedIn]); // eslint-disable-line react-hooks/exhaustive-deps

  const getQuote = (b) =>
    run("quote" + b.appId, async () => {
      setQuote({ appId: b.appId, name: b.name, loading: true });
      const q = await api.get(`/api/shop/quote?app=${b.appId}`);
      setQuote({ ...q, name: q.name || b.name });
    }).then((ok) => ok === undefined && setQuote((q) => (q?.loading ? null : q)));

  if (!signedIn) {
    return <NeedsSignIn />;
  }
  return (
    <>
      {shop ? <WalletCard {...shop} busy={capBusy} onCap={(c) => setCap(c, shop.currency)} /> : <div className="surface h-[124px] animate-pulse" />}
      {shop && !shop.allowed && (
        <Note tone="warn">
          <b className="font-semibold">The Market isn't open to this account yet.</b> {shop.reason}
        </Note>
      )}
      <Group title="Best Steam XP for the money" footer="Badges you've partly collected, priced by the cards they're missing. Each badge level is 100 XP.">
        {!shop ? (
          <Skeleton art rows={3} />
        ) : shop.badges.length === 0 ? (
          <Empty icon="cards">No partly collected card sets to finish. Idle some card drops first.</Empty>
        ) : (
          shop.badges.map((b) => (
            <button key={b.appId} type="button" onClick={() => getQuote(b)} disabled={!!busy} className="row row-tap">
              <GameArt appId={b.appId} name={b.name} className="h-8 w-[75px]" />
              <span className="min-w-0 flex-1">
                <span className="block truncate">{b.name}</span>
                <span className="sub block truncate text-[13px]">
                  {b.missing.length} missing · {Math.round(b.xpPerDollar)} XP/$
                </span>
              </span>
              <span className="num font-medium">~{money(b.estimate)}</span>
              <Icon name="chevron" className="faint size-4" strokeWidth={2.2} />
            </button>
          ))
        )}
      </Group>
      <QuoteSheet
        quote={quote}
        shop={shop}
        onClose={() => setQuote(null)}
        onBought={async () => {
          setQuote(null);
          setShop(await api.get("/api/shop"));
        }}
      />
    </>
  );
}

function QuoteSheet({ quote, shop, onClose, onBought }) {
  const [step, setStep] = useState("review"); // review | confirm
  const [busy, run] = useAction();
  useEffect(() => setStep("review"), [quote?.appId]);
  const remaining = shop ? Math.max(0, shop.cap - shop.spentToday) : 0;
  const blocked =
    quote && !quote.loading && shop && (!shop.allowed ? "The Market isn't open to this account yet." : quote.total > shop.balance ? `Your wallet has ${money(shop.balance, shop.currency)}.` : quote.total > remaining ? `That's over today's limit (${money(remaining, shop.currency)} left).` : "");
  const buy = () =>
    run(
      "buy",
      async () => {
        const res = await api.post("/api/shop/buy", { app: quote.appId, total: quote.total });
        await onBought();
        return res;
      },
      (res) =>
        res.error
          ? { title: `Bought ${res.cards.length} card(s), then stopped`, tone: "warn", detail: res.error }
          : { title: `Bought ${res.cards.length} card(s) for ${money(res.spent, quote.currency)}`, celebrate: true, detail: "The set is complete. Craft it in Badges & XP." },
    );

  return (
    <Sheet open={!!quote} onClose={onClose} title={quote?.name ? `Finish ${quote.name}` : ""}>
      {quote?.loading ? (
        <div className="surface overflow-hidden">
          <Skeleton rows={3} />
        </div>
      ) : quote ? (
        <>
          <div className="surface rows overflow-hidden">
            {quote.items.map((it) => (
              <a key={it.card} href={it.market} target="_blank" rel="noopener noreferrer" className="row row-tap">
                <span className="min-w-0 flex-1 truncate">{it.card}</span>
                <span className="num">{money(it.price, quote.currency)}</span>
                <Icon name="external" className="faint size-3.5" />
              </a>
            ))}
            <div className="row font-semibold">
              <span className="flex-1">Total</span>
              <span className="num">{money(quote.total, quote.currency)}</span>
            </div>
          </div>
          <p className="sub mt-2 px-1 text-[13px]">The cheapest listing of each card right now, fees included. If a price goes up before buying, houridle stops and asks again.</p>
          {blocked ? (
            <div className="mt-5">
              <Note tone="warn">{blocked}</Note>
            </div>
          ) : step === "review" ? (
            <button type="button" onClick={() => setStep("confirm")} className="btn-primary btn-lg mt-5 w-full">
              Buy {quote.items.length} card{quote.items.length === 1 ? "" : "s"} · {money(quote.total, quote.currency)}
            </button>
          ) : (
            <PayConfirm amount={money(quote.total, quote.currency)} what={`${plural(quote.items.length, "card")} from the Steam Market`} busy={busy === "buy"} onPay={buy} onBack={() => setStep("review")} />
          )}
        </>
      ) : null}
    </Sheet>
  );
}

// PayConfirm is the last step before money moves: the amount, from where,
// and a button that says exactly what it does.
export function PayConfirm({ amount, what, busy, onPay, onBack }) {
  return (
    <div className="animate-fade mt-5 rounded-2xl bg-fill p-4">
      <div className="flex items-center gap-3">
        <span className="grid size-10 shrink-0 place-items-center rounded-full bg-surface">
          <Icon name="wallet" className="size-5" />
        </span>
        <div className="min-w-0">
          <div className="font-semibold">Pay {amount} from your Steam wallet?</div>
          <div className="sub text-[13px]">{what}</div>
        </div>
      </div>
      <div className="mt-4 grid grid-cols-[1fr_2fr] gap-2">
        <button type="button" onClick={onBack} disabled={busy} className="btn btn-lg">
          Back
        </button>
        <button type="button" onClick={onPay} disabled={busy} className="btn-primary btn-lg !bg-good !text-white">
          {busy ? "Paying…" : `Pay ${amount}`}
        </button>
      </div>
    </div>
  );
}

