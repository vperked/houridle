#!/usr/bin/env bash
# Sets houridle up on a Linux server to idle with a saved Steam sign-in
# ("houridle login"). Unlike setup.sh it needs no Steam client, no virtual
# display and no VNC: just houridle and a service that starts it at boot.
#
#   bash deploy/linux/setup-signin.sh [path/to/houridle]
#
# As a normal user it adds user services (and keeps them running after you
# log out); as root it adds system services.
set -euo pipefail

here="$(cd "$(dirname "$0")" && pwd)"
install_dir="${HOURIDLE_DIR:-$HOME/houridle}"

say() { printf '\n\033[1;36m◆\033[0m %s\n' "$*"; }
die() { printf '\n\033[1;31m✕\033[0m %s\n' "$*" >&2; exit 1; }

find_binary() {
  local given="${1:-}"
  if [ -n "$given" ]; then
    [ -f "$given" ] || die "No file at $given"
    echo "$given"
    return
  fi
  for candidate in "$here/../../houridle" "./houridle" "$install_dir/houridle"; do
    if [ -f "$candidate" ]; then
      echo "$candidate"
      return
    fi
  done
  die "Couldn't find the houridle program. Build it with
    GOOS=linux go build -o houridle ./backend
then run: $0 path/to/houridle"
}

main() {
  local binary unit_dir target ctl
  binary="$(find_binary "${1:-}")"
  if [ "$(id -u)" -eq 0 ]; then
    unit_dir=/etc/systemd/system target=multi-user.target ctl="systemctl"
  else
    unit_dir="$HOME/.config/systemd/user" target=default.target ctl="systemctl --user"
  fi

  say "Installing houridle to $install_dir"
  mkdir -p "$install_dir"
  if [ "$(realpath "$binary")" != "$(realpath -m "$install_dir/houridle")" ]; then
    install -m 755 "$binary" "$install_dir/houridle"
  fi

  say "Adding the services to $unit_dir"
  mkdir -p "$unit_dir"
  for unit in houridle-signin.service houridle-signin@.service; do
    sed -e "s|INSTALL_DIR|$install_dir|g" -e "s|TARGET|$target|g" "$here/systemd/$unit" >"$unit_dir/$unit"
  done
  if [ "$(id -u)" -ne 0 ]; then
    say "Letting your services keep running when you log out"
    sudo loginctl enable-linger "$USER" || say "Couldn't enable lingering; services stop when you log out"
  fi
  $ctl daemon-reload

  say "Done. Next:"
  cat <<NEXT

  1. Start houridle (and at boot): $ctl enable --now houridle-signin
  2. Get the control panel's link: $install_dir/houridle web
  3. Open it on your phone, sign in with the Steam app (More), and pick
     what to idle (Idle). It all carries on after a reboot.

  Prefer the terminal? $install_dir/houridle login signs in there.
  Check on it any time:            tail -f $install_dir/houridle.log
NEXT
}

main "$@"
