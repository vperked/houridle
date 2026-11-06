import { useEffect, useState } from "react";
import type { CSSProperties } from "react";

// How long the loading screen takes to fade out, and to fade in (it waits a
// moment first, so a quick load never flashes it). Match index.css.
const LEAVE_MS = 260;
const ENTER_DELAY_MS = 150;

// SplashMark is houridle's mark as a clock whose hands go round.
export function SplashMark({ style }: { style?: CSSProperties }) {
  return (
    <span className="splash-mark" aria-hidden="true">
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round">
        <circle cx="12" cy="12" r="9" />
        <line className="splash-hour" x1="12" y1="12" x2="12" y2="8.5" style={style} />
        <line className="splash-minute" x1="12" y1="12" x2="12" y2="6" style={style} />
      </svg>
    </span>
  );
}

// Splash covers the page until the first status arrives (ready), then
// fades away. index.html shows the same markup before the script loads, so
// the hand-over is seamless: the animations are offset to where the page's
// own copy has got to.
export default function Splash({ ready, offline }: { ready: boolean; offline: boolean }) {
  const [since] = useState(() => performance.now());
  const [gone, setGone] = useState(false);
  useEffect(() => {
    if (!ready) return;
    const t = setTimeout(() => setGone(true), LEAVE_MS);
    return () => clearTimeout(t);
  }, [ready]);
  if (gone) return null;

  const hands: CSSProperties = { animationDelay: `${-since}ms` };
  const enter: CSSProperties = { animationDelay: `${ENTER_DELAY_MS - since}ms` };
  return (
    <div className={`splash ${ready ? "splash-leave" : ""}`} role="status" aria-live="polite" aria-busy={!ready}>
      <div className="splash-inner" style={enter}>
        <SplashMark style={hands} />
        <span className="splash-name">houridle</span>
        <span className={`splash-note ${offline ? "splash-note-warn" : ""}`}>{offline ? "Can't reach houridle. Retrying…" : "Loading…"}</span>
      </div>
    </div>
  );
}
