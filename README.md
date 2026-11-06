# houridle

A small tool for Steam, for Windows and Linux:

- **Idle hours.** Your games count as being played while houridle runs, up to 32 at once.
- **Farm trading cards.** Idle exactly the games that still have card drops, until they've all dropped.
- **Unlock achievements.** Tick the ones you want, press Enter.
- **Run it from your phone.** A control panel for everything houridle does, alerts when something goes wrong, and a running total of the hours gained.

By default it works through the Steam client you're already signed in to, and no games are launched. Or sign in to Steam through houridle itself (`houridle login`) and idle with no Steam client at all (`houridle start`), which is also what unlocks card farming, hour targets and the other extras below.

## Getting started

**On a Linux server** (the usual way: it idles around the clock and you run it from your phone):

```bash
git clone https://github.com/vperked/houridle
cd houridle
./install.sh
```

That builds houridle (or downloads the latest release if Go isn't installed), installs it to `~/houridle` with a service that starts it at boot, starts it, and prints the control panel's link. Open the link on your phone, sign in with the Steam app (**More → Accounts**), and pick what to idle (**Idle**). That's it: it carries on after a reboot and keeps itself up to date. Options: `--port 8095` (the default is 8080), `--no-start`, or `--no-service` to install the program only.

**On a Windows PC:** download `houridle.exe` from the [releases](https://github.com/vperked/houridle/releases) (or build it, below), open Steam, and run it. For the phone control panel, run `houridle serve` instead.

## Building it yourself

You need [Go](https://go.dev/dl/) 1.26 or newer and [Node.js](https://nodejs.org) 22 or newer: the control panel's page is TypeScript, built first and compiled into houridle.

```bash
make
```

That builds the page, then houridle, into `bin/` (not in git): run houridle from there, and the files it keeps next to itself (`config.json`, `houridle.log`, the Steam API library) stay there too. On Windows, double-click `build.bat`, which builds `bin/houridle.exe` and `bin/houridle` for Linux. Once the page is built (`make page`), plain Go builds include it, for any system and with no C compiler: `GOOS=windows go build -o bin/houridle.exe ./backend/cmd/houridle`. A houridle built without the page still works; its control panel just says the page wasn't built. `make test` runs the tests and `make all-os` builds every release file.

Ready-made builds are attached to each [release](https://github.com/vperked/houridle/releases): `houridle.exe`, `houridle-linux-amd64` and `houridle-linux-arm64` (rename it `houridle`), with a `SHA256SUMS` file to check them against.

## Use

Open Steam and sign in, then run houridle. The first time, it asks which games to idle. Each time you start idling, it asks whether to stop by itself: type hours (`3`), a time (`23:30`), or press Enter to keep going.

| Command | What it does |
|---|---|
| `houridle` | Idle your saved games |
| `houridle 730 440` | Idle these games (by app ID) |
| `houridle -stop 3h` | Stop after 3 hours, without asking (`90m` and `23:30` work too) |
| `houridle -stop never` | Keep going until you stop it, without asking |
| `houridle -setup` | Choose which games to idle, save them, then start idling them (`houridle -setup 730 440` starts the picker with those ticked) |
| `houridle -list` | Show your games and their app IDs |
| `houridle -achievements` | Unlock or lock achievements |
| `houridle -tray` | Idle in the system tray, by the clock (Windows) |
| `houridle -autostart on` | Start in the tray when you sign in (Windows); `off` to undo |
| `houridle -notify <url>` | Send alerts to a webhook for this run (see [Alerts](#alerts)) |
| `houridle serve` | Run as a control panel: do everything from its web page (see [Control panel](#control-panel)) |
| `houridle -web :8080` | Serve a status page for this run (see [Control panel](#control-panel)) |
| `houridle stats` | The hours houridle has idled: today, this week, per game (see [Hours history](#hours-history)) |

An app ID is the number in a game's Steam store link, like `store.steampowered.com/app/730`.

Flags can go before or after app IDs: `houridle 730 440 -stop 3h` works too.

Your game list is saved in `config.json` next to houridle. If that folder can't be written to (for example `C:\Program Files`), houridle uses your user config folder instead (`%AppData%\houridle` on Windows, `~/.config/houridle` on Linux), for `houridle.log` too.

### Signing in to Steam (optional)

| Command | What it does |
|---|---|
| `houridle login` | Asks how to sign in: a QR code to scan with the Steam mobile app, or your account name and password |
| `houridle login -qr` / `-password` | Skip the question and pick the method (`-method qr` or `-method password` mean the same). With a password, Steam Guard (app code, email code or app approval) is asked for as needed |
| `houridle status` | Whether you're signed in, whether Steam accepts it, whether Steam is reachable and whether houridle is idling |
| `houridle account` | Your Steam username and SteamID64 |
| `houridle logout` | Delete the saved sign-in from this computer |

```bash
houridle login
houridle status
houridle start
```

You sign in once. houridle keeps a Steam *refresh token*, never your password, and uses it every time it starts. While idling, it renews the token over its Steam connection when less than 90 days are left, and it reconnects by itself if the connection drops. If Steam ever stops accepting it (it expired, or you signed out all devices), `status` and `start` say so and you run `houridle login` again.

- **No keyboard (a service, a container, a script):** set `HOURIDLE_USER` and `HOURIDLE_PASSWORD` and `houridle login` signs in with them, no prompts. If Steam asks for a Steam Guard code, also set `HOURIDLE_GUARD_CODE` (codes are single-use and expire in seconds); with mobile-app approval enabled it just waits for you to approve. houridle removes the password and code from its own environment as soon as it reads them. You only need them once: the saved sign-in is used from then on, so unset them afterwards. Anything in an environment can be read by other programs running as you, so prefer the QR code on a machine you use, and on a server put them in a `chmod 600` file loaded for that one command, never in a service file or shell profile.
- The sign-in is saved in your user config folder (`%AppData%\houridle` on Windows, `~/.config/houridle` on Linux). On Windows it is encrypted for your account with DPAPI; on Linux the file is readable only by you (mode 600). Tokens are never printed or written to `houridle.log`.
- `logout` removes the token from this computer. Steam may keep the token valid until it expires; sign out of all devices in your Steam security settings to end it everywhere.

### Idling with the sign-in

`houridle start` idles over houridle's own connection to Steam, so Steam doesn't need to be installed. It takes the same games, `-stop`, `-setup`, dashboard and `-tray` as above, plus:

| Command | What it does |
|---|---|
| `houridle start [ids]` | Idle your saved games, or these app IDs |
| `houridle start -cards` | Idle the games that still have trading card drops, and stop when they've all dropped |
| `houridle start -until 100h` | Stop idling each game once Steam has 100 hours on record for it (`100` and `90m` work too) |
| `houridle start -all` | Idle every game you own; games you buy later join by themselves |
| `houridle start -all -until 10h` | Idle every game you own with under 10 hours on record, each until it gets there (`-under 10h` means the same) |
| `houridle start -claim` | Also add Steam's free giveaways to your account, checking every 12 hours (see [Free games](#free-games)) |
| `houridle start -craft` | Also craft game badges whenever a full card set is there (see [Badges and Steam XP](#badges-and-steam-xp)) |
| `houridle start -events` | During Steam's sales, also explore the discovery queue and claim the daily sticker |
| `houridle start -refundable` | Also idle games you could still refund; they're held back otherwise (see below) |
| `houridle start -rotate 2h` | With more than 32 games: how often they take turns (default every hour) |
| `houridle start -status away` | How you show to friends while idling: `online` (the default), `away`, `busy`, `invisible` or `offline` |
| `houridle start -achievements` | Unlock or lock achievements, no Steam client needed |
| `houridle start -list` | Show your games; with no Steam installed, the ones your Steam account owns |

- **Games.** With no Steam installed, `houridle start -setup` (or the first `start`) offers to load the games your Steam account owns, then shows the usual picker.
- **Real hours.** The dashboard shows each game's hours on record at Steam, read every 10 minutes.
- **Card drops.** `-cards` reads your badge pages to find the games with drops left, idles them, checks again every 20 minutes, drops each game when its last card arrives, and stops when there are none left. It needs no game list.
- **Hour targets.** With `-until`, a game is done once it reaches the target, and houridle stops when all of them are.
- **Everything you own.** `-all` reads your library from Steam (every 10 minutes, so new purchases join) and idles all of it, taking turns past 32. With `-until`, games already past the target are left out.
- **More than 32 games.** Steam counts at most 32 at a time. Pick more and they take turns, the ones that have idled least going first.
- **What friends see.** houridle shows you as Online and in-game, like the Steam client does when you play. With `offline` or `invisible`, Steam still records the hours, but nobody sees you in a game. Set `"status": "away"` (or any of them) in `config.json` to make it the default.
- **Refunds.** Steam refunds a game bought in the last 14 days with under 2 hours played, and idling counts as playing. So `start` holds back any game you could still refund: it shows as *Refundable* with the days left and its playtime, you get an alert (and another when only 2 days are left), and it joins in by itself once the 14 days are over or you've played 2 hours. Purchases are read from your account's licenses; keys bought elsewhere, free games and family-shared games aren't held. `-refundable` idles them anyway (in the control panel: **While idling → Idle refundable games**). This needs the sign-in, so idling through the Steam client doesn't hold games back.
- **Playing elsewhere.** Steam lets one device play at a time. If you start a game on another device, `start` pauses and resumes when you stop; it never kicks you out of your own game.
- **One connection per sign-in.** A second houridle using the same sign-in takes over the first, which reconnects a minute later. So `-list`, `-setup` (when it loads your games) and `-achievements` refuse while houridle is idling, and `status` doesn't connect then.

### Several accounts

| Command | What it does |
|---|---|
| `houridle login` | Sign in again as another account to add it |
| `houridle accounts` | List the saved accounts; `*` marks the default |
| `houridle start -account name` | Idle another saved account (name or SteamID). Also works with `status`, `account` and `logout` |
| `houridle account -default name` | Make a saved account the default |

The first account you sign in with is the default; commands use it unless you pass `-account`. Signing in again as an account you already have refreshes just that one. Each account idles in its own houridle, with its own game list (in `config.json`) and its own log (`houridle-name.log`), so several can idle at once. Logging out of the default makes the next account the default.

### Alerts

On a server you don't watch, have houridle tell you when something needs you:

```bash
houridle notify https://discord.com/api/webhooks/...
```

It sends a test message and saves the webhook in `config.json`. A Slack webhook or an [ntfy](https://ntfy.sh) topic (`https://ntfy.sh/your-topic`) works too, or anything that takes a plain-text POST. You get a message when Steam refuses the sign-in or idling stops working, when it works again, and when every game is done. The same message isn't repeated within six hours. `houridle notify off` stops them; `houridle notify` alone sends another test.

### Control panel

Run houridle from your phone (or any browser):

```bash
houridle serve
houridle web
```

`houridle serve` keeps houridle running as a control panel, and `houridle web` prints its link, like `http://192.168.1.20:8080/?key=...`. Open it once; it remembers you after that. From the page you can do everything the command line does:

| Tab | What's there |
|---|---|
| **Now** | What's idling (over the games' blurred artwork), hours this run, today and in all, the next milestone and when you'll reach it at this pace, each game's hours on record, progress and card drops, recent activity; **Start**, **Stop idling** and **Change** |
| **Idle** | Which account to idle with (or the Steam client), what to idle (pick games from your library, sorted by playtime, name or card drops left; everything you own; or card drops) and how: stop time, hour target, turns, what friends see, claiming free games, crafting badges, sale events |
| **Trophies** | Pick a game, tick or untick its achievements, save. Or tap **Select**, pick several games (up to 100), and unlock (or lock) everything in all of them at once, with progress and a summary per game. Some games set certain achievements only from their own servers; those are marked "game only" and are left alone |
| **History** | Today, 7 and 30 days and all time, a bar per day, a 12-week calendar with your current and best streaks, the games that got the most time |
| **More** | Free games (giveaways, and the best-reviewed free-to-play games: filter by achievements or cards, sort, add them), badges and Steam XP (craft, see missing cards, the discovery queue), cheap games for trading cards (bought with your wallet, you confirm), the badge shop (buy missing cards, you confirm), accounts (switch, make default, sign out, and **add one by scanning a QR code with the Steam app**), alerts, a new link for the page, and quitting houridle |

**More on the page.**
- **Steam level:** your level and XP to the next one, on the Now tab and in Badges & XP, with the badges ready to craft and the card sets you've started.
- **Card drop alerts:** when a card drops, the page and your alerts webhook say which game it dropped for.
- **Inventory & gems (More):** what your cards sell for, game by game. Turn spare copies into gems (one of each card is always kept) and gems into booster packs. Both use things up, so each asks you to confirm the exact cards or gems first.
- **Schedule (Idle):** idle only at certain hours and days, such as overnight. Idling pauses when the hours end and picks up again when they start; Start and Stop still work any time.
- **Unlock gradually (Trophies):** unlock a game's achievements one at a time, about every 15 minutes to a day, the most common first, so your profile looks like you played. It carries on while houridle runs.
- **Idle alongside (Accounts):** other saved accounts can idle at the same time, each on its own Steam connection: their card drops, or every game they own.
- **Family games:** games your [Steam family](https://store.steampowered.com/families) shares with you show up alongside your own (marked "Family") in the Idle and Achievements lists, and "Everything" idles them too. Hours count on your account. Borrowed games don't drop trading cards, and the owner always comes first: if they start playing, Steam stops the borrowed copy.
- **Steam record (Accounts):** VAC and game bans, trade restrictions, whether the account is limited, and whether the Market is open to it. houridle checks twice a day and alerts you when anything changes (a new ban, a trade restriction, or one lifted); a ban or trade restriction also shows on the Now tab.
- **Weekly summary (Alerts):** on Sunday evenings, a message with the week's hours, card drops, badges, achievements, money spent and your Steam level.

**Staying up to date.** houridle serve checks GitHub for a new release every few hours (More → Updates, or **Check now**). It downloads the build for your computer, checks it against the release's `SHA256SUMS`, swaps it in and restarts, carrying on with whatever it was idling. Turn **Update automatically** off to be told instead. Releases come from pushing a version tag (`git tag v1.13.0 && git push origin v1.13.0`; see `.github/workflows/release.yml`). No GitHub account or token is needed. (For a private fork, houridle uses a GitHub token if it finds one, in order: `update.token` in `config.json`, `HOURIDLE_GITHUB_TOKEN`, `GITHUB_TOKEN` or `GH_TOKEN`, or the GitHub CLI's sign-in. It's only sent to GitHub, and never logged.)

**Keeping up with Steam.** What houridle tells Steam when signing in (protocol and client versions, and optionally where the server list comes from) is read from `steam.json` in the repository twice a day. So if Steam changes something and sign-ins start failing, editing that file fixes every houridle without a new release (values that don't look right are ignored). houridle also keeps Steam's last server list, tries servers that failed recently last, and shows Valve's current Steam client version under More → Updates.

**On your phone's home screen.** Open the page's link, then use **Add to Home Screen** (Safari's share menu, or Chrome's menu). houridle opens full screen like an app, with its own icon. On a computer, keys **1**–**5** switch tabs and **/** jumps to the search box.

houridle serve remembers what it was doing, so after a restart (or a reboot, with the service) it carries on. Signing in with a password isn't offered on the page (it would travel over plain HTTP); use the QR code, or `houridle login` on the server.

- The page is at port 8080 unless you choose another: `houridle web :9000` (or `houridle serve -web :9000` for one run). `houridle web off` turns it off for `houridle` and `houridle start`.
- The link's key is the password: anyone who has it can control houridle. **More → This page → Make a new key** (or `houridle web -new-key`) makes a new one; old links stop working. The key is kept in `config.json` (readable only by you) and never written to the log.
- It's plain HTTP, meant for your home network. To use it from elsewhere, use a VPN such as [Tailscale](https://tailscale.com) rather than opening the port to the internet.
- Steam keeps one connection per sign-in, so while houridle idles, the page lists games, changes achievements and claims free games over the idler's own connection.
- `houridle` and `houridle start` serve a smaller **status page** instead when the page is turned on (or with `-web :8080`): Now and History, and a Stop button that ends houridle.

### Hours history

houridle adds up what it idles, per game per day, in `history.json` next to it (saved every few minutes, so a crash loses little). `houridle stats` shows the totals for today, the last 7 and 30 days and all time, a bar for each of the last 14 days (`-days 30` for more), and each game's share. The status page shows today's and the all-time total too. Hours are game-hours: idling 3 games for an hour counts as 3. Accounts other than the default keep `history-name.json`.

### Free games

Steam gives games away now and then: 100% off for a few days, yours to keep. houridle can add them to your account for you, like pressing **Add to Account** on their store pages:

```bash
houridle claim -dry-run
houridle claim
```

The first shows what's on offer, best reviewed first; the second adds it. Or let a running houridle do it: `houridle start -claim` checks when it starts and every 12 hours after, and says in the activity feed (and your [alerts](#alerts)) what it added. It skips what you already own, and DLC for games you don't own. Free weekends (play free for a few days, not to keep) aren't 100% off, so they're never added.

Steam also has games that are always free. `houridle claim -free-to-play 10` adds the 10 best-reviewed ones you don't have yet (with `-dry-run` to see them first). Add `-with achievements` (or `cards`, or `achievements,cards`) for only games with Steam achievements or trading cards, and `-sort popular` (most reviewed), `newest` or `name` to rank them differently. The control panel does the same under **More → Top free-to-play games**, 25 to 200 at a time, with each game's reviews. Once added, they're in your library like any other game, so houridle can idle them and you can work on their achievements. (Steam only drops trading cards in a free game after you've spent money in it.)

### Badges and Steam XP

Steam XP comes mostly from crafting game badges: each takes a full set of one game's trading cards (100 XP a level, up to 5 levels, plus a foil badge). Card drops give about half a set; the rest you buy or trade for on the Steam Market.

```bash
houridle craft -dry-run
houridle craft
```

The first shows which badges are ready, and the ones closest to a full set; the second crafts every ready one (using up the cards). `houridle start -craft` does it by itself while idling: when it starts, every 6 hours, and right after a card drops. The control panel (**More → Badges & XP**) shows the same, and lists each set's missing cards with a link to buy each on the Market.

**Buying the missing cards.** The control panel's **More → Badge shop** ranks the badges you're closest to by Steam XP per dollar, using the Market's current prices. Tapping a badge looks up the cheapest listing of each missing card right now and shows the total, fees included; **Buy**, then **Pay** to confirm, buys exactly those listings. houridle never buys on its own, and refuses a purchase if a price went up since the quote, if it's more than your wallet holds, or if it's over your **daily limit** (5.00 in your wallet's currency until you change it; "Off" turns buying off). What's spent is kept in `config.json`. Steam only opens the Market to accounts with a Steam purchase between 7 days and a year old; until then the shop shows why. With **Craft badges** on, the badge is crafted once the cards arrive.

**Buying cheap games for cards.** **More → Games for cards** lists games with trading cards that you don't own, up to a price you pick ($0.50 to $5), ranked by Steam XP per dollar (or by price, reviews, or how much of the price the card drops sell back for). Idling a game drops half its card set: sell those cards, or buy the rest to craft a badge (100 XP). Each game shows its price, how many cards drop, what they sell for, and what a level 1 badge would cost. **Buy**, then **Pay** to confirm, buys it with your Steam wallet (never a card). houridle refuses if your Steam cart already has something in it (so nothing else is bought), if the price went up, if it's more than your wallet holds or over the same daily limit as cards, and it cancels before paying if Steam's final total (tax included) is more than the price you agreed to. Games still marked "Profile Features Limited" (no card drops) are left out. Steam's 2-hour refund window is used up quickly by idling.

During Steam's big sales, exploring your discovery queue each day earns event cards and stickers. `houridle events` explores it and claims the daily sticker; `houridle start -events` does it every 12 hours while idling. Outside sales it just steps through the queue, which does nothing else.

### Keys

| Where | Key | Does |
|---|---|---|
| Lists | ↑ ↓ | Move |
| Lists | type | Search |
| Lists | Space | Tick or untick |
| Lists | Enter | Done |
| Lists | Esc | Cancel |
| While idling | T | Send to the tray (Windows) |
| While idling | Q | Stop |

### Log

Everything houridle does while idling goes into `houridle.log` next to it (or in the user config folder, see above): starts, stops, games that fail, Steam closing, and sign-in trouble. It's handy for checking on a server. Once the log reaches 1 MB it moves to `houridle.log.1` and a new one starts. Accounts other than the default log to `houridle-name.log`. The hours themselves are added up in `history.json` (see [Hours history](#hours-history)).

## Good to know

- You can only idle or change achievements for games you **own**.
- Unlocked achievements show on your profile like normal ones. You can lock them again.
- Only one houridle idles at a time (per account, with `start`). A second copy says so and exits.
- If Steam runs as administrator, run houridle as administrator too.
- Valve doesn't officially support idling or editing achievements. Use it at your own risk.

## Run it on a server

The simplest way is with a saved sign-in: no Steam client, no virtual screen and no VNC, just houridle and a service. Signing in works over SSH; the QR code is drawn right in the terminal.

### Linux server with a saved sign-in

On the server, over SSH:

```bash
git clone https://github.com/vperked/houridle && cd houridle && ./install.sh
```

The script does everything (see [Getting started](#getting-started)) and finishes with the control panel's link. Signing in works over SSH too, if you'd rather: `~/houridle/houridle login` draws the QR code right in the terminal. To get the link again later, run `~/houridle/houridle web`.

To install a build you made elsewhere instead, copy it and `deploy/linux` to the server and run `bash deploy/linux/setup-signin.sh path/to/houridle`, then `systemctl --user enable --now houridle-signin`.

Run as root, the script adds system services instead; leave out `--user` in the `systemctl` commands. The service runs `houridle serve`, which can switch between your accounts. To idle a second account at the same time, `houridle login` as it and enable `houridle-signin@<account name>`, which runs `houridle start -account <account name>` with its options on the command line.

To check on it:

```bash
tail -f ~/houridle/houridle.log
```

```bash
systemctl --user status houridle-signin
```

### Windows server or VM

1. Install Steam, sign in once with **Remember me** ticked, and enable **Settings → Interface → Run Steam when my computer starts**.
2. Copy `houridle.exe` to the server and run `houridle -setup` to choose your games.
3. Run `houridle -autostart on`. From then on, houridle starts in the tray whenever you sign in.
4. So it all comes back after a reboot, turn on automatic sign-in for that Windows account, for example with Microsoft's [Autologon](https://learn.microsoft.com/sysinternals/downloads/autologon) tool.

Over Remote Desktop, **close the window to disconnect**. Signing out closes Steam and houridle.

### Linux server with the Steam client

If you'd rather idle through the Steam client, the kit in `deploy/linux/` sets that up on an Ubuntu or Debian server, including one without a screen. It runs three background services that start at boot and restart if they stop:

1. a virtual screen (Xvfb), because the Steam client needs one
2. the Steam client
3. houridle

You sign in to Steam once by viewing that virtual screen from your own computer. After that it all runs on its own.

**On your computer**, build houridle for Linux and copy it and the kit to the server:

```bash
make page
GOOS=linux go build -o bin/houridle ./backend/cmd/houridle
```

```bash
ssh you@your-server mkdir -p houridle-setup
scp -r bin/houridle deploy/linux you@your-server:houridle-setup/
```

**On the server**, over SSH:

1. Install everything (Steam asks you to accept its license):

   ```bash
   bash ~/houridle-setup/linux/setup.sh ~/houridle-setup/houridle
   ```

2. Sign in to Steam. This prints a one-time password and the exact SSH tunnel and VNC steps; you'll need a VNC viewer on your computer, e.g. TigerVNC or RealVNC Viewer:

   ```bash
   bash ~/houridle-setup/linux/steam-login.sh
   ```

   While you're in Steam, tick **Remember me**, approve Steam Guard, and install any small **native Linux** game you own. houridle needs its `libsteam_api.so` (or copy a 64-bit `libsteam_api.so` into `~/houridle/` yourself).

3. Choose your games. After saving them, houridle starts idling right there; press `Q` to stop it before the next step:

   ```bash
   ~/houridle/houridle -setup
   ```

4. Start idling, now and after every reboot:

   ```bash
   systemctl --user enable --now houridle
   ```

To check on it:

```bash
tail -f ~/houridle/houridle.log
```

```bash
systemctl --user status houridle houridle-steam
```

To change the games later, stop the service (only one houridle idles at a time), pick them (press `Q` once it starts idling), and start the service again so it reads the new list:

```bash
systemctl --user stop houridle
~/houridle/houridle -setup
systemctl --user restart houridle
```

To stop it:

```bash
systemctl --user disable --now houridle
```

Re-running `setup.sh` with a newer `houridle` installs it and restarts the houridle service if it's running (and the display and Steam, if their service files changed). The virtual screen only accepts programs that have its key (`~/houridle/Xauthority`, readable only by you), so other users on the server can't watch it.

Other distros work too: install Steam, Xvfb, xauth and x11vnc with your package manager, then run `setup.sh`. Use the regular Steam package, not the Flatpak or Snap.

## Code

The repository has two parts:

| Folder | What's in it |
|---|---|
| `backend/` | houridle itself, in Go: everything it does, and the server for the status page |
| `frontend/` | The status page, in React, TypeScript and Tailwind |
| `deploy/linux/` | Linux server setup: installers (with a saved sign-in, or with the Steam client), Steam sign-in helper, services |

### Backend

The Go code is in `backend/`, one folder per package, grouped by what they do:

| Folder | What's in it |
|---|---|
| `cmd/houridle/` | The program: reads the command and runs it (`go build ./backend/cmd/houridle`) |
| **`client/`** | **The Steam client on this computer** |
| `client/steam/` | Finds Steam and reads your games (Windows and Linux) |
| `client/steamworks/` | Talks to Steam through Valve's API library |
| `client/achievements/` | Reads and changes achievements through the Steam client |
| `client/childproc/` | Starts the helper processes hidden, on each OS |
| **`online/`** | **Steam's servers and websites, with a saved sign-in** |
| `online/steamauth/` | Signing in to Steam (QR, password, Steam Guard), the saved sign-ins (one per account) and keeping them fresh |
| `online/steamcm/` | Connects to Steam's servers: reports games as played, sets your status, reads playtime, licenses and achievements |
| `online/steamweb/` | What talking to Steam's websites shares: a signed-in HTTP client and short errors |
| `online/steamver/` | Keeps the values Steam's sign-in expects up to date, from `steam.json` |
| **`features/`** | **What houridle does besides idling** |
| `features/cards/` | Reads your badge pages for card drops left, and crafts badges |
| `features/freegames/` | Finds Steam's giveaways and adds them to your account |
| `features/gamestore/` | Store prices and deals; buys a game with your wallet when you confirm |
| `features/inventory/` | Your inventory: cards, gems and booster packs |
| `features/market/` | Reads the Steam Market (wallet, card prices, listings) and buys a listing when you confirm |
| `features/refund/` | Steam's refund window: which recent purchases could still be refunded, and which games they hold |
| `features/history/` | Adds up the hours idled, in `history.json` |
| `features/notify/` | Sends alerts to a webhook |
| `features/update/` | Checks GitHub for new releases and updates houridle |
| **`run/`** | **Running an idling session** |
| `run/idler/` | Runs an idling session: a helper process per game through the Steam client, or one connection with a saved sign-in |
| `run/stoptime/` | Reads "3h", "90m" or "23:30" for the automatic stop |
| `run/handover/` | Switches between window and tray; stops two copies idling at once |
| `run/config/` | Saves and loads `config.json`: game lists and defaults |
| `run/eventlog/` | Writes `houridle.log` |
| `run/autostart/` | Starts houridle when you sign in (Windows) |
| **`views/`** | **What you see** |
| `views/ui/` | Everything in the console |
| `views/tray/` | The tray icon and its menu (Windows) |
| `views/web/` | Serves the page and its API; `web/page/dist/` is the built frontend (not in git) |
| `views/panel/` | `houridle serve`: runs idling for the control panel |
| `tests/` | The tests |

Files ending in `Windows.go` or `Linux.go` hold the parts that differ per OS; everything else is shared. Run the tests with `go test ./...`.

### Frontend

The status page is a small React app in TypeScript, styled with Tailwind, built with Vite. It builds into `backend/views/web/page/dist/`, which is compiled into houridle and isn't in git (`make` and `build.bat` build it first). To build just the page (you need [Node.js](https://nodejs.org) 22 or newer):

```bash
cd frontend
npm ci
npm run build
```

`npm run build` type-checks the code first (`npm run typecheck` does just that, with no build).

| Folder | What's in it |
|---|---|
| `src/` | `main.tsx` starts the page, `App.tsx` is its frame and tabs, `index.css` its looks |
| `src/screens/` | One file per tab (Idle, Achievements, More), and the More menu's screens (`screens/more/`) |
| `src/components/` | The pieces screens are built from (`ui.tsx`), the Now and History blocks, and notifications (`toast.tsx`) |
| `src/hooks/` | Reading houridle's status, your library and card drops |
| `src/lib/` | The API (`api.ts`) and the shape of its answers (`types.ts`, matching `backend/views/web`), formatting, money, game pictures |
| `mock/` | The test data `npm run dev` answers with, shaped by `src/lib/types.ts` |

`npm run dev` serves the page with live reload and test data, so no houridle is needed (open the `http://127.0.0.1:…` address it prints; it moves to the next port if one is taken): a pretend one (in `frontend/mock/`, used only by the dev server, never built into the page) with two accounts, a few games idling, history, badges, a shop and so on. Clicking around works: starting and stopping, switching accounts, saving achievements and settings, buying all change its state until the dev server restarts. Answers take about 300 ms, like over a network. Other situations, by adding `?mock=<scenario>` to the page's address or starting with `MOCK=<scenario> npm run dev`:

| Scenario | What the page sees |
|---|---|
| `panel` | The control panel (`houridle serve`), signed in and idling. The default |
| `status` | A plain status page (`houridle start -web`): games and Stop, nothing else |
| `big` | A library of 650 games, all idling: the long lists and their pages |
| `empty` | Signed in, nothing idling, nothing in the library or inventory |
| `offline` | houridle can't be reached |
| `unauthorized` | The page's key is wrong |

To work against a real houridle instead, point `HOURIDLE_URL` at one running its status page and give its key: `HOURIDLE_URL=http://127.0.0.1:8095 HOURIDLE_KEY=<key> npm run dev` (the key is the part after `?key=` in that houridle's link). The dev server says which it's using when it starts.

### Builds and releases

GitHub runs the tests, checks both platforms build, and checks the built status page matches `frontend/`, on every push. Pushing a version tag (`git tag v1.6.0 && git push origin v1.6.0`) builds the release binaries and publishes them, with that version.
