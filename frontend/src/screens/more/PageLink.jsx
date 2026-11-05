import { useState } from "react";
import { Confirm, Group, Row } from "../../components/ui.jsx";
import { api } from "../../api.js";
import { pageAddress } from "../../format.js";
import { useAction } from "../../toast.jsx";

// This page: where it's served, and making a new key.

export default function PageLink({ panel }) {
  const [links, setLinks] = useState(null);
  const [busy, run] = useAction();
  const [confirm, setConfirm] = useState(false);
  return (
    <>
      <Group title="Serving on" footer="Its link includes a key: anyone with the link can control houridle. Keep it on your own network.">
        <Row icon="link" title={pageAddress(panel.webAddr)} detail="Open it on any device on this network" />
      </Group>
      {links && (
        <Group title="New links">
          {links.map((l) => (
            <div key={l} className="row">
              <code className="min-w-0 flex-1 truncate font-mono text-[13px]">{l}</code>
              <button type="button" className="btn btn-sm" onClick={() => run("copy", () => navigator.clipboard?.writeText(l), "Copied")}>
                Copy
              </button>
            </div>
          ))}
        </Group>
      )}
      <Group>
        <Row icon="key" title="Make a new key" detail="Old links stop working" onClick={() => setConfirm(true)} chevron={false} />
      </Group>
      <Confirm
        open={confirm}
        onClose={() => setConfirm(false)}
        title="Make a new key?"
        action={busy ? "Making…" : "Make a new key"}
        busy={!!busy}
        onConfirm={() => run("key", async () => { const r = await api.post("/api/webkey"); setLinks(r.links); setConfirm(false); }, "Made a new key")}
      >
        Every link you've shared stops working. This browser stays signed in, and you'll get the new links to share.
      </Confirm>
    </>
  );
}
