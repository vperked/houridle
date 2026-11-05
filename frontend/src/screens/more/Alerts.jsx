import { useState } from "react";
import { Badge, Group, Row } from "../../components/ui.jsx";
import { api } from "../../api.js";
import { notifyLabel } from "../../format.js";
import { useAction } from "../../toast.jsx";
import { Switch } from "../../components/Checkbox.jsx";

// Alerts: the webhook houridle messages, and the weekly summary.

export default function Alerts({ panel, refresh }) {
  const [url, setUrl] = useState("");
  const [busy, run] = useAction();
  const save = () => run("save", async () => { await api.post("/api/notify", { url }); setUrl(""); await refresh(); }, "Sent a test and saved");
  return (
    <>
      <Group title="Status" footer="A message when signing in fails, idling stops working or recovers, every game is done, or a free game is added.">
        <Row icon="bell" title="Alerts" trailing={panel.notify ? <Badge tone="good">{notifyLabel(panel.notifyKind)}</Badge> : <Badge>Off</Badge>} />
        {panel.notify && (
          <>
            <Row title={busy === "test" ? "Sending…" : "Send a test"} onClick={busy ? undefined : () => run("test", () => api.post("/api/notify/test"), "Sent a test message")} chevron={false} className="text-accent" />
            <Row title="Turn off" tone="bad" onClick={busy ? undefined : () => run("off", async () => { await api.post("/api/notify", { url: "" }); await refresh(); }, "Alerts are off")} chevron={false} />
          </>
        )}
      </Group>
      {panel.notify && <Weekly panel={panel} refresh={refresh} />}
      <Group title={panel.notify ? "New webhook" : "Webhook"} footer="Discord, Slack or ntfy. houridle sends a test before saving it." flush>
        <div className="flex gap-2 p-3">
          <input type="url" value={url} onChange={(e) => setUrl(e.target.value)} placeholder="https://…" aria-label="Webhook URL" className="input min-w-0 flex-1" />
          <button type="button" className="btn-primary" disabled={!url || !!busy} onClick={save}>
            {busy === "save" ? "Testing…" : "Save"}
          </button>
        </div>
      </Group>
    </>
  );
}

// Weekly is the weekly summary: on Sunday evenings, a message with the
// week's hours, card drops, badges, achievements and spending.
function Weekly({ panel, refresh }) {
  const [busy, run] = useAction();
  return (
    <Group title="Weekly summary" footer="Sunday evenings: the week's hours, card drops, badges, achievements, money spent and your Steam level.">
      <label className="row cursor-pointer">
        <span className="flex-1">Send a weekly summary</span>
        <Switch checked={panel.weekly} disabled={!!busy} onChange={(on) => run("weekly", async () => { await api.post("/api/weekly", { on }); await refresh(); })} label="Send a weekly summary" />
      </label>
      <Row
        title={busy === "send" ? "Sending…" : "Send one now"}
        className="text-accent"
        chevron={false}
        onClick={busy ? undefined : () => run("send", () => api.post("/api/weekly/send"), (r) => ({ title: "Sent the summary", detail: r.text.split("\n").slice(1).join(" · ") }))}
      />
    </Group>
  );
}
