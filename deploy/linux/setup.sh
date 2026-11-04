#!/usr/bin/env bash
# Sets houridle up on a Linux server (Ubuntu or Debian):
#   - installs Steam, a virtual display (Xvfb) and a VNC server (x11vnc)
#   - copies houridle to ~/houridle
#   - adds services for the display, Steam and houridle that start at boot
#
# Run it as your normal user (it uses sudo where needed):
#   ./deploy/linux/setup.sh [path/to/houridle]
set -euo pipefail

here="$(cd "$(dirname "$0")" && pwd)"
install_dir="$HOME/houridle"
unit_dir="$HOME/.config/systemd/user"

say() { printf '\n\033[1;36m◆\033[0m %s\n' "$*"; }
die() { printf '\n\033[1;31m✕\033[0m %s\n' "$*" >&2; exit 1; }

find_binary() {
  local given="${1:-}"
  if [ -n "$given" ]; then
    [ -f "$given" ] || die "No file at $given"
    echo "$given"
    return
  fi
  for candidate in "$here/../../houridle" "./houridle"; do
    if [ -f "$candidate" ]; then
      echo "$candidate"
      return
    fi
  done
  die "Couldn't find the houridle program. Build it with
    GOOS=linux go build -o houridle ./backend
then run: $0 path/to/houridle"
}

install_packages() {
  if command -v Xvfb >/dev/null && command -v x11vnc >/dev/null && steam_path >/dev/null; then
    say "Steam, Xvfb and x11vnc are already installed"
    return
  fi
  command -v apt-get >/dev/null ||
    die "This script installs packages with apt (Ubuntu/Debian). Install Steam, Xvfb and x11vnc with your package manager, then run it again."

  say "Installing Steam, Xvfb and x11vnc (Steam will ask you to accept its license)"
  sudo dpkg --add-architecture i386 # Steam needs 32-bit libraries
  sudo apt-get update
  if sudo apt-get install -y xvfb x11vnc steam-installer; then
    return
  fi
  if grep -qi '^ID=ubuntu' /etc/os-release; then
    say "Turning on Ubuntu's multiverse repository, where Steam lives"
    sudo apt-get install -y software-properties-common
    sudo add-apt-repository -y multiverse
    sudo apt-get update
    sudo apt-get install -y xvfb x11vnc steam-installer
  else
    die "Couldn't install Steam. On Debian, add 'contrib non-free' to the lines in /etc/apt/sources.list, then run this again."
  fi
}

# steam_path prints where the Steam launcher is.
steam_path() {
  for path in "$(command -v steam 2>/dev/null || true)" /usr/games/steam /usr/bin/steam; do
    if [ -n "$path" ] && [ -x "$path" ]; then
      echo "$path"
      return 0
    fi
  done
  return 1
}

install_houridle() {
  say "Installing houridle to $install_dir"
  mkdir -p "$install_dir"
  install -m 755 "$1" "$install_dir/houridle"
}

install_services() {
  local steam
  steam="$(steam_path)" || die "Steam was installed but its launcher wasn't found."

  say "Adding services (display, Steam, houridle)"
  mkdir -p "$unit_dir"
  cp "$here/systemd/houridle-display.service" "$here/systemd/houridle.service" "$unit_dir/"
  sed "s|STEAM_PATH|$steam|" "$here/systemd/houridle-steam.service" >"$unit_dir/houridle-steam.service"

  say "Letting your services keep running when you log out"
  sudo loginctl enable-linger "$USER"

  systemctl --user daemon-reload
  systemctl --user enable --now houridle-display.service houridle-steam.service
}

main() {
  [ "$(id -u)" -ne 0 ] || die "Run this as your normal user, not root. It uses sudo where it needs to."
  local binary
  binary="$(find_binary "${1:-}")"

  install_packages
  install_houridle "$binary"
  install_services

  say "Done. Steam is starting on the virtual display. Next:"
  cat <<EOF

  1. Sign in to Steam once:      bash $here/steam-login.sh
  2. Choose the games to idle:   ~/houridle/houridle -setup
  3. Start idling (and at boot): systemctl --user enable --now houridle

  Check on it any time:          tail -f ~/houridle/houridle.log
EOF
}

main "$@"
