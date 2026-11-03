#!/usr/bin/env bash
# Shows the server's Steam window over VNC so you can sign in once and
# approve Steam Guard. The VNC server only listens on the server itself, so
# you reach it through an SSH tunnel, and it needs a one-time password.
#
# Run it after setup.sh:
#   ./deploy/linux/steam-login.sh
set -euo pipefail

die() { printf '\n\033[1;31m✕\033[0m %s\n' "$*" >&2; exit 1; }

command -v x11vnc >/dev/null || die "x11vnc isn't installed. Run setup.sh first."
systemctl --user start houridle-display.service houridle-steam.service

# A random one-time password, in a file only you can read. x11vnc deletes
# the file once it has read it ("rm:").
password="$(tr -dc 'a-zA-Z0-9' </dev/urandom | head -c 12 || true)"
password_file="$(mktemp)"
chmod 600 "$password_file"
printf '%s\n' "$password" >"$password_file"

host="$(hostname -f 2>/dev/null || hostname)"
cat <<EOF

On your own computer:

  1. Open a tunnel to the server (leave it open):
       ssh -L 5900:localhost:5900 $USER@$host

  2. Point a VNC viewer at  localhost:5900  with the password  $password
     (TigerVNC, RealVNC Viewer and Remmina all work)

  3. Sign in to Steam, tick "Remember me", and approve Steam Guard.
     Also install any small native Linux game you own: houridle uses its
     libsteam_api.so. Then close the VNC viewer.

Waiting for you to connect…
EOF

x11vnc -display :99 -localhost -once -quiet -passwdfile "rm:$password_file"
printf '\nDone. Steam stays signed in on the server.\n'
