# houridle

A small Windows tool for Steam:

- **Idle hours.** Your games count as being played while houridle runs, up to 32 at once.
- **Unlock achievements.** Tick the ones you want, press Enter.

It works through the Steam client you're already signed in to. You don't log in to houridle, and no games are launched.

## Build

You need [Go](https://go.dev/dl/) 1.26 or newer.

```bash
go build -o houridle.exe ./src
```

Or double-click `build.bat`.

## Use

Open Steam and sign in, then run `houridle.exe`. The first time, it asks which games to idle.

| Command | What it does |
|---|---|
| `houridle` | Idle your saved games |
| `houridle 730 440` | Idle these games (by app ID) |
| `houridle -setup` | Choose which games to idle |
| `houridle -tray` | Idle in the system tray, by the clock |
| `houridle -list` | Show your games and their app IDs |
| `houridle -achievements` | Unlock or lock achievements |

An app ID is the number in a game's Steam store link, like `store.steampowered.com/app/730`.

### Keys

| Where | Key | Does |
|---|---|---|
| Lists | ↑ ↓ | Move |
| Lists | type | Search |
| Lists | Space | Tick or untick |
| Lists | Enter | Done |
| Lists | Esc | Cancel |
| While idling | T | Send to the tray |
| While idling | Q | Stop |

## Good to know

- You can only idle or change achievements for games you **own**.
- Unlocked achievements show on your profile like normal ones. You can lock them again.
- If Steam runs as administrator, run houridle as administrator too.
- Valve doesn't officially support idling or editing achievements. Use it at your own risk.

## Run it on a server

houridle works through the Steam client, so the server needs to be a **Windows** machine or VM with Steam signed in on a desktop session. Linux servers aren't supported.

1. Install Steam on the server and sign in once, with **Remember me** ticked.
2. Copy `houridle.exe` and your `config.json` to the server.
3. Run `houridle -tray` so it idles quietly in the tray.

When you connect with Remote Desktop, **close the window to disconnect**. Don't sign out: signing out closes Steam and houridle too.

To keep it running after a reboot:

1. Turn on automatic sign-in for the server's Windows account, for example with Microsoft's [Autologon](https://learn.microsoft.com/sysinternals/downloads/autologon) tool.
2. In Steam, enable **Settings → Interface → Run Steam when my computer starts**.
3. Press Win+R, type `shell:startup`, and add a shortcut to `houridle.exe -tray` in the folder that opens.

## Code

All the code is in `src/`:

| Folder | What's in it |
|---|---|
| `src/` | Starting point: reads the command and runs it |
| `config/` | Saves and loads your game list |
| `steam/` | Finds Steam on your PC and reads your games |
| `steamworks/` | Talks to Steam through Valve's `steam_api64.dll` |
| `idler/` | Keeps one small helper process running per game |
| `achievements/` | Reads and changes achievements |
| `handover/` | Switches between window and tray; stops two copies idling at once |
| `tray/` | The tray icon and its menu |
| `ui/` | Everything you see in the console |
| `tests/` | All the tests |

Run the tests with `go test ./...`.
