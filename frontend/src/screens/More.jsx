import { useState } from "react";
import { Confirm, Group, Page, Row } from "../components/ui.jsx";
import { api } from "../api.js";
import { useAction } from "../toast.jsx";
import { notifyLabel, pageAddress } from "../format.js";
import Deals from "./Deals.jsx";
import Inventory from "./Inventory.jsx";
import Shop from "./Shop.jsx";
import Accounts from "./more/Accounts.jsx";
import Alerts from "./more/Alerts.jsx";
import Badges from "./more/Badges.jsx";
import Free from "./more/Free.jsx";
import PageLink from "./more/PageLink.jsx";
import Updates from "./more/Updates.jsx";

// More is everything else houridle's command line does, as a menu: each
// row opens its own screen (#more/<name>), with a back button.
const SECTIONS = [
  { id: "free", title: "Free games", Screen: Free, icon: "gift", group: "Steam", summary: () => "Giveaways and free-to-play" },
  { id: "deals", title: "Games for cards", Screen: Deals, icon: "tag", group: "Steam", summary: () => "Cheap games that pay back in cards" },
  { id: "badges", title: "Badges & XP", Screen: Badges, icon: "award", group: "Steam", summary: () => "Craft badges, sale events" },
  { id: "inventory", title: "Inventory & gems", Screen: Inventory, icon: "cards", group: "Steam", summary: () => "Card values, gems, booster packs" },
  { id: "shop", title: "Badge shop", Screen: Shop, icon: "cart", group: "Steam", summary: () => "Buy the cards a badge is missing" },
  { id: "accounts", title: "Accounts", Screen: Accounts, icon: "user", group: "houridle", summary: (p) => (p.accounts.length ? p.accounts.map((a) => a.name).join(", ") : "Not signed in") },
  { id: "alerts", title: "Alerts", Screen: Alerts, icon: "bell", group: "houridle", summary: (p) => (p.notify ? notifyLabel(p.notifyKind) : "Off") },
  { id: "updates", title: "Updates", Screen: Updates, icon: "refresh", group: "houridle", summary: (p) => (p.update?.available ? `Version ${p.update.latest} is out` : `Version ${p.update?.current ?? ""}`) },
  { id: "page", title: "This page", Screen: PageLink, icon: "link", group: "houridle", summary: (p) => pageAddress(p.webAddr) },
];

export default function More({ panel, refresh, onQuit, sub, goSub }) {
  const signedIn = panel.signedIn;
  const section = SECTIONS.find((x) => x.id === sub);
  const [quitting, setQuitting] = useState(false);
  const [busy, run] = useAction();

  if (section) {
    const Screen = section.Screen;
    return (
      <Page back="More" onBack={() => goSub("")} title={section.title}>
        <Screen signedIn={signedIn} panel={panel} refresh={refresh} />
      </Page>
    );
  }
  const groups = [...new Set(SECTIONS.map((x) => x.group))];
  return (
    <Page title="More">
      {groups.map((g) => (
        <Group key={g} title={g}>
          {SECTIONS.filter((x) => x.group === g).map((x) => (
            <Row key={x.id} icon={x.icon} title={x.title} detail={x.summary(panel)} onClick={() => goSub(x.id)} />
          ))}
        </Group>
      ))}
      <Group>
        <Row icon="power" tone="bad" title="Quit houridle" onClick={() => setQuitting(true)} chevron={false} />
      </Group>
      <Confirm
        open={quitting}
        onClose={() => setQuitting(false)}
        title="Quit houridle?"
        action={busy ? "Quitting…" : "Quit"}
        danger
        busy={!!busy}
        onConfirm={() => run("quit", async () => { await api.post("/api/quit"); setQuitting(false); onQuit(); })}
      >
        houridle exits on the server and this page stops. Its service won't restart it; start it again on the server.
      </Confirm>
    </Page>
  );
}
