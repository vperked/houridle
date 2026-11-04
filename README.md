# houridle

A small tool for Steam, for Windows and Linux:

- **Idle hours.** Your games count as being played while houridle runs, up to 32 at once.
- **Unlock achievements.** Tick the ones you want, press Enter.

By default it works through the Steam client you're already signed in to, and no games are launched. Or sign in to Steam through houridle itself (`houridle login`) and idle with no Steam client at all (`houridle start`).

## Build

You need [Go](https://go.dev/dl/) 1.26 or newer.

```bash
go build -o houridle.exe ./src
```

For Linux, build `houridle` instead. This works from any OS, and no C compiler is needed:

```bash
GOOS=linux go build -o houridle ./src
```

On Windows you can also double-click `build.bat`.

## Use

Open Steam and sign in, then run houridle. The first time, it asks which games to idle. Each time you start idling, it asks whether to stop by itself: type hours (`3`), a time (`23:30`), or press Enter to keep going.

| Command | What it does |
|---|---|
| `houridle` | Idle your saved games |
| `houridle 730 440` | Idle these games (by app ID) |
| `houridle -stop 3h` | Stop after 3 hours, without asking (`90m` and `23:30` work too) |
| `houridle -stop never` | Keep going until you stop it, without asking |
| `houridle -setup` | Choose which games to idle |
| `houridle -list` | Show your games and their app IDs |
| `houridle -achievements` | Unlock or lock achievements |
| `houridle -tray` | Idle in the system tray, by the clock (Windows) |
| `houridle -autostart on` | Start in the tray when you sign in (Windows); `off` to undo |

### Signing in to Steam (optional)

| Command | What it does |
|---|---|
| `houridle login` | Asks how to sign in: a QR code to scan with the Steam mobile app, or your account name and password |
| `houridle login -qr` / `-password` | Skip the question and pick the method (`-method qr` or `-method password` mean the same). With a password, Steam Guard (app code, email code or app approval) is asked for as needed |
| `houridle start [ids]` | Idle over the network with the saved sign-in. Same games, `-stop`, `-setup`, dashboard and `-tray` as above. Steam doesn't need to be installed |
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
- houridle only ever uses the account you signed in with. Signing in as a different account asks before replacing the saved one.
- Steam lets one device play at a time. If you start a game on another device, `start` pauses and resumes when you stop; it never kicks you out of your own game.
- With no Steam installed, `houridle start -setup` (or the first `start`) offers to load the games your Steam account owns, over the saved sign-in, and then shows the usual picker. `houridle start -list` does the same without asking. You can also pass app IDs (`houridle start 730 440`). `-achievements` and `-autostart` still use the Steam client.
- `logout` removes the token from this computer. Steam may keep the token valid until it expires; sign out of all devices in your Steam security settings to end it everywhere.

An app ID is the number in a game's Steam store link, like `store.steampowered.com/app/730`.

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

Everything houridle does while idling goes into `houridle.log` next to it: starts, stops, games that fail, and Steam closing. It's handy for checking on a server. Once the log reaches 1 MB it moves to `houridle.log.1` and a new one starts.

## Good to know

- You can only idle or change achievements for games you **own**.
- Unlocked achievements show on your profile like normal ones. You can lock them again.
- Only one houridle idles at a time. A second copy says so and exits.
- If Steam runs as administrator, run houridle as administrator too.
- Valve doesn't officially support idling or editing achievements. Use it at your own risk.

## Run it on a server

By default houridle needs the Steam client running and signed in on the same machine. With `houridle login` and `houridle start` it signs in to Steam itself instead, so a server needs no Steam client, no virtual screen and no VNC: run `houridle login` once (in a terminal over SSH, the QR code is drawn right there), then `houridle start` under tmux or a service.

### Windows server or VM

1. Install Steam, sign in once with **Remember me** ticked, and enable **Settings → Interface → Run Steam when my computer starts**.
2. Copy `houridle.exe` to the server and run `houridle -setup` to choose your games.
3. Run `houridle -autostart on`. From then on, houridle starts in the tray whenever you sign in.
4. So it all comes back after a reboot, turn on automatic sign-in for that Windows account, for example with Microsoft's [Autologon](https://learn.microsoft.com/sysinternals/downloads/autologon) tool.

Over Remote Desktop, **close the window to disconnect**. Signing out closes Steam and houridle.

### Linux server

The kit in `deploy/linux/` sets everything up on an Ubuntu or Debian server, including one without a screen. It runs three background services that start at boot and restart if they stop:

1. a virtual screen (Xvfb), because the Steam client needs one
2. the Steam client
3. houridle

You sign in to Steam once by viewing that virtual screen from your own computer. After that it all runs on its own.

**On your computer**, build houridle for Linux and copy it and the kit to the server:

```bash
GOOS=linux go build -o houridle ./src
```

```bash
scp -r houridle deploy/linux you@your-server:houridle-setup/
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

3. Choose your games:

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

To stop it:

```bash
systemctl --user disable --now houridle
```

Other distros work too: install Steam, Xvfb and x11vnc with your package manager, then run `setup.sh`. Use the regular Steam package, not the Flatpak or Snap.

## Code

All the code is in `src/`:

| Folder | What's in it |
|---|---|
| `src/` | Starting point: reads the command and runs it |
| `config/` | Saves and loads your game list |
| `steam/` | Finds Steam and reads your games (Windows and Linux) |
| `steamworks/` | Talks to Steam through Valve's API library |
| `steamauth/` | Signing in to Steam (QR, password, Steam Guard), the saved sign-in and keeping it fresh |
| `steamcm/` | Connects to Steam's servers with that sign-in and reports games as played |
| `idler/` | Keeps one small helper process running per game |
| `childproc/` | Starts those helpers hidden, on each OS |
| `achievements/` | Reads and changes achievements |
| `stoptime/` | Reads "3h", "90m" or "23:30" for the automatic stop |
| `eventlog/` | Writes `houridle.log` |
| `handover/` | Switches between window and tray; stops two copies idling at once |
| `autostart/` | Starts houridle when you sign in (Windows) |
| `tray/` | The tray icon and its menu (Windows) |
| `ui/` | Everything you see in the console |
| `tests/` | All the tests |
| `deploy/linux/` | Linux server setup: installer, Steam sign-in helper, services |

Files ending in `Windows.go` or `Linux.go` hold the parts that differ per OS; everything else is shared.

Run the tests with `go test ./...`.
