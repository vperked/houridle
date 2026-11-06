import { useState } from "react";
import { useArt } from "../lib/art.ts";

// GameArt is the game's store picture, or a quiet grey tile with its
// initial if there's none (or it can't load).
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
  if (art === undefined) return <span className={`${base} block bg-fill`} aria-hidden="true" />; // looking it up
  if (!src || failed) {
    return (
      <span className={`${base} sub grid place-items-center bg-fill font-semibold`} aria-hidden="true">
        <span className={variant === "header" ? "text-2xl" : "text-sm"}>{(name || "?").trim().charAt(0).toUpperCase()}</span>
      </span>
    );
  }
  return <img src={src} alt="" loading="lazy" referrerPolicy="no-referrer" onError={() => setFailed(true)} className={`${base} bg-fill object-cover ring-1 ring-line ring-inset`} />;
}
