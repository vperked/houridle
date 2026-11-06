import { useEffect, useState } from "react";
import GameArt from "../components/GameArt.tsx";
import { Checkbox } from "../components/Checkbox.tsx";
import { Empty, ErrorState, Group, Icon, NeedsSignIn, Note, Segmented, SelectRow, Sheet, Skeleton } from "../components/ui.tsx";
import { api, errorText } from "../lib/api.ts";
import { money } from "../lib/money.ts";
import { useAction } from "../components/toast.tsx";
import { forgetLibrary } from "../hooks/useLibrary.ts";
import { PayConfirm, WalletCard, useCap } from "./Shop.tsx";
import { reviewText, truthy } from "../lib/format.ts";
import type { SegmentOption } from "../components/ui.tsx";
import type { Deal, Deals as DealsInfo, GameBought } from "../lib/types.ts";

// DealsWant is what to look for.
interface DealsWant {
  max: number;
  sort: string;
  reviews: boolean;
}

const PRICES: SegmentOption<number>[] = [
  { value: 50, label: "$0.50" },
  { value: 100, label: "$1" },
  { value: 200, label: "$2" },
  { value: 500, label: "$5" },
];
const SORTS: [string, string][] = [
  ["xp", "Most XP per $1"],
  ["payback", "Cards pay back most"],
  ["price", "Cheapest"],
  ["reviews", "Best reviewed"],
];

// Deals finds cheap games with trading cards you don't own and buys one
// with the Steam wallet, after you confirm the exact price. Idling a game
// drops half its card set: cards to sell, or most of a badge (Steam XP).
export default function Deals({ signedIn }: { signedIn: boolean }) {
  const [want, setWant] = useState<DealsWant>({ max: 100, sort: "xp", reviews: true });
  const [res, setRes] = useState<DealsInfo | null>(null);
  const [error, setError] = useState("");
  const [open, setOpen] = useState<Deal | null>(null);
  const [busy, run] = useAction();
  const [capBusy, setCap] = useCap(setRes);

  const load = (w: DealsWant = want) =>
    run("load", async () => {
      setError("");
      try {
        const q = new URLSearchParams({ max: String(w.max), sort: w.sort, reviews: w.reviews ? "1" : "0", count: "12" });
        setRes(await api.get<DealsInfo>(`/api/deals?${q}`));
      } catch (e) {
        setError(errorText(e));
      }
    });
  useEffect(() => {
    if (signedIn) load();
  }, [signedIn]); // eslint-disable-line react-hooks/exhaustive-deps
  const change = (patch: Partial<DealsWant>) => {
    const w = { ...want, ...patch };
    setWant(w);
    load(w);
  };

  if (!signedIn) {
    return <NeedsSignIn />;
  }
  const loading = busy === "load";
  return (
    <>
      {res ? <WalletCard {...res} busy={capBusy} onCap={(c) => setCap(c, res.currency)} /> : <div className="surface shimmer h-[124px]" />}

      <section>
        <h2 className="heading">Price up to</h2>
        <Segmented label="Highest price" options={PRICES} value={want.max} onChange={(max) => change({ max })} />
        <div className="surface rows mt-2.5 overflow-hidden">
          <SelectRow label="Sort by" value={want.sort} onChange={(sort) => change({ sort })} options={SORTS} />
          <label className="row cursor-pointer">
            <span className="flex-1">Well reviewed only</span>
            <Checkbox checked={want.reviews} onChange={() => change({ reviews: !want.reviews })} label="Well reviewed only" />
          </label>
        </div>
      </section>

      <Group
        title="Games with trading cards"
        action={
          <button type="button" onClick={() => load()} disabled={!!busy} aria-label="Look again" className="btn-ghost btn-sm h-7">
            <Icon name="refresh" className={`size-4 ${loading ? "animate-spin" : ""}`} strokeWidth={2} />
          </button>
        }
        footer="Cards: how many drop from idling (half the set). XP/$: Steam XP per dollar for a level 1 badge. Idling a game drops half its card set. Sell the cards, or buy the rest to craft a badge: 100 Steam XP. Card values are Market estimates."
      >
        {loading || (!res && !error) ? (
          <>
            <p className="sub px-4 pt-3 text-[13px]">Checking store prices and what each game's cards sell for…</p>
            <Skeleton art rows={5} />
          </>
        ) : error ? (
          <ErrorState error={error} onRetry={() => load()} />
        ) : res!.deals.length === 0 ? (
          <Empty icon="tag">No games with cards at this price that you don't already own. Try a higher price.</Empty>
        ) : (
          res!.deals.map((d) => (
            <button key={d.appId} type="button" onClick={() => setOpen(d)} className="row row-tap">
              <GameArt appId={d.appId} name={d.name} className="h-8 w-[75px]" />
              <span className="min-w-0 flex-1">
                <span className="block truncate">{d.name}</span>
                <span className="sub block truncate text-[13px]">
                  {d.drops} cards · {Math.round(d.xpPerDollar)} XP/$
                </span>
              </span>
              <span className="text-right">
                <span className="num block font-medium">{money(d.price, d.currency)}</span>
                {d.discount > 0 && <span className="num block text-[12px] text-good">−{d.discount}%</span>}
              </span>
              <Icon name="chevron" className="faint size-4" strokeWidth={2.2} />
            </button>
          ))
        )}
      </Group>

      <DealSheet
        deal={open}
        wallet={res}
        onClose={() => setOpen(null)}
        onBought={async (appId) => {
          forgetLibrary(); // the new game shows up in the pickers
          setRes((r) => ({ ...r!, deals: r!.deals.filter((d) => d.appId !== appId) }));
          load();
        }}
      />
    </>
  );
}

interface DealSheetProps {
  deal: Deal | null;
  wallet: DealsInfo | null;
  onClose: () => void;
  onBought: (appId: number) => Promise<void>;
}

function DealSheet({ deal, wallet, onClose, onBought }: DealSheetProps) {
  const [step, setStep] = useState<"review" | "confirm" | "done">("review");
  const [result, setResult] = useState<GameBought | null>(null);
  const [busy, run] = useAction();
  useEffect(() => {
    setStep("review");
    setResult(null);
  }, [deal?.appId]);
  if (!deal) return <Sheet open={false} onClose={onClose} />;

  const price = money(deal.price, deal.currency);
  const remaining = wallet ? Math.max(0, wallet.cap - wallet.spentToday) : 0;
  const blocked = !wallet
    ? ""
    : wallet.cap === 0
      ? "Buying is off. Set a daily limit above to buy games."
      : deal.price > wallet.balance
        ? `Your Steam wallet has ${money(wallet.balance, wallet.currency)}. Add funds on Steam first.`
        : deal.price > remaining
          ? `That's over today's limit (${money(remaining, wallet.currency)} left).`
          : "";
  const buy = () =>
    run(
      "buy",
      async () => {
        const r = await api.post<GameBought>("/api/deals/buy", { app: deal.appId, price: deal.price });
        setResult(r);
        setStep("done");
        await onBought(deal.appId);
        return r;
      },
      (r) => ({ title: `Bought ${r.name}`, celebrate: true, detail: `${money(r.spent, r.currency)} from your Steam wallet.` }),
    );

  const rows: ([string, string] | null)[] = [
    ["Price", price + (deal.discount > 0 ? ` (−${deal.discount}%)` : "")],
    ["Cards that drop from idling", `${deal.drops} of ${deal.cards}`],
    ["What they sell for", `~${money(deal.dropValue)} after fees`],
    ["Level 1 badge (100 XP)", `~${money(deal.xpCost)} with the ${deal.cards - deal.drops} cards to buy`],
    deal.review?.positive ? ["Reviews", reviewText(deal.review)] : null,
  ];
  const facts = rows.filter(truthy);

  return (
    <Sheet open onClose={onClose} title={deal.name}>
      <GameArt appId={deal.appId} name={deal.name} variant="header" className="aspect-[460/215] w-full" />
      {step === "done" ? (
        <div className="animate-fade py-6 text-center">
          <span className="mx-auto grid size-12 place-items-center rounded-full bg-good text-white">
            <Icon name="check" className="size-6" strokeWidth={2.6} />
          </span>
          <h3 className="mt-3 text-[19px] font-semibold">It's yours</h3>
          <p className="sub mx-auto mt-1 max-w-xs text-[15px]">
            {money(result!.spent, result!.currency)} came from your wallet. Idle it in “Card drops” mode, or pick it on the Idle tab, to collect its {deal.drops} cards.
          </p>
          <button type="button" onClick={onClose} className="btn btn-lg mt-5 w-full">
            Done
          </button>
        </div>
      ) : (
        <>
          <div className="surface rows mt-4 overflow-hidden">
            {facts.map(([k, v]) => (
              <div key={k} className="flex min-h-11 items-center justify-between gap-4 px-4 py-2.5 text-[14px]">
                <span className="sub">{k}</span>
                <span className="num text-right font-medium">{v}</span>
              </div>
            ))}
          </div>
          {deal.package && deal.package !== deal.name && <p className="sub mt-2 px-1 text-[13px]">You'd buy “{deal.package}”.</p>}
          <p className="sub mt-2 px-1 text-[13px]">Tax, if any, is added at checkout; houridle stops if the total is more than this price. Steam's refund window ends after 2 hours of play, which idling uses up quickly.</p>
          {blocked ? (
            <div className="mt-5">
              <Note tone="warn">{blocked}</Note>
            </div>
          ) : step === "review" ? (
            <button type="button" onClick={() => setStep("confirm")} className="btn-primary btn-lg mt-5 w-full">
              Buy · {price}
            </button>
          ) : (
            <PayConfirm amount={price} what={`${deal.name}, for your own account`} busy={busy === "buy"} onPay={buy} onBack={() => setStep("review")} />
          )}
        </>
      )}
    </Sheet>
  );
}
