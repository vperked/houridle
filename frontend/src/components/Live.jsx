import { useEffect, useState } from "react";
import { useArt } from "../art.js";

// Roll shows a number or clock whose digits roll into place when they
// change, like a mechanical counter. Each digit's cell holds an invisible
// "0", so it lines up with the text around it like any other digit.
export function Roll({ value, className = "" }) {
  return (
    <span className={`num ${className}`} aria-label={String(value)}>
      {String(value)
        .split("")
        .map((ch, i) =>
          /\d/.test(ch) ? (
            <span key={i} aria-hidden="true" className="relative inline-block">
              <span className="invisible">0</span>
              <span className="absolute inset-0 overflow-hidden">
                <span className="roll-strip absolute inset-x-0 top-0 flex flex-col text-center" style={{ transform: `translateY(${-Number(ch) * 10}%)` }}>
                  {DIGITS.map((d) => (
                    <span key={d}>{d}</span>
                  ))}
                </span>
              </span>
            </span>
          ) : (
            <span key={i} aria-hidden="true">
              {ch}
            </span>
          ),
        )}
    </span>
  );
}
const DIGITS = [0, 1, 2, 3, 4, 5, 6, 7, 8, 9];

// Equalizer is three bars bouncing while something is live.
export function Equalizer({ live }) {
  return (
    <span className={`eq inline-flex h-3 items-end gap-[2px] ${live ? "" : "eq-paused"}`} aria-hidden="true">
      <span className="w-[3px] rounded-full bg-current" />
      <span className="w-[3px] rounded-full bg-current" />
      <span className="w-[3px] rounded-full bg-current" />
    </span>
  );
}

// Backdrop is the idling games' artwork, blurred, slowly taking turns
// behind the status card.
export function Backdrop({ appIds }) {
  const [i, setI] = useState(0);
  const key = appIds.join(",");
  useEffect(() => {
    setI(0);
    if (appIds.length < 2) return;
    const t = setInterval(() => setI((n) => (n + 1) % appIds.length), 8000);
    return () => clearInterval(t);
  }, [key]); // eslint-disable-line react-hooks/exhaustive-deps
  if (!appIds.length) return null;
  const current = appIds[i % appIds.length];
  const previous = appIds[(i + appIds.length - 1) % appIds.length];
  return (
    <span className="pointer-events-none absolute inset-0 overflow-hidden" aria-hidden="true">
      {/* Only the current picture and the one fading out are loaded. */}
      {[...new Set([previous, current])].map((id) => (
        <BackdropImage key={id} appId={id} shown={id === current} />
      ))}
      <span className="backdrop-veil absolute inset-0" />
    </span>
  );
}

function BackdropImage({ appId, shown }) {
  const art = useArt(appId);
  if (!art?.header) return null;
  return (
    <img
      src={art.header}
      alt=""
      referrerPolicy="no-referrer"
      onError={(e) => (e.currentTarget.style.display = "none")}
      className={`backdrop-art absolute inset-0 size-full scale-150 object-cover transition-opacity duration-[2000ms] ${shown ? "opacity-100" : "opacity-0"}`}
    />
  );
}

// Ring is a small circular progress indicator.
export function Ring({ value, size = 44, stroke = 4, children }) {
  const r = (size - stroke) / 2;
  const c = 2 * Math.PI * r;
  return (
    <span className="relative inline-grid shrink-0 place-items-center" style={{ width: size, height: size }}>
      <svg width={size} height={size} className="-rotate-90" aria-hidden="true">
        <circle cx={size / 2} cy={size / 2} r={r} fill="none" stroke="currentColor" strokeWidth={stroke} className="text-fill" />
        <circle
          cx={size / 2}
          cy={size / 2}
          r={r}
          fill="none"
          stroke="currentColor"
          strokeWidth={stroke}
          strokeLinecap="round"
          strokeDasharray={c}
          strokeDashoffset={c * (1 - Math.min(1, Math.max(0, value)))}
          className="text-accent transition-[stroke-dashoffset] duration-1000 ease-out"
        />
      </svg>
      <span className="absolute text-[11px] font-semibold">{children}</span>
    </span>
  );
}
