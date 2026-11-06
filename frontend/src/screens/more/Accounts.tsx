import { useEffect, useState } from "react";
import type { ReactNode } from "react";
import { Avatar, Badge, Confirm, Empty, ErrorState, Group, Icon, Note, Row, Segmented, Skeleton } from "../../components/ui.tsx";
import { api } from "../../lib/api.ts";
import { useAction } from "../../components/toast.tsx";
import { Switch } from "../../components/Checkbox.tsx";
import { plural } from "../../lib/format.ts";
import type { AccountInfo, ExtraInfo, Login, Standing as StandingInfo } from "../../lib/types.ts";
import type { MoreScreenProps } from "../More.tsx";

type ScreenProps = Pick<MoreScreenProps, "panel" | "refresh">;

// Accounts: saved sign-ins, other accounts idling alongside, and adding
// one with the Steam app.

export default function Accounts({ panel, refresh }: ScreenProps) {
  const [busy, run] = useAction();
  const [signingOut, setSigningOut] = useState<AccountInfo | null>(null);
  const act = (name: string, path: string, id: string, done: string) => run(name, async () => { await api.post(path, { id }); await refresh(); }, done);

  return (
    <>
      {panel.signedIn && <Standing standing={panel.standing} refresh={refresh} />}
      <Group title="Saved accounts">
        {panel.accounts.length === 0 && !panel.steamClient && <Empty icon="user">No accounts saved yet.</Empty>}
        {panel.accounts.map((a) => {
          const inUse = panel.source === a.steamId;
          return (
            <div key={a.steamId} className="px-4 py-3">
              <div className="flex items-center gap-3">
                <Avatar name={a.name} size="size-9" />
                <span className="min-w-0 flex-1">
                  <span className="flex items-center gap-1.5">
                    <span className="truncate font-medium">{a.name}</span>
                    {inUse && <Badge tone="good">In use</Badge>}
                    {a.default && <Badge>Default</Badge>}
                    {a.damaged && <Badge tone="bad">Damaged</Badge>}
                  </span>
                  <span className="sub num block truncate text-[13px]">{a.expires ? `Signed in until ${new Date(a.expires).toLocaleDateString()}` : a.steamId}</span>
                </span>
              </div>
              <div className="mt-3 flex flex-wrap gap-2 pl-12">
                {!inUse && !a.damaged && (
                  <button type="button" className="btn btn-sm" disabled={!!busy} onClick={() => act("use", "/api/accounts/use", a.steamId, `Switched to ${a.name}`)}>
                    Use
                  </button>
                )}
                {!a.default && !a.damaged && (
                  <button type="button" className="btn btn-sm" disabled={!!busy} onClick={() => act("def", "/api/accounts/default", a.steamId, `${a.name} is now the default`)}>
                    Make default
                  </button>
                )}
                <button type="button" className="btn-danger btn-sm" disabled={!!busy} onClick={() => setSigningOut(a)}>
                  Sign out
                </button>
              </div>
            </div>
          );
        })}
        {panel.steamClient && (
          <Row
            icon="monitor"
            title="Steam client on this computer"
            trailing={
              panel.source === "client" ? (
                <Badge tone="good">In use</Badge>
              ) : (
                <button type="button" className="btn btn-sm" disabled={!!busy} onClick={() => act("use", "/api/accounts/use", "client", "Switched to the Steam client")}>
                  Use
                </button>
              )
            }
          />
        )}
      </Group>
      {(panel.extras?.length ?? 0) > 0 && <Extras extras={panel.extras} refresh={refresh} />}
      <QRSignIn login={panel.login} refresh={refresh} />
      <Confirm
        open={!!signingOut}
        onClose={() => setSigningOut(null)}
        title={`Sign out of ${signingOut?.name}?`}
        action="Sign out"
        danger
        busy={!!busy}
        onConfirm={() => {
          const a = signingOut!;
          setSigningOut(null);
          act("out", "/api/accounts/logout", a.steamId, `Signed out of ${a.name}`);
        }}
      >
        houridle forgets this sign-in. To use the account again, sign in with the Steam app.
      </Confirm>
    </>
  );
}

// Extras idles other saved accounts alongside the main one, each with its
// own Steam connection.
function Extras({ extras, refresh }: { extras: ExtraInfo[]; refresh: () => Promise<void> }) {
  const [busy, run] = useAction();
  const set = (e: ExtraInfo, patch: { mode?: string; on?: boolean }) =>
    run("extra" + e.steamId, async () => {
      await api.post("/api/extras", { steamId: e.steamId, mode: patch.mode ?? e.mode, on: patch.on ?? e.on });
      await refresh();
    }, patch.on === false ? `Stopped idling ${e.name}` : patch.on ? `Idling ${e.name} too` : null);
  return (
    <Group title="Idle alongside" footer="Other saved accounts can idle at the same time, each on its own Steam connection: their card drops, or every game they own.">
      {extras.map((e) => (
        <div key={e.steamId}>
          <label className="row cursor-pointer">
            <Avatar name={e.name} />
            <span className="min-w-0 flex-1">
              <span className="block truncate">{e.name}</span>
              <span className={`block truncate text-[13px] ${e.note ? "text-warn" : "sub"}`}>
                {!e.on ? "Not idling" : e.note || (e.idling ? `${plural(e.idling, "game")} idling` : "Signing in…")}
              </span>
            </span>
            <Switch checked={e.on} disabled={!!busy} onChange={(on) => set(e, { on })} label={`Idle ${e.name} too`} />
          </label>
          {e.on && (
            <div className="px-4 pb-3">
              <Segmented
                label={`What ${e.name} idles`}
                value={e.mode}
                onChange={(mode) => set(e, { mode, on: true })}
                options={[
                  { value: "cards", label: "Card drops" },
                  { value: "all", label: "Every game" },
                ]}
              />
            </div>
          )}
        </div>
      ))}
    </Group>
  );
}

function QRSignIn({ login: initial, refresh }: { login: Login; refresh: () => Promise<void> }) {
  const [login, setLogin] = useState<Login | null>(initial?.state === "waiting" ? initial : null);
  const [busy, run] = useAction();

  // Poll while the code is up: Steam may rotate it, and the app approves it.
  useEffect(() => {
    if (login?.state !== "waiting") return;
    const t = setInterval(async () => {
      try {
        const next = await api.get<Login>("/api/login");
        setLogin(next);
        if (next.state === "done") refresh();
      } catch {
        // keep trying
      }
    }, 2000);
    return () => clearInterval(t);
  }, [login?.state, refresh]);

  const begin = () => run("qr", async () => setLogin(await api.post<Login>("/api/login")));

  if (!login || login.state === "") {
    return (
      <button type="button" className="btn btn-lg w-full" disabled={!!busy} onClick={begin}>
        <Icon name="plus" className="size-[18px]" strokeWidth={2.2} />
        {busy ? "Asking Steam for a code…" : "Add an account"}
      </button>
    );
  }
  if (login.state === "done") {
    return (
      <Note tone="good">
        Signed in as <b>{login.account}</b> and saved.{" "}
        <button type="button" className="font-medium underline" onClick={() => setLogin(null)}>
          Add another
        </button>
      </Note>
    );
  }
  if (login.state === "failed") {
    return (
      <Note tone="bad">
        {login.error}{" "}
        <button type="button" className="font-medium underline" onClick={begin}>
          Try again
        </button>
      </Note>
    );
  }
  return (
    <section className="surface p-5 text-center">
      <h3 className="font-semibold">Scan with the Steam app</h3>
      <p className="sub mt-1 text-[13px]">Steam app → Steam Guard → Scan a QR code</p>
      {login.qr && <img src={`data:image/svg+xml;utf8,${encodeURIComponent(login.qr)}`} alt="Sign-in QR code" className="mx-auto mt-4 size-52 rounded-xl bg-white p-2 ring-1 ring-line" />}
      <a className="btn btn-sm mt-4" href={login.url}>
        On this phone? Open in the Steam app
      </a>
      <p className="faint mt-3 flex items-center justify-center gap-2 text-[13px]">
        <span className="size-1.5 animate-pulse rounded-full bg-accent" />
        Waiting for you to approve it
      </p>
    </section>
  );
}

// Standing is the account's Steam record: bans, trading, whether it's
// limited, and the Market. houridle checks it twice a day and alerts when
// it changes.
function Standing({ standing: s, refresh }: { standing: StandingInfo | undefined; refresh: () => Promise<void> }) {
  const [busy, run] = useAction();
  const check = () =>
    run("check", async () => {
      await api.post("/api/standing/check");
      await refresh();
    }, "Checked your Steam record");
  const ok = <Icon name="check" className="size-4 text-good" strokeWidth={2.6} />;
  const rows: false | undefined | [label: string, value: ReactNode, detail?: ReactNode][] = s?.ok && [
    ["VAC bans", s.vacBans ? <Badge tone="bad">{s.vacBans}</Badge> : ok, s.vacBans && s.daysSinceBan >= 0 ? `Last one ${s.daysSinceBan} day${s.daysSinceBan === 1 ? "" : "s"} ago` : null],
    ["Game bans", s.gameBans ? <Badge tone="bad">{s.gameBans}</Badge> : ok],
    ["Trading", s.tradeBan === "None" ? ok : <Badge tone="bad">{s.tradeBan}</Badge>],
    ["Account", s.limited ? <Badge tone="warn">Limited</Badge> : ok, s.limited ? "Spend $5 on Steam to lift the limits (the Market, friends, profile)" : null],
    ["Market", s.market ? ok : <Badge tone="warn">Restricted</Badge>, s.market ? null : s.marketReason],
  ];
  return (
    <Group
      title="Steam record"
      action={
        <button type="button" onClick={check} disabled={!!busy} className="btn-ghost btn-sm h-7">
          {busy ? "Checking…" : "Check now"}
        </button>
      }
      footer={s ? `Checked ${new Date(s.checked).toLocaleString([], { weekday: "short", hour: "2-digit", minute: "2-digit" })}. houridle checks twice a day and alerts you if anything changes.${s.error ? ` Last check failed: ${s.error}` : ""}` : "houridle checks twice a day and alerts you if anything changes."}
    >
      {!s ? (
        <Skeleton rows={3} />
      ) : !s.ok ? (
        <ErrorState error={s.error || "Not checked yet"} onRetry={check} />
      ) : (
        (rows || []).map(([label, value, detail]) => (
          <div key={label} className="row">
            <span className="min-w-0 flex-1">
              <span className="block">{label}</span>
              {detail && <span className="sub block text-[13px]">{detail}</span>}
            </span>
            {value}
          </div>
        ))
      )}
    </Group>
  );
}
