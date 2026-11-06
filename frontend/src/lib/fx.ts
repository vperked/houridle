// Little moments of feedback: a buzz on phones that can, and a short burst
// of confetti for things worth celebrating. Both stay out of the way of
// people who asked for less motion.

const calm = () => matchMedia("(prefers-reduced-motion: reduce)").matches;

export function buzz(pattern: VibratePattern = 12): void {
  try {
    navigator.vibrate?.(pattern);
  } catch {
    // not supported: fine
  }
}

const COLORS = ["#3b8bff", "#22c55e", "#f0b429", "#f05252", "#a78bfa", "#f4f4f5"];

// celebrate bursts confetti from a point (the screen's centre by default).
export function celebrate(x: number = innerWidth / 2, y: number = innerHeight * 0.4): void {
  buzz([10, 40, 18]);
  if (calm()) return;
  const canvas = document.createElement("canvas");
  const dpr = Math.min(devicePixelRatio || 1, 2); // sharper than 2x isn't visible on moving bits
  canvas.width = innerWidth * dpr;
  canvas.height = innerHeight * dpr;
  Object.assign(canvas.style, { position: "fixed", inset: "0", width: "100%", height: "100%", pointerEvents: "none", zIndex: "100" });
  document.body.appendChild(canvas);
  const ctx = canvas.getContext("2d")!; // a fresh canvas always has one
  ctx.scale(dpr, dpr);
  const bits = Array.from({ length: 90 }, () => {
    const angle = -Math.PI / 2 + (Math.random() - 0.5) * Math.PI * 1.1;
    const speed = 6 + Math.random() * 9;
    return {
      x,
      y,
      vx: Math.cos(angle) * speed,
      vy: Math.sin(angle) * speed,
      w: 5 + Math.random() * 5,
      h: 8 + Math.random() * 6,
      r: Math.random() * Math.PI,
      vr: (Math.random() - 0.5) * 0.4,
      c: COLORS[Math.floor(Math.random() * COLORS.length)],
    };
  });
  const start = performance.now();
  const frame = (now: number) => {
    const t = now - start;
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
    ctx.clearRect(0, 0, innerWidth, innerHeight);
    ctx.globalAlpha = Math.max(0, 1 - t / 1600);
    for (const b of bits) {
      b.vy += 0.32;
      b.vx *= 0.985;
      b.x += b.vx;
      b.y += b.vy;
      b.r += b.vr;
      const cos = Math.cos(b.r) * dpr;
      const sin = Math.sin(b.r) * dpr;
      ctx.setTransform(cos, sin, -sin, cos, b.x * dpr, b.y * dpr); // one call instead of save, translate, rotate, restore
      ctx.fillStyle = b.c;
      ctx.fillRect(-b.w / 2, -b.h / 2, b.w, b.h * Math.abs(Math.cos(b.r * 2)));
    }
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
    if (t < 1600) requestAnimationFrame(frame);
    else canvas.remove();
  };
  requestAnimationFrame(frame);
}
