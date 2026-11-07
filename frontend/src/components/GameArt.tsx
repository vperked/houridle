import { useState } from "react";
import type { CSSProperties } from "react";
import { useArt } from "../lib/art.ts";

// GameArt is the game's store picture, or a softly tinted tile with its
// initial if there's none (or it can't load). Each game keeps its own tint,
// so a list of them doesn't read as a column of identical grey boxes.
interface GameArtProps {
  appId: number;
  name: string;
  className?: string;
  variant?: "capsule" | "header";
}

export default function GameArt({ appId, name, className = "", variant = "capsule" }: GameArtProps) {
  const art = useArt(appId);
  const [failed, setFailed] = useState(false);
  const base = `shrink-0 overflow-hidden ${variant === "header" ? "rounded-xl" : "rounded-md"} ${className}`;
  const src = art?.[variant === "header" ? "header" : "capsule"];
  if (art === undefined) return <span className={`${base} shimmer block`} aria-hidden="true" />; // looking it up
  if (!src || failed) {
    const hue = Math.round((appId * 137.508) % 360); // the golden angle spreads neighbours apart
    return (
      <span className={`${base} art-fallback grid place-items-center font-semibold`} style={{ "--hue": hue } as CSSProperties} aria-hidden="true">
        <span className={variant === "header" ? "text-2xl" : "text-sm"}>{(name || "?").trim().charAt(0).toUpperCase()}</span>
      </span>
    );
  }
  return <img src={src} alt="" loading="lazy" referrerPolicy="no-referrer" onError={() => setFailed(true)} className={`${base} bg-fill object-cover ring-1 ring-line ring-inset`} />;
}
