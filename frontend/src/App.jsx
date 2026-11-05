import { useCallback, useEffect, useMemo, useState } from "react";
import { flushSync } from "react-dom";
import Activity from "./components/Activity.jsx";
import Games from "./components/Games.jsx";
import History from "./components/History.jsx";
import Summary from "./components/Summary.jsx";
import BottomNav, { PANEL_TABS, Sidebar } from "./components/Nav.jsx";
import { Banner, ConfirmStop, Footer, Header, NeedsKey, StopButton } from "./components/Chrome.jsx";
import { Avatar, Confirm, Icon, Note, Page, Segmented } from "./components/ui.jsx";
import Idle from "./screens/Idle.jsx";
import LevelCard, { useLevel } from "./components/Level.jsx";
import { hours, plural } from "./format.js";
import Achievements from "./screens/Achievements.jsx";
import More from "./screens/More.jsx";
import { api } from "./api.js";
import { ToastProvider, useAction, useEventNotifications } from "./toast.jsx";
import { useNow, useStatus } from "./useStatus.js";

const STATUS_TABS = [
  { value: "now", label: "Now" },
  { value: "history", label: "History" },
];

export default function App() {
  return (
    <ToastProvider>
      <Shell />
    </ToastProvider>
  );
}

// useTab keeps the open tab (and a screen inside it) in the address
// (#more/shop), so the phone's back button and bookmarks work.
function useTab(valid) {
  const read = useCallback(() => {
    const [t = "", sub = ""] = location.hash.replace("#", "").split("/");
    return valid.includes(t) ? [t, sub] : ["now", ""];
  }, [valid]);
  const [state, setState] = useState(read);
  useEffect(() => {
    const on = () => setState(read());
    addEventListener("hashchange", on);
    return () => removeEventListener("hashchange", on);
  }, [read]);
  const go = useCallback((t, sub = "") => {
    const hash = t === "now" && !sub ? "" : sub ? `${t}/${sub}` : t;
    const change = () => {
      if (location.hash.replace("#", "") !== hash) location.hash = hash;
      flushSync(() => setState([t, sub]));
      scrollTo({ top: 0 });
    };
    // Cross-fade between screens where the browser can.
    if (document.startViewTransition && !matchMedia("(prefers-reduced-motion: reduce)").matches) document.startViewTransition(change);
    else change();
  }, []);
  return [state[0], go, state[1]];
}

const panelTabs = PANEL_TABS.map((t) => t.value);
const statusTabs = STATUS_TABS.map((t) => t.value);

function Shell() {
  const { data, state, fetchedAt, refresh, markStopped } = useStatus();
  const panel = data?.panel;
  const [hashTab, setTab, sub] = useTab(panelTabs);
  // A plain status page only has Now and History.
  const tab = panel || !data || statusTabs.includes(hashTab) ? hashTab : "now";
  const [confirming, setConfirming] = useState(false);
  useEventNotifications(data?.events);
  useShortcuts(panel ? panelTabs : statusTabs, setTab);
  const level = useLevel(!!panel?.signedIn);
  const goSub = useCallback((x) => setTab("more", x), [setTab]);

  if (state === "unauthorized") return <NeedsKey />;
  const live = { data, state, fetchedAt };

  const content = (() => {
    if (!data) return null;
    switch (tab) {
      case "history":
        return <HistoryScreen live={live} title={panel ? "History" : null} />;
      case "idle":
        return panel && <Idle panel={panel} refresh={refresh} goTo={setTab} />;
      case "achievements":
        return panel && <Achievements panel={panel} />;
      case "more":
        return panel && <More panel={panel} refresh={refresh} onQuit={markStopped} sub={sub} goSub={goSub} />;
      default:
        return <NowScreen live={live} refresh={refresh} setTab={setTab} level={level} />;
    }
  })();

  return (
    <main
      className={`mx-auto max-w-xl px-4 pt-[max(0.75rem,env(safe-area-inset-top))] ${panel ? "pb-[calc(5.5rem+env(safe-area-inset-bottom))] lg:ml-60 lg:max-w-6xl lg:px-10 lg:pt-10 lg:pb-12" : "pb-[max(2rem,env(safe-area-inset-bottom))] lg:max-w-2xl lg:pt-8"}`}
    >
      {panel && state !== "stopped" && <Sidebar tab={tab} onChange={setTab} data={data} live={state === "ok"} />}
      <div className={panel ? "lg:hidden" : ""}>
        <Header data={data} live={state === "ok"} />
      </div>
      <Banner state={state} />
      {!panel && (
        <div className="mb-5">
          <Segmented label="Sections" options={STATUS_TABS} value={tab} onChange={setTab} />
        </div>
      )}
      <div key={`${tab}/${sub}`}>{content}</div>

      {/* A status page (houridle start -web) can only stop, which ends houridle. */}
      {!panel && state !== "stopped" && data && <StopButton onClick={() => setConfirming(true)} />}
      {(!panel || tab === "now") && <Footer data={panel && !panel.idling ? null : data} fetchedAt={fetchedAt} />}
      {confirming && (
        <ConfirmStop
          onCancel={() => setConfirming(false)}
          onStopped={() => {
            setConfirming(false);
            markStopped();
          }}
        />
      )}
      {panel && state !== "stopped" && <BottomNav tab={tab} onChange={setTab} />}
    </main>
  );
}

// useLive keeps idling games' clocks ticking between status refreshes. Only
// the screens that show them use it, so the rest of the page redraws only
// when new status arrives.
function useLive({ data, state, fetchedAt }) {
  const now = useNow();
  const since = state === "ok" && fetchedAt ? Math.max(0, (now - fetchedAt) / 1000) : 0;
  const base = data?.games;
  const games = useMemo(
    () => (base ?? []).map((g) => (g.state === "idling" ? { ...g, idled: g.idled + since, total: g.total ? g.total + since : 0 } : g)),
    [base, since],
  );
  const extra = games.filter((g) => g.state === "idling").length * since; // time not yet in the history
  return { now, games, extra };
}

function NowScreen({ live, refresh, setTab, level }) {
  const { data, state } = live;
  const panel = data.panel;
  const { now, games, extra } = useLive(live);
  const showGames = !panel || panel.idling;
  const activity = <Activity events={data.events ?? []} />;
  return (
    <Page title={panel ? "Now" : null} wide={!!panel}>
      {/* On a desktop: the status on the left; the games (or, when not
          idling, the activity) on the right. */}
      <div className={`space-y-7 ${panel ? "lg:grid lg:grid-cols-[minmax(0,5fr)_minmax(0,7fr)] lg:items-start lg:gap-8 lg:space-y-0" : ""}`}>
        <div className="space-y-7">
          <StandingWarning standing={panel?.standing} onOpen={() => setTab("more", "accounts")} />
          {panel && !panel.idling ? (
            <NotIdling panel={panel} refresh={refresh} goTo={setTab} />
          ) : (
            <div className="space-y-3">
              <Summary data={data} games={games} now={now} extra={extra} state={state} />
              {panel?.idling && <IdlingControls refresh={refresh} goTo={setTab} />}
            </div>
          )}
          {level && <LevelCard level={level} onOpen={() => setTab("more", "badges")} />}
          {panel?.extras?.some((e) => e.on) && <AlsoIdling extras={panel.extras.filter((e) => e.on)} onOpen={() => setTab("more", "accounts")} />}
          {showGames && panel && <div className="max-lg:hidden">{activity}</div>}
        </div>
        <div className="space-y-7">
          {showGames ? <Games games={games} loading={!data} /> : activity}
          {showGames && <div className={panel ? "lg:hidden" : ""}>{activity}</div>}
        </div>
      </div>
    </Page>
  );
}

function HistoryScreen({ live, title }) {
  const { extra } = useLive(live);
  return (
    <Page title={title} wide={!!title}>
      <History stats={live.data.stats} extra={extra} columns={!!title} />
    </Page>
  );
}

// NotIdling is the control panel's Now tab when nothing is idling.
function NotIdling({ panel, refresh, goTo }) {
  const [busy, run] = useAction();
  const st = panel.settings;
  const canResume = panel.source && (st.mode === "all" || st.mode === "cards" || (st.games?.length ?? 0) > 0);
  const what = { all: "every game you own", cards: "games with card drops left" }[st.mode] ?? `${st.games?.length ?? 0} game${st.games?.length === 1 ? "" : "s"}`;
  const start = () =>
    run("start", async () => {
      await api.post("/api/idle/start", { ...st, stop: "" });
      await refresh();
    }, "Started idling");
  return (
    <section className="surface px-5 py-6 text-center">
      <span className="mx-auto grid size-12 place-items-center rounded-full bg-fill">
        <Icon name={panel.source ? "play" : "user"} className="size-6" />
      </span>
      <h2 className="mt-3 text-[19px] font-semibold tracking-tight">{!panel.source ? "Sign in to start" : panel.schedule?.paused ? "Waiting for your schedule" : canResume ? "Not idling" : "Pick some games"}</h2>
      <p className="sub mx-auto mt-1 max-w-xs text-[15px]">
        {!panel.source
          ? "Add your Steam account with the Steam mobile app."
          : panel.schedule?.paused
            ? `Idling picks up again ${whenText(panel.schedule.next)}. Start now to idle anyway.`
            : canResume
              ? `Last time: ${what}.`
              : "Choose what to idle, then start."}
      </p>
      <div className="mx-auto mt-5 grid max-w-xs gap-2">
        {canResume && (
          <button type="button" disabled={!!busy} onClick={start} className="btn-primary btn-lg">
            <Icon name="play" className="size-4" strokeWidth={2.4} />
            {busy ? "Starting…" : "Start idling"}
          </button>
        )}
        <button type="button" onClick={() => goTo(panel.source ? "idle" : "more", panel.source ? "" : "accounts")} className={canResume ? "btn btn-lg" : "btn-primary btn-lg"}>
          {panel.source ? (canResume ? "Change what to idle" : "Choose games") : "Sign in"}
        </button>
      </div>
    </section>
  );
}

// IdlingControls stops idling (houridle keeps running) or changes it.
function IdlingControls({ refresh, goTo }) {
  const [busy, run] = useAction();
  const [confirm, setConfirm] = useState(false);
  const stop = () =>
    run("stop", async () => {
      await api.post("/api/idle/stop");
      await refresh();
      setConfirm(false);
    }, "Stopped idling");
  return (
    <>
      <div className="grid grid-cols-2 gap-2">
        <button type="button" onClick={() => goTo("idle")} className="btn">
          <Icon name="sliders" className="size-[18px]" />
          Change
        </button>
        <button type="button" onClick={() => setConfirm(true)} className="btn-danger">
          <Icon name="stop" className="size-[18px]" />
          Stop
        </button>
      </div>
      <Confirm open={confirm} onClose={() => setConfirm(false)} title="Stop idling?" action={busy ? "Stopping…" : "Stop idling"} danger busy={!!busy} onConfirm={stop}>
        Steam stops counting hours for these games. houridle keeps running, so you can start again from here.
      </Confirm>
    </>
  );
}

// useShortcuts: on a keyboard, 1–5 open the tabs and / jumps to the search
// box.
function useShortcuts(tabs, go) {
  useEffect(() => {
    const onKey = (e) => {
      if (e.metaKey || e.ctrlKey || e.altKey || /input|select|textarea/i.test(e.target.tagName)) return;
      const n = Number(e.key);
      if (n >= 1 && n <= tabs.length) {
        go(tabs[n - 1]);
        e.preventDefault();
      } else if (e.key === "/") {
        const box = document.querySelector('input[type="search"]');
        if (box) {
          box.focus();
          e.preventDefault();
        }
      }
    };
    addEventListener("keydown", onKey);
    return () => removeEventListener("keydown", onKey);
  }, [tabs.join(), go]); // eslint-disable-line react-hooks/exhaustive-deps
}

// whenText is "at 22:00" today, "tomorrow at 08:00", or "Mon at 22:00".
function whenText(iso) {
  if (!iso) return "later";
  const d = new Date(iso);
  const t = d.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
  const days = Math.round((new Date(d.toDateString()) - new Date(new Date().toDateString())) / 864e5);
  return days === 0 ? `at ${t}` : days === 1 ? `tomorrow at ${t}` : `${d.toLocaleDateString([], { weekday: "short" })} at ${t}`;
}

// AlsoIdling is the other accounts idling alongside the main one.
function AlsoIdling({ extras, onOpen }) {
  return (
    <section>
      <h2 className="heading">Also idling</h2>
      <div className="surface rows overflow-hidden">
        {extras.map((e) => (
          <button key={e.steamId} type="button" onClick={onOpen} className="row row-tap">
            <Avatar name={e.name} />
            <span className="min-w-0 flex-1">
              <span className="block truncate">{e.name}</span>
              <span className={`block truncate text-[13px] ${e.note ? "text-warn" : "sub"}`}>
                {e.note || (e.idling ? `${plural(e.idling, "game")} · ${e.mode === "cards" ? "card drops" : "every game"}` : "Signing in…")}
              </span>
            </span>
            <span className="num text-[14px] text-good">{e.idled ? hours(e.idled) : ""}</span>
          </button>
        ))}
      </div>
    </section>
  );
}

// StandingWarning shows on Now when Steam's record for the account has a
// ban or a trade restriction.
function StandingWarning({ standing: s, onOpen }) {
  if (!s?.ok || (!s.vacBans && !s.gameBans && s.tradeBan === "None")) return null;
  const parts = [s.vacBans && plural(s.vacBans, "VAC ban"), s.gameBans && plural(s.gameBans, "game ban"), s.tradeBan !== "None" && `trading ${s.tradeBan.toLowerCase()}`].filter(Boolean);
  return (
    <button type="button" onClick={onOpen} className="block w-full text-left">
      <Note tone="bad">
        <b className="font-semibold">Steam's record for this account: {parts.join(", ")}.</b> Tap for details.
      </Note>
    </button>
  );
}
