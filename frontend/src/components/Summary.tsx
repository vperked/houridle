import { Stat, Stats } from "./ui.tsx";
import { Backdrop, Equalizer, Ring, Roll } from "./Live.tsx";
import { clock, hours, span, target, truthy } from "../lib/format.ts";
import { stateOf } from "../lib/states.ts";
import type { Game, Run, Status } from "../lib/types.ts";
import type { LoadState } from "../hooks/useStatus.ts";

// Summary is the status block on Now: what houridle is doing and the run's
// numbers.
interface SummaryProps {
  data: Status | null;
  games: Game[];
  now: number;
  extra: number;
  state: LoadState;
}

export default function Summary({ data, games, now, extra, state }: SummaryProps) {
  const idling = games.filter((g) => g.state === "idling").length;
  const queued = games.filter((g) => g.state === "queued").length;
  const done = games.filter((g) => g.state === "done").length;
  const refundable = games.filter((g) => g.state === "refundable").length;
  const drops = games.reduce((t, g) => t + (g.drops || 0), 0);
  const runSecs = games.reduce((t, g) => t + g.idled, 0);
  const stats = data?.stats;
  const live = idling > 0 && state === "ok";

  let label = "Loading";
  if (state === "stopped") label = "Stopped";
  else if (idling) label = "Idling";
  else if (games[0]) {
    const s = stateOf(games[0].state);
    label = s.label + (games[0].note ? ` · ${games[0].note}` : "");
  } else if (data) label = "Looking for games";

  const uptime = data ? (now - Date.parse(data.started)) / 1000 : 0;
  const untilStop = data?.stopAt ? (Date.parse(data.stopAt) - now) / 1000 : null;
  const extras = [
    untilStop !== null && ["Stops in", untilStop > 0 ? clock(untilStop) : "now"],
    drops > 0 && ["Card drops left", drops],
    queued > 0 && ["Waiting their turn", queued],
    refundable > 0 && ["Held back, still refundable", refundable],
    done > 0 && ["Done", done],
  ].filter(truthy);
  const how = data ? runDescription(data.run, games.length) : "";
  const art = games.filter((g) => g.state === "idling").slice(0, 8).map((g) => g.appId);
  const allTime = stats ? stats.all + extra : 0;

  return (
    <section className="surface relative overflow-hidden">
      {live && <Backdrop appIds={art} />}
      <div className="relative px-5 pt-4 pb-5">
        <div className="flex items-center justify-between gap-3 text-[13px]">
          <span className="flex min-w-0 items-center gap-2 font-medium">
            <span className={live ? "text-good" : "faint"}>
              <Equalizer live={live} />
            </span>
            <span className={`truncate first-letter:uppercase ${live ? "text-good" : "sub"}`}>{label}</span>
          </span>
          {data && <span className="faint num shrink-0">Up {span(uptime)}</span>}
        </div>
        <div className="mt-3 flex items-baseline gap-2">
          <Roll value={idling || (data ? games.length : 0)} className="text-[44px] leading-none font-semibold tracking-[-0.03em]" />
          <span className="sub text-[17px]">{(idling || games.length) === 1 ? "game" : "games"}</span>
        </div>
        {idling > 0 && (
          <div className="sub mt-1.5 text-[15px]">
            <Roll value={clock(runSecs)} /> idled this run
          </div>
        )}
        {how && <p className="faint mt-3 text-[13px]">{how}</p>}
      </div>
      <div className="relative border-t border-line">
        <Stats>
          <Stat value={data ? hours(runSecs) : "–"} label="This run" />
          <Stat value={stats ? hours(stats.today + extra) : "–"} label="Today" />
          <Stat value={stats ? hours(stats.all + extra) : "–"} label="All time" />
        </Stats>
      </div>
      {stats && <Milestone total={allTime} rate={live ? idling : 0} />}
      {extras.length > 0 && (
        <div className="rows border-t border-line">
          {extras.map(([k, v]) => (
            <div key={k} className="flex h-11 items-center justify-between px-5 text-[14px]">
              <span className="sub">{k}</span>
              <span className="num font-medium">{v}</span>
            </div>
          ))}
        </div>
      )}
    </section>
  );
}

const STATUS_LABEL: Record<string, string> = { online: "online", away: "away", busy: "busy", invisible: "invisible", offline: "offline" };

// runDescription says how this run was started, in one quiet line.
function runDescription(run: Run, count: number): string {
  const parts: string[] = [];
  if (!run.signedIn) parts.push("Through the Steam client");
  if (run.cards) parts.push("Card drops");
  if (run.all) parts.push("Every game");
  if (run.target) parts.push(`Until ${target(run.target)}`);
  if (count > 32) parts.push(`Turns every ${span(run.rotate || 3600)}`);
  if (run.claim) parts.push("Free games");
  if (run.craft) parts.push("Crafting");
  if (run.events) parts.push("Sale events");
  if (run.signedIn && run.status) {
    const hidden = run.status === "offline" || run.status === "invisible";
    parts.push(hidden ? "Hidden from friends" : `Shown ${STATUS_LABEL[run.status] ?? run.status}`);
  }
  return parts.join(" · ");
}

const MILESTONES = [10, 25, 50, 100, 250, 500, 1000, 2500, 5000, 10000, 25000, 50000, 100000];

// Milestone is the next round number of game-hours and how soon it comes
// at the current pace.
function Milestone({ total, rate }: { total: number; rate: number }) {
  const h = total / 3600;
  const next = MILESTONES.find((m) => m > h);
  if (!next) return null;
  const prev = [...MILESTONES].reverse().find((m) => m <= h) ?? 0;
  const left = next - h;
  const eta = rate > 0 ? left / rate : null; // hours of wall time
  return (
    <div className="relative flex items-center gap-3.5 border-t border-line px-5 py-3.5">
      <Ring value={(h - prev) / (next - prev)}>{Math.floor(((h - prev) / (next - prev)) * 100)}%</Ring>
      <div className="min-w-0 flex-1">
        <div className="text-[15px] font-medium">Next milestone: {next.toLocaleString()} h</div>
        <div className="sub text-[13px]">
          {left < 1 ? `${Math.ceil(left * 60)}m` : `${left.toFixed(1)} h`} to go{eta !== null && ` · about ${eta < 1 ? `${Math.max(1, Math.round(eta * 60))} min` : eta < 48 ? `${eta.toFixed(1)} h` : `${Math.round(eta / 24)} days`} at this pace`}
        </div>
      </div>
    </div>
  );
}
