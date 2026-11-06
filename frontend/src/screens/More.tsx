import { useState } from "react";
import type { ComponentType } from "react";
import { Confirm, Group, Page, Row } from "../components/ui.tsx";
import { api } from "../lib/api.ts";
import { useAction } from "../components/toast.tsx";
import { notifyLabel, pageAddress } from "../lib/format.ts";
import Deals from "./Deals.tsx";
import Inventory from "./Inventory.tsx";
import Shop from "./Shop.tsx";
import Accounts from "./more/Accounts.tsx";
import Alerts from "./more/Alerts.tsx";
import Badges from "./more/Badges.tsx";
import Free from "./more/Free.tsx";
import PageLink from "./more/PageLink.tsx";
import Updates from "./more/Updates.tsx";
import type { IconName, Tint } from "../components/ui.tsx";
import type { Panel } from "../lib/types.ts";

// MoreScreenProps is what each of More's screens is given.
export interface MoreScreenProps {
  signedIn: boolean;
  panel: Panel;
  refresh: () => Promise<void>;
}

interface Section {
  id: string;
  tint?: Tint;
  title: string;
  Screen: ComponentType<MoreScreenProps>;
  icon: IconName;
  group: string;
  summary: (p: Panel) => string;
}

// More is everything else houridle's command line does, as a menu: each
// row opens its own screen (#more/<name>), with a back button.
const SECTIONS: Section[] = [
  { id: "free", tint: "green", title: "Free games", Screen: Free, icon: "gift", group: "Steam", summary: () => "Giveaways and free-to-play" },
  { id: "deals", tint: "amber", title: "Games for cards", Screen: Deals, icon: "tag", group: "Steam", summary: () => "Cheap games that pay back in cards" },
  { id: "badges", tint: "violet", title: "Badges & XP", Screen: Badges, icon: "award", group: "Steam", summary: () => "Craft badges, sale events" },
  { id: "inventory", tint: "sky", title: "Inventory & gems", Screen: Inventory, icon: "cards", group: "Steam", summary: () => "Card values, gems, booster packs" },
  { id: "shop", tint: "pink", title: "Badge shop", Screen: Shop, icon: "cart", group: "Steam", summary: () => "Buy the cards a badge is missing" },
  { id: "accounts", tint: "indigo", title: "Accounts", Screen: Accounts, icon: "user", group: "houridle", summary: (p) => (p.accounts.length ? p.accounts.map((a) => a.name).join(", ") : "Not signed in") },
  { id: "alerts", tint: "red", title: "Alerts", Screen: Alerts, icon: "bell", group: "houridle", summary: (p) => (p.notify ? notifyLabel(p.notifyKind) : "Off") },
  { id: "updates", tint: "teal", title: "Updates", Screen: Updates, icon: "refresh", group: "houridle", summary: (p) => (p.update?.available ? `Version ${p.update.latest} is out` : `Version ${p.update?.current ?? ""}`) },
  { id: "page", title: "This page", Screen: PageLink, icon: "link", group: "houridle", summary: (p) => pageAddress(p.webAddr) },
];

interface MoreProps {
  panel: Panel;
  refresh: () => Promise<void>;
  onQuit: () => void;
  sub: string;
  goSub: (sub: string) => void;
}

export default function More({ panel, refresh, onQuit, sub, goSub }: MoreProps) {
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
            <Row key={x.id} icon={x.icon} tint={x.tint} title={x.title} detail={x.summary(panel)} onClick={() => goSub(x.id)} />
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
