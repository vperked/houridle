import { useEffect, useState } from "react";
import { api } from "../api.js";
import { plural } from "../format.js";

// The level is read once per page visit (houridle keeps it for a while,
// since it means reading every badge page).
let cached = null;

export function useLevel(enabled) {
  const [level, setLevel] = useState(cached);
  useEffect(() => {
    if (!enabled || cached) return;
    let live = true;
    api
      .get("/api/level")
      .then((l) => {
        cached = l;
        if (live) setLevel(l);
      })
      .catch(() => {});
    return () => {
      live = false;
    };
  }, [enabled]);
  return level;
}

// LevelCard is the Steam level: a badge with the number, XP toward the
// next level, and the XP in reach from card sets.
export default function LevelCard({ level, onOpen }) {
  if (!level || !level.level) return null;
  const span = Math.max(1, level.nextXp - level.levelXp);
  const pct = Math.min(100, (100 * (level.xp - level.levelXp)) / span);
  const reach = [
    level.ready > 0 && `${plural(level.ready, "badge")} ready to craft (+${level.ready * 100} XP)`,
    level.partial > 0 && `${plural(level.partial, "set")} started, ${plural(level.missing, "card")} missing`,
  ].filter(Boolean);
  const Tag = onOpen ? "button" : "div";
  return (
    <Tag type={onOpen ? "button" : undefined} onClick={onOpen} className={`surface flex w-full items-center gap-4 px-4 py-3.5 text-left ${onOpen ? "row-tap" : ""}`}>
      <span className={`grid size-12 ring-[2.5px] ring-inset shrink-0 place-items-center rounded-full text-[17px] font-bold ${levelTone(level.level)}`}>{level.level}</span>
      <span className="min-w-0 flex-1">
        <span className="flex items-baseline justify-between gap-2">
          <span className="font-medium">Steam level {level.level}</span>
          <span className="sub num text-[13px]">{(level.nextXp - level.xp).toLocaleString()} XP to go</span>
        </span>
        <span className="mt-2 block h-1.5 overflow-hidden rounded-full bg-fill">
          <span className="block h-full rounded-full bg-accent transition-[width] duration-1000" style={{ width: `${pct}%` }} />
        </span>
        {reach.length > 0 && <span className="sub mt-1.5 block truncate text-[13px]">{reach.join(" · ")}</span>}
      </span>
    </Tag>
  );
}

// Steam colors level badges by tens; these are quieter versions.
function levelTone(l) {
  const tones = ["ring-faint", "ring-bad", "ring-warn", "ring-[#eab308]", "ring-good", "ring-accent", "ring-[#a78bfa]", "ring-[#ec4899]", "ring-[#92400e]", "ring-[#64748b]"];
  return tones[Math.floor(l / 10) % 10];
}
