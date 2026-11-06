import { useState } from "react";
import { errorText } from "../lib/api.ts";
import { dateTime } from "../lib/format.ts";
import type { Status } from "../lib/types.ts";
import type { LoadState } from "../hooks/useStatus.ts";
import { Confirm, Icon, Note } from "./ui.tsx";

// The page's frame: header, banners, footer, the stop dialog and the
// "needs its key" screen.

export function Logo({ className = "size-7" }: { className?: string }) {
  return (
    <span className={`grid shrink-0 place-items-center rounded-[9px] bg-fg text-bg ${className}`} aria-hidden="true">
      <Icon name="clock" className="size-[60%]" strokeWidth={2.2} />
    </span>
  );
}

export function Header({ data, live }: { data: Status | null; live: boolean }) {
  return (
    <header className="mb-4 flex h-10 items-center justify-between gap-3">
      <div className="flex items-center gap-2">
        <Logo />
        <span className="text-[16px] font-semibold tracking-tight">houridle</span>
      </div>
      {data?.account && (
        <div className="flex min-w-0 items-center gap-2 rounded-full border border-line bg-surface py-1 pr-3 pl-1">
          <span className="relative grid size-6 shrink-0 place-items-center rounded-full bg-fill text-[11px] font-semibold">
            {data.account.charAt(0).toUpperCase()}
            <span title={live ? "Live" : "Not updating"} className={`absolute -right-px -bottom-px size-2 rounded-full ring-2 ring-surface ${live ? "bg-good" : "bg-faint"}`} />
          </span>
          <span className="truncate text-[13px] font-medium">{data.account}</span>
        </div>
      )}
    </header>
  );
}

export function Banner({ state }: { state: LoadState }) {
  const text = ({
    offline: "Can't reach houridle. It may have stopped, or this device is off its network. Retrying…",
    stopped: "houridle has stopped. Start it again on the server; its service won't restart it after a Stop.",
  } as Partial<Record<LoadState, string>>)[state];
  if (!text) return null;
  return (
    <div className="mb-4">
      <Note tone={state === "offline" ? "warn" : undefined}>{text}</Note>
    </div>
  );
}

export function Footer({ data, fetchedAt }: { data: Status | null; fetchedAt: number }) {
  if (!data) return null;
  return (
    <p className="faint num mt-8 text-center text-[12px]">
      Started {dateTime(data.started)}
      {data.stopAt && <> · stops {dateTime(data.stopAt)}</>}
      {fetchedAt > 0 && <> · updated {new Date(fetchedAt).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit", second: "2-digit" })}</>}
    </p>
  );
}

export function StopButton({ onClick }: { onClick: () => void }) {
  return (
    <button type="button" onClick={onClick} className="btn-danger btn-lg mt-7 w-full">
      Stop houridle
    </button>
  );
}

export function ConfirmStop({ onCancel, onStopped }: { onCancel: () => void; onStopped: () => void }) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  async function stop() {
    setBusy(true);
    setError("");
    try {
      // The header is what another site can't send, so it can't press Stop.
      const r = await fetch("/api/stop", { method: "POST", headers: { "X-Houridle": "stop" } });
      if (!r.ok) throw new Error(`houridle answered HTTP ${r.status}`);
      onStopped();
    } catch (e) {
      setError(errorText(e) || "Couldn't reach houridle");
      setBusy(false);
    }
  }

  return (
    <Confirm open onClose={onCancel} title="Stop houridle?" action={busy ? "Stopping…" : "Stop"} danger busy={busy} onConfirm={stop}>
      houridle will exit. Start it again on the server; its service won't restart it after a Stop.
      {error && <p className="mt-3 text-bad">{error}</p>}
    </Confirm>
  );
}

export function NeedsKey() {
  return (
    <main className="mx-auto grid min-h-dvh max-w-sm place-items-center px-6 text-center">
      <div>
        <Logo className="mx-auto mb-5 size-12" />
        <h1 className="text-[22px] font-semibold tracking-tight">This page needs its key</h1>
        <p className="sub mt-2 text-[15px]">
          Run <code className="rounded-md bg-fill px-1.5 py-0.5 font-mono text-[13px] text-fg">houridle web</code> on the server and open the link it shows. If you made a new key, old links stop
          working.
        </p>
      </div>
    </main>
  );
}
