import { useState } from "react";
import { Confirm, Group, Icon, Note, Row } from "../../components/ui.tsx";
import { api } from "../../lib/api.ts";
import { useAction } from "../../components/toast.tsx";
import { Switch } from "../../components/Checkbox.tsx";
import type { SteamInfo, UpdateInfo } from "../../lib/types.ts";
import type { MoreScreenProps } from "../More.tsx";

// Updates: keeping houridle up to date, and keeping up with Steam.

// Updates keeps houridle itself up to date from its GitHub releases, and
// shows how it keeps up with Steam.
export default function Updates({ panel, refresh }: Pick<MoreScreenProps, "panel" | "refresh">) {
  const u: Partial<UpdateInfo> = panel.update ?? {};
  const st: Partial<SteamInfo> = panel.steam ?? {};
  const [busy, run] = useAction();
  const [confirm, setConfirm] = useState(false);
  const checked = u.checked ? new Date(u.checked).toLocaleString([], { weekday: "short", hour: "2-digit", minute: "2-digit" }) : "not yet";
  const build = st.build ? new Date(Number(st.build) * 1000).toLocaleDateString([], { year: "numeric", month: "short", day: "numeric" }) : null;
  return (
    <>
      <section className="surface px-5 py-5 text-center">
        <span className={`mx-auto grid size-12 place-items-center rounded-full ${u.available ? "bg-accent text-white" : "bg-fill"}`}>
          <Icon name={u.available ? "down" : "check"} className="size-6" strokeWidth={2.2} />
        </span>
        <h2 className="mt-3 text-[19px] font-semibold tracking-tight">{u.available ? `houridle ${u.latest} is out` : "houridle is up to date"}</h2>
        <p className="sub mt-1 text-[14px]">
          This is version {u.current}. {u.error ? "" : `Checked ${checked}.`}
        </p>
        {u.available && u.notes && <p className="sub mx-auto mt-3 max-w-sm text-left text-[13px] whitespace-pre-line">{u.notes.slice(0, 600)}</p>}
        <div className="mx-auto mt-5 grid max-w-xs gap-2">
          {u.available && (
            <button type="button" disabled={!!busy || u.installing} onClick={() => setConfirm(true)} className="btn-primary btn-lg">
              {u.installing ? "Updating…" : `Update to ${u.latest}`}
            </button>
          )}
          <button
            type="button"
            disabled={!!busy}
            onClick={() => run("check", async () => { const r = await api.post<UpdateInfo>("/api/update/check"); await refresh(); return r; }, (r) => (r.available ? { title: `houridle ${r.latest} is out`, tone: "info" } : "houridle is up to date"))}
            className="btn btn-lg"
          >
            {busy === "check" ? "Checking…" : "Check now"}
          </button>
        </div>
      </section>
      {u.error && <Note tone="warn">{u.error}</Note>}
      <Group title="Updates" footer="houridle checks GitHub every few hours. A new version is checked against the release's SHA256SUMS before it's installed, then houridle restarts and carries on idling.">
        <label className="row cursor-pointer">
          <span className="min-w-0 flex-1">
            <span className="block">Update automatically</span>
            <span className="sub block text-[13px]">{panel.update?.auto ? "New versions install by themselves" : "You'll be told when one is out"}</span>
          </span>
          <Switch checked={!!u.auto} disabled={!!busy} onChange={(on) => run("auto", async () => { await api.post("/api/update/auto", { on }); await refresh(); })} label="Update automatically" />
        </label>
      </Group>
      <Group title="Keeping up with Steam" footer="What houridle tells Steam when signing in comes from steam.json in houridle's repository, checked twice a day, so a change on Steam's side can be fixed there without a new version. Steam's server list is kept too, in case its directory is down.">
        <Row icon="monitor" title="Steam client" detail={build ? `Valve's current, ${build}` : "Not checked yet"} />
        <Row icon="key" title="Sign-in settings" detail={st.fromRepo ? "From steam.json" : "Built in for now"} trailing={<span className="num text-[13px]">{st.protocol} · {st.client}</span>} />
      </Group>
      <Confirm
        open={confirm}
        onClose={() => setConfirm(false)}
        title={`Update to houridle ${u.latest}?`}
        action={busy === "install" ? "Updating…" : "Update and restart"}
        busy={busy === "install"}
        onConfirm={() => run("install", async () => { await api.post("/api/update/install"); setConfirm(false); }, { title: "Updating", detail: "houridle restarts in a moment and carries on idling. This page reconnects by itself." })}
      >
        houridle downloads version {u.latest}, checks it, and restarts with it. Idling pauses for a few seconds and then carries on.
      </Confirm>
    </>
  );
}
