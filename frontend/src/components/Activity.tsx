import { memo, useMemo, useState } from "react";
import { Empty, Pager } from "./ui.tsx";
import { usePages } from "../hooks/usePages.ts";
import { timeOfDay } from "../lib/format.ts";
import { LEVEL_DOT } from "../lib/states.ts";
import type { StatusEvent } from "../lib/types.ts";

const COLLAPSED = 5;
const PAGE = 20; // events a page, once they're all shown

// Activity is the recent events, newest first, with problems easy to spot.
// Memoized: events only change when a status poll brings new ones, not on
// each tick of the clocks around it.
export default memo(function Activity({ events }: { events: StatusEvent[] }) {
  const [all, setAll] = useState(false);
  const [problemsOnly, setProblemsOnly] = useState(false);
  const list = useMemo(() => [...events].reverse().filter((e) => !problemsOnly || e.level === "warn" || e.level === "bad"), [events, problemsOnly]);
  const problems = useMemo(() => events.filter((e) => e.level === "warn" || e.level === "bad").length, [events]);
  const p = usePages(list, PAGE, String(problemsOnly));
  const shown = all ? p.items : list.slice(0, COLLAPSED);

  return (
    <section>
      <div className="flex items-end justify-between">
        <h2 className="heading">Activity</h2>
        {problems > 0 && (
          <button type="button" onClick={() => setProblemsOnly(!problemsOnly)} className="btn-ghost btn-sm mb-1 h-7">
            {problemsOnly ? "Show all" : `Problems (${problems})`}
          </button>
        )}
      </div>
      <div className="surface overflow-hidden">
        {list.length === 0 ? (
          <Empty icon="clock">{problemsOnly ? "No problems" : "Nothing yet"}</Empty>
        ) : (
          <>
            <ol ref={p.ref} className="rows">
              {shown.map((e, i) => (
                <li key={`${e.at}-${i}`} className="flex gap-3 px-4 py-3 text-[14px]">
                  <span className={`mt-[7px] size-1.5 shrink-0 rounded-full ${LEVEL_DOT[e.level] ?? LEVEL_DOT.info}`} />
                  <span className={`min-w-0 flex-1 break-words ${e.level === "bad" ? "text-bad" : ""}`}>{e.text}</span>
                  <time dateTime={e.at} className="faint num shrink-0 text-[12px] leading-5 whitespace-nowrap">
                    {timeOfDay(e.at)}
                  </time>
                </li>
              ))}
            </ol>
            {all && <Pager {...p} />}
            {list.length > COLLAPSED && (
              <button type="button" onClick={() => {
                  setAll(!all);
                  p.setPage(0);
                }} className="row-tap w-full border-t border-line py-3 text-center text-[14px] text-accent">
                {all ? "Show less" : `Show all ${list.length}`}
              </button>
            )}
          </>
        )}
      </div>
    </section>
  );
});
