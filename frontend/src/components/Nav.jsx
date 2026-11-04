import { Logo } from "./Chrome.jsx";
import { Dot, Icon } from "./ui.jsx";

// BottomNav is the control panel's tab bar, docked to the bottom edge like
// a phone app's.
export const PANEL_TABS = [
  { value: "now", label: "Now", icon: "now" },
  { value: "idle", label: "Idle", icon: "play" },
  { value: "achievements", label: "Trophies", icon: "trophy" },
  { value: "history", label: "History", icon: "history" },
  { value: "more", label: "More", icon: "more" },
];

export default function BottomNav({ tab, onChange }) {
  return (
    <nav aria-label="Sections" className="vt-tabbar fixed inset-x-0 bottom-0 z-30 border-t lg:hidden border-line bg-surface/85 pb-[env(safe-area-inset-bottom)] backdrop-blur-xl backdrop-saturate-150">
      <div className="mx-auto flex max-w-xl">
        {PANEL_TABS.map((t) => {
          const on = tab === t.value;
          return (
            <button
              key={t.value}
              type="button"
              aria-current={on ? "page" : undefined}
              onClick={() => onChange(t.value)}
              className={`group flex h-[52px] min-w-0 flex-1 flex-col items-center justify-center gap-[3px] text-[10px] font-medium transition-colors ${on ? "text-accent" : "faint"}`}
            >
              <span className={`transition-transform duration-200 group-active:scale-85 ${on ? "scale-105" : ""}`}>
                <Icon name={t.icon} className="size-[22px]" strokeWidth={on ? 2.1 : 1.8} />
              </span>
              <span className="max-w-full truncate px-1">{t.label}</span>
            </button>
          );
        })}
      </div>
    </nav>
  );
}

// Sidebar is the control panel's navigation on a desktop: the logo, the
// sections, and the account at the bottom.
export function Sidebar({ tab, onChange, data, live }) {
  return (
    <aside className="vt-tabbar fixed inset-y-0 left-0 z-30 hidden w-60 flex-col border-r border-line bg-surface/60 px-3 py-5 backdrop-blur-xl lg:flex">
      <div className="mb-6 flex items-center gap-2.5 px-3">
        <Logo />
        <span className="text-[17px] font-semibold tracking-tight">houridle</span>
      </div>
      <nav aria-label="Sections" className="space-y-0.5">
        {PANEL_TABS.map((t) => {
          const on = tab === t.value;
          return (
            <button
              key={t.value}
              type="button"
              aria-current={on ? "page" : undefined}
              onClick={() => onChange(t.value)}
              className={`flex h-10 w-full items-center gap-3 rounded-xl px-3 text-[15px] font-medium transition-colors ${on ? "bg-fill text-fg" : "sub hover:bg-fill hover:text-fg"}`}
            >
              <Icon name={t.icon} className={`size-5 ${on ? "text-accent" : ""}`} strokeWidth={on ? 2.1 : 1.8} />
              {t.value === "achievements" ? "Achievements" : t.label}
            </button>
          );
        })}
      </nav>
      <div className="mt-auto space-y-3 px-3">
        {data?.account && (
          <div className="flex items-center gap-2.5">
            <span className="grid size-8 shrink-0 place-items-center rounded-full bg-fill text-[13px] font-semibold">{data.account.charAt(0).toUpperCase()}</span>
            <span className="min-w-0 flex-1">
              <span className="block truncate text-[14px] font-medium">{data.account}</span>
              <span className="sub flex items-center gap-1.5 text-[12px]">
                <Dot live={live} />
                {live ? "Connected" : "Not updating"}
              </span>
            </span>
          </div>
        )}
        {data && <p className="faint text-[12px]">houridle {data.version} · keys 1–5</p>}
      </div>
    </aside>
  );
}
