import { useEffect, useState } from "react";
import type { FormEvent, ReactNode } from "react";
import { Avatar, Badge, Confirm, Empty, ErrorState, Group, Icon, Note, Row, Segmented, Skeleton } from "../../components/ui.tsx";
import { api, errorText } from "../../lib/api.ts";
import { useAction } from "../../components/toast.tsx";
import { Switch } from "../../components/Checkbox.tsx";
import { plural } from "../../lib/format.ts";
import type { AccountInfo, ExtraInfo, Login, Standing as StandingInfo } from "../../lib/types.ts";
import type { MoreScreenProps } from "../More.tsx";

type ScreenProps = Pick<MoreScreenProps, "panel" | "refresh">;

// Accounts: saved sign-ins, other accounts idling alongside, and adding
// one (with the Steam app, or a password).

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
      <SignIn login={panel.login} refresh={refresh} />
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
        houridle forgets this sign-in. To use the account again, add it again below.
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

// SignIn adds an account: with a QR code the Steam app scans, or with the
// account name and password and then Steam Guard (a code from the app or
// an email, or an approval there).
type Way = "qr" | "password";

const WAYS = [
  { value: "qr" as Way, label: "Steam app" },
  { value: "password" as Way, label: "Password" },
];

function SignIn({ login: initial, refresh }: { login: Login; refresh: () => Promise<void> }) {
  const resumed = initial?.state === "waiting" ? initial : null;
  const [login, setLogin] = useState<Login | null>(resumed);
  const [open, setOpen] = useState(!!resumed);
  const [way, setWay] = useState<Way>(resumed?.method === "password" ? "password" : "qr");
  const [user, setUser] = useState("");
  const [password, setPassword] = useState("");
  const [code, setCode] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  // Poll while it's waiting: Steam may rotate the QR code, and the sign-in
  // finishes once it's approved (or the code is in).
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

  const call = async (path: string, body?: unknown) => {
    setBusy(true);
    setError("");
    try {
      setLogin(await api.post<Login>(path, body));
      return true;
    } catch (e) {
      setError(errorText(e) || "Something went wrong");
      return false;
    } finally {
      setBusy(false);
    }
  };

  const beginQR = () => call("/api/login");
  // A QR code is asked for as soon as that way is picked (or started over).
  const qrWaiting = login?.state === "waiting" && login.method !== "password";
  const fresh = !login || login.state === "";
  useEffect(() => {
    if (open && way === "qr" && fresh) beginQR();
  }, [open, way, fresh]); // eslint-disable-line react-hooks/exhaustive-deps

  const signIn = async (e: FormEvent) => {
    e.preventDefault();
    if (await call("/api/login/password", { user: user.trim(), password })) setPassword(""); // not kept once sent
  };
  const sendCode = async (e: FormEvent) => {
    e.preventDefault();
    if (await call("/api/login/code", { code: code.trim().toUpperCase() })) setCode("");
  };
  const reset = () => {
    setLogin(null);
    setError("");
    setCode("");
  };
  const close = () => {
    api.post("/api/login/cancel").catch(() => {});
    reset();
    setPassword("");
    setOpen(false);
  };

  if (!open) {
    return (
      <button type="button" className="btn btn-lg w-full" onClick={() => setOpen(true)}>
        <Icon name="plus" className="size-[18px]" strokeWidth={2.2} />
        Add an account
      </button>
    );
  }
  if (login?.state === "done") {
    return (
      <Note tone="good">
        Signed in as <b>{login.account}</b> and saved.{" "}
        <button type="button" className="font-medium underline" onClick={reset}>
          Add another
        </button>
      </Note>
    );
  }

  const passwordWaiting = login?.state === "waiting" && login.method === "password";
  return (
    <section className="surface animate-fade p-5">
      <div className="mb-4 flex items-center justify-between gap-3">
        <h3 className="font-semibold">Add an account</h3>
        <button type="button" onClick={close} className="btn-ghost btn-sm h-7">
          Cancel
        </button>
      </div>
      {!passwordWaiting && (
        <Segmented
          label="How to sign in"
          options={WAYS}
          value={way}
          onChange={(w) => {
            reset();
            setWay(w);
          }}
        />
      )}

      <div className="mt-4">
        {login?.state === "failed" ? (
          <Note tone="bad">
            {login.error}{" "}
            <button type="button" className="font-medium underline" onClick={() => (way === "qr" ? beginQR() : reset())}>
              Try again
            </button>
          </Note>
        ) : passwordWaiting ? (
          <Guard login={login} code={code} setCode={setCode} busy={busy} onSubmit={sendCode} />
        ) : way === "password" ? (
          <form onSubmit={signIn} className="space-y-3">
            <label className="block">
              <span className="sub mb-1 block text-[13px]">Steam account name</span>
              <input className="input" value={user} onChange={(e) => setUser(e.target.value)} autoComplete="username" autoCapitalize="none" autoCorrect="off" spellCheck={false} required />
            </label>
            <label className="block">
              <span className="sub mb-1 block text-[13px]">Password</span>
              <input className="input" type="password" value={password} onChange={(e) => setPassword(e.target.value)} autoComplete="current-password" required />
            </label>
            <button type="submit" className="btn-primary w-full" disabled={busy || !user.trim() || !password}>
              {busy ? "Signing in…" : "Sign in"}
            </button>
            <p className="faint text-[12px]">houridle passes your password straight to Steam (encrypted with Steam's key) and doesn't keep it. It saves only the sign-in Steam gives back.</p>
          </form>
        ) : qrWaiting ? (
          <div className="text-center">
            <p className="sub text-[13px]">Steam app → Steam Guard → Scan a QR code</p>
            {login.qr && <img src={`data:image/svg+xml;utf8,${encodeURIComponent(login.qr)}`} alt="Sign-in QR code" className="mx-auto mt-4 size-52 rounded-xl bg-white p-2 ring-1 ring-line" />}
            <a className="btn btn-sm mt-4" href={login.url}>
              On this phone? Open in the Steam app
            </a>
            <Waiting>Waiting for you to approve it</Waiting>
          </div>
        ) : (
          <Waiting>{busy ? "Asking Steam for a code…" : "Getting a code…"}</Waiting>
        )}
        {error && (
          <p role="alert" className="mt-3 text-[14px] text-bad">
            {error}
          </p>
        )}
      </div>
    </section>
  );
}

// Guard is the Steam Guard step of a password sign-in: a code to type in,
// or an approval to wait for.
function Guard({ login, code, setCode, busy, onSubmit }: { login: Login; code: string; setCode: (c: string) => void; busy: boolean; onSubmit: (e: FormEvent) => void }) {
  const approve = login.approve === "app" ? "approve it in the Steam app" : login.approve === "email" ? "open the link Steam emailed you" : "";
  if (login.code) {
    const where = login.code === "app" ? "the Steam app" : `your email${login.hint ? ` (${login.hint})` : ""}`;
    return (
      <form onSubmit={onSubmit} className="space-y-3">
        <label className="block">
          <span className="sub mb-1 block text-[13px]">Steam Guard code from {where}</span>
          <input
            className="input num text-center text-[20px] font-semibold tracking-[0.3em] uppercase"
            value={code}
            onChange={(e) => setCode(e.target.value.replace(/\s/g, "").toUpperCase())}
            autoComplete="one-time-code"
            autoCapitalize="characters"
            autoCorrect="off"
            spellCheck={false}
            maxLength={5}
            autoFocus
            required
          />
        </label>
        <button type="submit" className="btn-primary w-full" disabled={busy || code.trim().length < 5}>
          {busy ? "Checking…" : "Continue"}
        </button>
        {approve && <p className="faint text-center text-[13px]">Or {approve}; this goes on by itself.</p>}
      </form>
    );
  }
  return <Waiting>{approve ? `Waiting for you to ${approve}` : "Finishing signing in…"}</Waiting>;
}

function Waiting({ children }: { children: ReactNode }) {
  return (
    <p className="faint mt-3 flex items-center justify-center gap-2 text-[13px]">
      <span className="size-1.5 animate-pulse rounded-full bg-accent" />
      {children}
    </p>
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
