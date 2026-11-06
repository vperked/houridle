import { useState } from "react";
import GameArt from "./GameArt.tsx";
import { Empty, Group, Stat, Stats } from "./ui.tsx";
import { dayLabel, hours } from "../lib/format.ts";
import type { Day, GameTotal, Stats as StatsData } from "../lib/types.ts";

// History is the History tab: totals, a bar per day, and the games that got
// the most time.
export default function History({ stats, extra, columns }: { stats: StatsData | undefined; extra: number; columns: boolean }) {
  if (!stats) {
    return (
      <div className="surface">
        <Empty icon="history">No history is kept for this run (houridle couldn't open history.json next to it).</Empty>
      </div>
    );
  }
  return (
    <div className={`space-y-7 ${columns ? "lg:grid lg:grid-cols-2 lg:items-start lg:gap-8 lg:space-y-0 [&>*]:min-w-0" : ""}`}>
      <div className="space-y-7">
        <Group footer="Game-hours: idling 3 games for an hour counts as 3." flush>
          <Stats>
            <Stat value={hours(stats.today + extra)} label="Today" />
            <Stat value={hours(stats.week + extra)} label="7 days" />
          </Stats>
          <div className="border-t border-line">
            <Stats>
              <Stat value={hours(stats.month + extra)} label="30 days" />
              <Stat value={hours(stats.all + extra)} label={stats.since ? `Since ${dayLabel(stats.since).short}` : "All time"} />
            </Stats>
          </div>
        </Group>
        <Group title="Last 14 days" flush>
          <DayChart days={stats.days.slice(-14)} since={stats.since} todayExtra={extra} />
        </Group>
      </div>
      <div className="space-y-7">
        {stats.days.length > 14 && (
          <Group title={`Last ${Math.round(stats.days.length / 7)} weeks`} flush>
            <Calendar days={stats.days} todayExtra={extra} />
          </Group>
        )}
        <Group title="Most idled">
          <TopGames games={stats.games} />
        </Group>
      </div>
    </div>
  );
}

function DayChart({ days, since, todayExtra }: { days: Day[]; since?: string; todayExtra: number }) {
  const bars = days.map((d, i) => ({ ...d, time: d.time + (i === days.length - 1 ? todayExtra : 0) }));
  const [picked, setPicked] = useState(bars.length - 1);
  const most = Math.max(1, ...bars.map((d) => d.time));
  const sel = bars[Math.min(picked, bars.length - 1)];
  if (!sel) return <Empty icon="history">Nothing yet</Empty>;

  return (
    <div className="p-4">
      <div className="mb-4 flex items-baseline justify-between">
        <span className="sub text-[14px]">{dayLabel(sel.day).long}</span>
        <span className="num text-[20px] font-semibold tracking-tight">{hours(sel.time)}</span>
      </div>
      <div className="flex h-32 items-end gap-[3px]" role="list" aria-label="Hours idled per day">
        {bars.map((d, i) => {
          const before = since && d.day < since;
          const h = d.time > 0 ? Math.max(3, (100 * d.time) / most) : 0;
          return (
            <button key={d.day} type="button" role="listitem" aria-label={`${dayLabel(d.day).long}: ${hours(d.time)}`} onClick={() => setPicked(i)} className="flex h-full flex-1 flex-col justify-end">
              <span className={`block w-full rounded-[3px] transition-colors ${i === picked ? "bg-accent" : before ? "bg-fill" : "bg-fg/20"}`} style={{ height: h ? `${h}%` : "2px" }} />
            </button>
          );
        })}
      </div>
      <div className="faint num mt-2 flex gap-[3px] text-center text-[10px]">
        {bars.map((d, i) => (
          <span key={d.day} className={`flex-1 ${i === picked ? "font-semibold text-fg" : ""}`}>
            {bars.length <= 14 || i % 5 === 0 || i === bars.length - 1 ? dayLabel(d.day).day : ""}
          </span>
        ))}
      </div>
    </div>
  );
}

function TopGames({ games }: { games: GameTotal[] }) {
  if (!games.length) return <Empty icon="history">Nothing yet</Empty>;
  const most = Math.max(1, ...games.map((g) => g.all));
  return games.map((g) => (
    <div key={g.appId} className="row">
      <GameArt appId={g.appId} name={g.name} className="h-9 w-[84px]" />
      <div className="min-w-0 flex-1">
        <div className="flex items-baseline gap-2">
          <span className="min-w-0 flex-1 truncate text-[15px] font-medium">{g.name || `App ${g.appId}`}</span>
          <span className="num text-[14px] font-medium">{hours(g.all)}</span>
        </div>
        <div className="mt-1.5 flex items-center gap-2">
          <div className="h-[3px] flex-1 overflow-hidden rounded-full bg-fill">
            <div className="h-full rounded-full bg-fg/40" style={{ width: `${(100 * g.all) / most}%` }} />
          </div>
          <span className="faint num w-14 text-right text-[11px]">{g.week ? `${hours(g.week)}/wk` : "–"}</span>
        </div>
      </div>
    </div>
  ));
}

// Calendar is a grid of days, one column per week, darker for more hours,
// with streaks: days in a row with some idling.
function Calendar({ days, todayExtra }: { days: Day[]; todayExtra: number }) {
  const list = days.map((d, i) => ({ ...d, time: d.time + (i === days.length - 1 ? todayExtra : 0) }));
  const [picked, setPicked] = useState<number | null>(null);
  const most = Math.max(1, ...list.map((d) => d.time));
  const level = (t: number) => (t <= 0 ? 0 : Math.min(4, Math.ceil((4 * t) / most)));
  // Weeks start on Monday: pad the first column.
  const [y, m, dd] = list[0].day.split("-").map(Number);
  const pad = (new Date(y, m - 1, dd).getDay() + 6) % 7;
  const cells: (Day | null)[] = [...Array<null>(pad).fill(null), ...list];

  let best = 0;
  let run = 0;
  for (const d of list) {
    run = d.time > 0 ? run + 1 : 0;
    best = Math.max(best, run);
  }
  let current = 0;
  for (let i = list.length - 1; i >= 0; i--) {
    if (list[i].time > 0) current++;
    else if (i === list.length - 1) continue; // today hasn't started yet: count from yesterday
    else break;
  }
  const active = list.filter((d) => d.time > 0).length;
  const sel = picked !== null ? list[picked] : null;

  return (
    <>
      <div className="p-4">
        <div className="mb-3 flex h-5 items-baseline justify-between text-[14px]">
          <span className="sub">{sel ? dayLabel(sel.day).long : "Tap a day"}</span>
          {sel && <span className="num font-semibold">{hours(sel.time)}</span>}
        </div>
        <div className="grid grid-flow-col grid-rows-7 gap-[3px]" role="list" aria-label="Hours idled per day">
          {cells.map((d, i) =>
            d ? (
              <button
                key={d.day}
                type="button"
                role="listitem"
                aria-label={`${dayLabel(d.day).long}: ${hours(d.time)}`}
                onClick={() => setPicked(i - pad)}
                className={`heat-${level(d.time)} aspect-square rounded-[3px] transition-transform active:scale-90 ${picked === i - pad ? "ring-2 ring-fg ring-offset-1 ring-offset-surface" : ""}`}
              />
            ) : (
              <span key={`pad${i}`} />
            ),
          )}
        </div>
        <div className="faint mt-3 flex items-center justify-end gap-1 text-[11px]">
          Less
          {[0, 1, 2, 3, 4].map((l) => (
            <span key={l} className={`heat-${l} size-2.5 rounded-[2px]`} />
          ))}
          More
        </div>
      </div>
      <div className="border-t border-line">
        <Stats>
          <Stat value={`${current} ${current === 1 ? "day" : "days"}`} label="Current streak" />
          <Stat value={`${best} ${best === 1 ? "day" : "days"}`} label="Best streak" />
          <Stat value={active} label="Days idled" />
        </Stats>
      </div>
    </>
  );
}
